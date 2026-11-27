import type { ApiClient } from '@shared/http/client'
import { InvalidResponseError } from '@shared/http/errors'
import { readCredential, type CredentialRow } from './group-detail'
import { boolean, integer, list, oneOf, record, text } from './response'

export const capacityKey = ['modern', 'capacity'] as const

export interface QuotaForecast {
  windowId: string
  /** Share of the window already used, in basis points (10,000 = 100%). */
  usedBasisPoints: number
  resetAt: number | null
  /** Basis points used per hour at the current pace; 0 when unknown. */
  pacePerHour: number
  paceSource: 'recent' | 'window_average' | 'none'
  /** When the window runs out, if that happens before it resets. */
  exhaustsAt: number | null
  exhausted: boolean
  resetPassed: boolean
}

export interface CapacityAccount {
  groupId: number
  credential: CredentialRow
  forecasts: QuotaForecast[]
}

export interface Capacity {
  observedAt: number
  accounts: CapacityAccount[]
}

const optionalTime = (value: unknown) => (value == null ? null : integer(value))

function readForecast(value: unknown): QuotaForecast {
  const row = record(value)
  const pace = row.pace_basis_points_per_hour
  if (typeof pace !== 'number' || !Number.isFinite(pace) || pace < 0) {
    throw new InvalidResponseError()
  }
  return {
    windowId: text(row.window_id),
    usedBasisPoints: integer(row.used_basis_points),
    resetAt: optionalTime(row.reset_at_ms),
    pacePerHour: pace,
    paceSource: oneOf(row.pace_source, ['recent', 'window_average', 'none'] as const),
    exhaustsAt: optionalTime(row.exhausts_at_ms),
    exhausted: boolean(row.exhausted),
    resetPassed: boolean(row.reset_passed),
  }
}

export async function getCapacity(client: ApiClient, signal: AbortSignal): Promise<Capacity> {
  const data = record(await client.request('/api/modern/capacity', { signal }))
  return {
    observedAt: integer(data.observed_at_ms),
    accounts: list(data.accounts).map((value): CapacityAccount => {
      const account = record(value)
      return {
        groupId: integer(account.group_id, 1),
        credential: readCredential(account.credential),
        forecasts: list(account.forecasts).map(readForecast),
      }
    }),
  }
}
