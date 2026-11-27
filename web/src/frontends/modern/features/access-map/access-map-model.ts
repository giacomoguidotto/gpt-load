import type { AccessKeyRow } from '@modern/api/access-keys'
import type { CapacityAccount, QuotaForecast } from '@modern/api/capacity'
import type { CredentialQuota } from '@modern/api/credential-observation'
import type { GroupRow } from '@modern/api/groups'
import { quotaRemaining, sortedQuotaWindows } from '@modern/features/groups/credential-presentation'

/**
 * How long something keeps serving:
 * - `lasts`: every limit it reports holds until its next reset.
 * - `runs-out`: at the current pace it runs out at `at`.
 * - `exhausted`: out now; `at` is when it comes back, if known.
 * - `offline`: disabled, blocked or signed out; it serves nothing.
 * - `unmetered`: it reports no quota (for example a plain API key).
 */
export type Runway =
  | { kind: 'lasts' }
  | { kind: 'runs-out'; at: number }
  | { kind: 'exhausted'; at: number | null }
  | { kind: 'offline' }
  | { kind: 'unmetered' }

export interface MapWindow {
  quota: CredentialQuota
  forecast?: QuotaForecast
  /** Percent of the window left, 0–100. */
  remaining?: number
}

export interface MapAccount {
  key: string
  id: number
  poolId: number
  source: CapacityAccount
  windows: MapWindow[]
  /** Percent left in the tightest account-wide window. */
  remaining?: number
  runway: Runway
}

export interface MapPool {
  id: number
  group: GroupRow
  accountKeys: string[]
  userIds: number[]
  remaining?: number
  runway: Runway
  available: number
}

export interface MapChannelRunway {
  channelId: string
  channelName: string
  channelIcon: string
  available: number
  total: number
  runway: Runway
}

export interface MapUser {
  id: number
  key: AccessKeyRow
  poolIds: number[]
  allPools: boolean
  channels: MapChannelRunway[]
}

export interface AccessMap {
  users: MapUser[]
  pools: MapPool[]
  accounts: MapAccount[]
  nextToRunOut?: { account: MapAccount; at: number }
}

export const accountKey = (poolId: number, credentialId: number) => `${poolId}:${credentialId}`

function isOffline(account: CapacityAccount): boolean {
  const credential = account.credential
  return (
    !credential.enabled ||
    credential.state === 'blacklisted' ||
    credential.state === 'disabled' ||
    credential.authState === 'reauthorization_required'
  )
}

function accountRunway(account: CapacityAccount, windows: MapWindow[]): Runway {
  if (isOffline(account)) return { kind: 'offline' }
  // Model-scoped windows limit only some models; the account keeps serving
  // the rest, so only account-wide windows decide the account's runway.
  const limits = windows.filter((window) => window.quota.scope === 'account' && window.forecast)
  if (!limits.length) {
    return account.credential.state === 'cooldown'
      ? { kind: 'exhausted', at: account.credential.cooldownUntil }
      : { kind: 'unmetered' }
  }
  const exhausted = limits.filter((window) => window.forecast!.exhausted)
  if (exhausted.length) {
    const resets = exhausted.map((window) => window.forecast!.resetAt)
    return {
      kind: 'exhausted',
      at: resets.every((at) => at !== null) ? Math.max(...(resets as number[])) : null,
    }
  }
  const runsOut = limits
    .map((window) => window.forecast!.exhaustsAt)
    .filter((at): at is number => at !== null)
  return runsOut.length ? { kind: 'runs-out', at: Math.min(...runsOut) } : { kind: 'lasts' }
}

/**
 * The scheduler fails over between accounts, so a set of accounts serves
 * until the last one runs out, and comes back when the first one returns.
 */
export function combinedRunway(runways: Runway[]): Runway {
  const serving = runways.filter((runway) => runway.kind !== 'offline')
  if (!serving.length) return { kind: 'offline' }
  const live = serving.filter((runway) => runway.kind !== 'exhausted')
  if (!live.length) {
    const returns = serving
      .map((runway) => (runway.kind === 'exhausted' ? runway.at : null))
      .filter((at): at is number => at !== null)
    return { kind: 'exhausted', at: returns.length ? Math.min(...returns) : null }
  }
  if (live.some((runway) => runway.kind === 'lasts')) return { kind: 'lasts' }
  if (live.every((runway) => runway.kind === 'unmetered')) return { kind: 'unmetered' }
  if (live.some((runway) => runway.kind === 'unmetered')) return { kind: 'lasts' }
  return {
    kind: 'runs-out',
    at: Math.max(...live.map((runway) => (runway.kind === 'runs-out' ? runway.at : 0))),
  }
}

function averageRemaining(accounts: MapAccount[]): number | undefined {
  const values = accounts
    .filter((account) => account.runway.kind !== 'offline')
    .map((account) => (account.runway.kind === 'exhausted' ? 0 : account.remaining))
    .filter((value): value is number => value !== undefined)
  return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : undefined
}

const isAvailable = (account: MapAccount) =>
  account.runway.kind !== 'offline' && account.runway.kind !== 'exhausted'

export function buildAccessMap(
  groups: GroupRow[],
  keys: AccessKeyRow[],
  capacity: CapacityAccount[],
): AccessMap {
  const accounts = capacity.map((source): MapAccount => {
    const forecasts = new Map(source.forecasts.map((forecast) => [forecast.windowId, forecast]))
    const windows = sortedQuotaWindows(source.credential.observation?.windows ?? [])
      .filter((quota) => quota.unit === 'percent')
      .map((quota): MapWindow => {
        const forecast = forecasts.get(quota.id)
        return {
          quota,
          forecast,
          remaining: forecast?.resetPassed ? 100 : quotaRemaining(quota),
        }
      })
    const accountWide = windows
      .filter((window) => window.quota.scope === 'account' && window.remaining !== undefined)
      .map((window) => window.remaining!)
    return {
      key: accountKey(source.groupId, source.credential.id),
      id: source.credential.id,
      poolId: source.groupId,
      source,
      windows,
      remaining: accountWide.length ? Math.min(...accountWide) : undefined,
      runway: accountRunway(source, windows),
    }
  })

  const accountsByPool = new Map<number, MapAccount[]>()
  for (const account of accounts) {
    accountsByPool.set(account.poolId, [...(accountsByPool.get(account.poolId) ?? []), account])
  }

  const users = keys.map((key): MapUser => {
    const allPools = key.filters.groups.length === 0
    const poolIds = allPools
      ? groups.map((group) => group.id)
      : groups.filter((group) => key.filters.groups.includes(group.id)).map((group) => group.id)
    const channels = new Map<string, MapAccount[]>()
    const channelGroups = new Map<string, GroupRow>()
    for (const group of groups) {
      if (!poolIds.includes(group.id) || !group.enabled) continue
      channelGroups.set(group.channelID, group)
      channels.set(group.channelID, [
        ...(channels.get(group.channelID) ?? []),
        ...(accountsByPool.get(group.id) ?? []),
      ])
    }
    return {
      id: key.id,
      key,
      poolIds,
      allPools,
      channels: [...channels].map(([channelId, reachable]) => {
        const group = channelGroups.get(channelId)!
        return {
          channelId,
          channelName: group.channelName,
          channelIcon: group.channelIcon,
          available: reachable.filter(isAvailable).length,
          total: reachable.length,
          runway: combinedRunway(reachable.map((account) => account.runway)),
        }
      }),
    }
  })

  const pools = groups.map((group): MapPool => {
    const members = accountsByPool.get(group.id) ?? []
    return {
      id: group.id,
      group,
      accountKeys: members.map((account) => account.key),
      userIds: users.filter((user) => user.poolIds.includes(group.id)).map((user) => user.id),
      remaining: averageRemaining(members),
      runway: group.enabled
        ? combinedRunway(members.map((account) => account.runway))
        : { kind: 'offline' },
      available: members.filter(isAvailable).length,
    }
  })

  let nextToRunOut: AccessMap['nextToRunOut']
  for (const account of accounts) {
    if (account.runway.kind !== 'runs-out') continue
    if (!nextToRunOut || account.runway.at < nextToRunOut.at) {
      nextToRunOut = { account, at: account.runway.at }
    }
  }
  return { users, pools, accounts, nextToRunOut }
}

export type Focus =
  { kind: 'user'; id: number } | { kind: 'pool'; id: number } | { kind: 'account'; key: string }

export interface FocusSet {
  users: Set<number>
  pools: Set<number>
  accounts: Set<string>
}

/** Everything connected to the focused item: who reaches it and what it reaches. */
export function focusSet(map: AccessMap, focus: Focus | null): FocusSet | null {
  if (!focus) return null
  const result: FocusSet = { users: new Set(), pools: new Set(), accounts: new Set() }
  const addPool = (pool: MapPool, withUsers: boolean) => {
    result.pools.add(pool.id)
    pool.accountKeys.forEach((key) => result.accounts.add(key))
    if (withUsers) pool.userIds.forEach((id) => result.users.add(id))
  }
  if (focus.kind === 'user') {
    const user = map.users.find((item) => item.id === focus.id)
    if (!user) return null
    result.users.add(user.id)
    map.pools
      .filter((pool) => user.poolIds.includes(pool.id))
      .forEach((pool) => addPool(pool, false))
  } else if (focus.kind === 'pool') {
    const pool = map.pools.find((item) => item.id === focus.id)
    if (!pool) return null
    addPool(pool, true)
  } else {
    const account = map.accounts.find((item) => item.key === focus.key)
    const pool = account && map.pools.find((item) => item.id === account.poolId)
    if (!account || !pool) return null
    result.accounts.add(account.key)
    result.pools.add(pool.id)
    pool.userIds.forEach((id) => result.users.add(id))
  }
  return result
}
