<script setup lang="ts">
import { KeyRound, Layers2, TriangleAlert, UserRound, Waypoints } from '@lucide/vue'
import { useQuery } from '@tanstack/vue-query'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { accessKeysKey, getAccessKeys, type AccessKeyRow } from '@modern/api/access-keys'
import { capacityKey, getCapacity } from '@modern/api/capacity'
import { getGroupWorkspace } from '@modern/api/groups'
import { usePageRefresh } from '@modern/app/page-refresh'
import { useReadOnlySession } from '@modern/app/use-read-only'
import {
  AppBadge,
  AppButton,
  AppChannelIcon,
  AppCollectionState,
  AppIcon,
  AppLinkLayer,
  AppPressable,
  AppTooltip,
  type AppLink,
  type SemanticTone,
} from '@modern/components/ui'
import { useClock } from '@modern/components/ui/clock'
import { formatRemainingDuration } from '@modern/components/ui/format'
import { numberFormatter } from '@modern/components/ui/intl-formatters'
import { quotaWindowTitle } from '@modern/features/groups/credential-presentation'
import { ApiError } from '@shared/http/errors'
import { useApiClient } from '@shared/http/client-context'
import {
  buildAccessMap,
  focusSet,
  type Focus,
  type MapAccount,
  type MapPool,
  type MapWindow,
  type Runway,
} from './access-map-model'

const { t, te, locale } = useI18n()
const client = useApiClient()
const readOnly = useReadOnlySession()
const now = useClock()

const groupsQuery = useQuery({
  queryKey: ['modern', 'groups'],
  queryFn: ({ signal }) => getGroupWorkspace(client, signal),
})
const capacityQuery = useQuery({
  queryKey: capacityKey,
  queryFn: ({ signal }) => getCapacity(client, signal),
})
const keysQuery = useQuery({
  queryKey: [...accessKeysKey, 'access-map'],
  enabled: computed(() => !readOnly.value),
  queryFn: async ({ signal }) => {
    const items: AccessKeyRow[] = []
    for (let page = 1; ; page++) {
      const result = await getAccessKeys(
        client,
        {
          q: '',
          status: '',
          sort: 'updated_desc',
          page,
          pageSize: 100,
          group: '',
          expiry: '',
        },
        signal,
      )
      items.push(...result.items)
      if (page >= result.pagination.total_pages) return items
    }
  },
})

const queries = [groupsQuery, capacityQuery, keysQuery]
const pending = computed(
  () =>
    groupsQuery.isPending.value ||
    capacityQuery.isPending.value ||
    (!readOnly.value && keysQuery.isPending.value),
)
const failure = computed(
  () => groupsQuery.error.value ?? capacityQuery.error.value ?? keysQuery.error.value,
)
const map = computed(() =>
  groupsQuery.data.value && capacityQuery.data.value
    ? buildAccessMap(
        groupsQuery.data.value.items,
        keysQuery.data.value ?? [],
        capacityQuery.data.value.accounts,
      )
    : undefined,
)

async function refresh(): Promise<void> {
  await Promise.all(queries.map((query) => query.refetch()))
}
usePageRefresh({
  refresh,
  pending: () => queries.some((query) => query.isFetching.value),
  updatedAt: () => capacityQuery.data.value?.observedAt,
  autoRefresh: async () => {
    const results = await Promise.all(queries.map((query) => query.refetch()))
    return results.every((result) => !result.isError)
  },
})

// Pools are listed by channel so a user's links fan out to one block per
// provider instead of zigzagging down the column.
const channels = computed(() => {
  const result = new Map<string, { id: string; pools: MapPool[] }>()
  for (const pool of map.value?.pools ?? []) {
    const entry = result.get(pool.group.channelID) ?? { id: pool.group.channelID, pools: [] }
    entry.pools.push(pool)
    result.set(pool.group.channelID, entry)
  }
  return [...result.values()]
})
const accountsByPool = computed(() => {
  const result = new Map<number, MapAccount[]>()
  for (const account of map.value?.accounts ?? []) {
    result.set(account.poolId, [...(result.get(account.poolId) ?? []), account])
  }
  return result
})

// Hover shows connections for a moment; a click keeps them until cleared.
const hovered = ref<Focus | null>(null)
const pinned = ref<Focus | null>(null)
const focus = computed(() => hovered.value ?? pinned.value)
const connected = computed(() => (map.value ? focusSet(map.value, focus.value) : null))

function sameFocus(a: Focus | null, b: Focus): boolean {
  if (!a || a.kind !== b.kind) return false
  return a.kind === 'account' ? a.key === (b as typeof a).key : a.id === (b as typeof a).id
}
function toggle(target: Focus): void {
  pinned.value = sameFocus(pinned.value, target) ? null : target
}
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') pinned.value = null
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

const userState = (id: number) =>
  !connected.value ? '' : connected.value.users.has(id) ? 'is-linked' : 'is-dimmed'
const poolState = (id: number) =>
  !connected.value ? '' : connected.value.pools.has(id) ? 'is-linked' : 'is-dimmed'
const accountState = (key: string) =>
  !connected.value ? '' : connected.value.accounts.has(key) ? 'is-linked' : 'is-dimmed'

// Only the focused item's connections are drawn: every user reaches several
// pools, so drawing all of them at rest buries the structure in lines.
const links = computed<AppLink[]>(() => {
  const focusConnections = connected.value
  if (!focusConnections) return []
  return (map.value?.users ?? [])
    .filter((user) => focusConnections.users.has(user.id))
    .flatMap((user) =>
      user.poolIds
        .filter((poolId) => focusConnections.pools.has(poolId))
        .map((poolId) => ({
          id: `${user.id}-${poolId}`,
          from: `user:${user.id}`,
          to: `pool:${poolId}`,
          active: true,
        })),
    )
})

const summary = computed(() => {
  const value = map.value
  if (!value) return undefined
  return {
    users: value.users.filter((user) => user.key.status === 'active' && !user.key.expired).length,
    pools: value.pools.filter(
      (pool) => pool.runway.kind !== 'offline' && pool.runway.kind !== 'exhausted',
    ).length,
    poolsTotal: value.pools.length,
    accounts: value.accounts.filter(
      (account) => account.runway.kind !== 'offline' && account.runway.kind !== 'exhausted',
    ).length,
    accountsTotal: value.accounts.length,
    next: value.nextToRunOut,
  }
})

function duration(at: number): string {
  return formatRemainingDuration(at - now.value, locale.value)
}
function percent(value: number | undefined): string {
  return value === undefined
    ? '—'
    : numberFormatter(locale.value, { style: 'percent', maximumFractionDigits: 0 }).format(
        value / 100,
      )
}
function runwayText(runway: Runway): string {
  switch (runway.kind) {
    case 'lasts':
      return t('accessMap.runway.lasts')
    case 'runs-out':
      return runway.at <= now.value
        ? t('accessMap.runway.runsOutNow')
        : t('accessMap.runway.runsOut', { time: duration(runway.at) })
    case 'exhausted':
      return runway.at && runway.at > now.value
        ? t('accessMap.runway.exhaustedUntil', { time: duration(runway.at) })
        : t('accessMap.runway.exhausted')
    case 'offline':
      return t('accessMap.runway.offline')
    case 'unmetered':
      return t('accessMap.runway.unmetered')
  }
}
function runwayTone(runway: Runway): SemanticTone {
  switch (runway.kind) {
    case 'lasts':
      return 'success'
    case 'runs-out':
      return runway.at - now.value < 60 * 60 * 1000 ? 'danger' : 'warning'
    case 'exhausted':
      return 'danger'
    case 'offline':
      return 'neutral'
    case 'unmetered':
      return 'info'
  }
}
function remainingTone(value: number | undefined): SemanticTone {
  if (value === undefined) return 'neutral'
  return value <= 0 ? 'danger' : value < 30 ? 'danger' : value < 60 ? 'warning' : 'success'
}
function windowTitle(window: MapWindow): string {
  const key = 'credentialCards.quotaLabels.' + window.quota.labelKey
  return quotaWindowTitle(
    window.quota,
    window.quota.labelKey && te(key) ? t(key) : window.quota.label,
  )
}
function windowDetail(window: MapWindow): string {
  const forecast = window.forecast
  const parts: string[] = []
  if (forecast?.resetPassed) return t('accessMap.account.resetPassed')
  const resetAt = forecast?.resetAt ?? window.quota.resetsAt
  if (resetAt && resetAt > now.value)
    parts.push(t('accessMap.account.resetsIn', { time: duration(resetAt) }))
  if (forecast && forecast.pacePerHour > 0) {
    parts.push(t('accessMap.account.pace', { percent: percent(forecast.pacePerHour / 100) }))
  }
  if (forecast?.exhaustsAt) {
    parts.push(
      forecast.exhaustsAt <= now.value
        ? t('accessMap.runway.runsOutNow')
        : t('accessMap.runway.runsOut', { time: duration(forecast.exhaustsAt) }),
    )
  }
  return parts.join(' · ')
}
function initials(name: string): string {
  const words = name.trim().split(/\s+/u).filter(Boolean)
  return (words.length > 1 ? words[0]![0]! + words[1]![0]! : name.slice(0, 2)).toUpperCase()
}
const failureIsServer = computed(
  () => failure.value instanceof ApiError && failure.value.status >= 500,
)
</script>

<template>
  <div class="modern-access-map">
    <header class="modern-access-map-intro">
      <p>{{ t('accessMap.intro') }}</p>
      <div class="modern-access-map-hint">
        <span>{{ t('accessMap.hint') }}</span>
        <AppButton v-if="pinned" size="sm" variant="ghost" @click="pinned = null">{{
          t('accessMap.clearFocus')
        }}</AppButton>
      </div>
    </header>

    <AppCollectionState
      v-if="pending && !map"
      :title="t('collection.loading')"
      loading
      :on-retry="refresh"
    />
    <AppCollectionState
      v-else-if="!map"
      :title="t('accessMap.loadFailed')"
      :description="
        t(failureIsServer ? 'collection.serverFailedHelp' : 'collection.loadFailedHelp')
      "
      :icon="TriangleAlert"
      error
      ><AppButton @click="refresh">{{ t('collection.retry') }}</AppButton></AppCollectionState
    >
    <AppCollectionState
      v-else-if="!map.pools.length"
      :title="t('accessMap.empty')"
      :description="t('accessMap.emptyHelp')"
      :icon="Waypoints"
    />
    <template v-else>
      <section v-if="summary" class="modern-access-map-summary">
        <div class="modern-access-map-stat">
          <span>{{ t('accessMap.summary.users') }}</span>
          <strong>{{ readOnly ? '—' : summary.users }}</strong>
        </div>
        <div class="modern-access-map-stat">
          <span>{{ t('accessMap.summary.pools') }}</span>
          <strong
            >{{ summary.pools }}<small>/{{ summary.poolsTotal }}</small></strong
          >
        </div>
        <div class="modern-access-map-stat">
          <span>{{ t('accessMap.summary.accounts') }}</span>
          <strong
            >{{ summary.accounts }}<small>/{{ summary.accountsTotal }}</small></strong
          >
        </div>
        <AppPressable
          v-if="summary.next"
          class="modern-access-map-stat modern-access-map-stat--next"
          @click="toggle({ kind: 'account', key: summary.next.account.key })"
        >
          <span>{{ t('accessMap.summary.next') }}</span>
          <strong>{{ summary.next.account.source.credential.label }}</strong>
          <em>{{
            summary.next.at <= now
              ? t('accessMap.runway.runsOutNow')
              : t('accessMap.summary.nextIn', { time: duration(summary.next.at) })
          }}</em>
        </AppPressable>
        <div v-else class="modern-access-map-stat modern-access-map-stat--next">
          <span>{{ t('accessMap.summary.next') }}</span>
          <em>{{ t('accessMap.summary.nextNone') }}</em>
        </div>
      </section>

      <section
        class="modern-access-map-board"
        :class="{
          'modern-access-map-board--focused': connected,
          'modern-access-map-board--pools-only': readOnly,
        }"
        @mouseleave="hovered = null"
      >
        <AppLinkLayer v-if="!readOnly" class="modern-access-map-links" :links="links" />

        <div v-if="!readOnly" class="modern-access-map-column">
          <div class="modern-access-map-column-head">
            <AppIcon :icon="UserRound" size="sm" />
            <strong>{{ t('accessMap.columns.users') }}</strong>
            <span>{{ t('accessMap.columnHints.users') }}</span>
          </div>
          <AppPressable
            v-for="(user, index) in map.users"
            :key="user.id"
            class="modern-access-map-card modern-access-map-user"
            :class="[
              userState(user.id),
              { 'is-pinned': sameFocus(pinned, { kind: 'user', id: user.id }) },
            ]"
            :style="{ '--i': index }"
            :data-link="`user:${user.id}`"
            :pressed="sameFocus(pinned, { kind: 'user', id: user.id })"
            @mouseenter="hovered = { kind: 'user', id: user.id }"
            @focus="hovered = { kind: 'user', id: user.id }"
            @blur="hovered = null"
            @click="toggle({ kind: 'user', id: user.id })"
          >
            <span class="modern-access-map-user-head">
              <span class="modern-access-map-avatar" aria-hidden="true">{{
                initials(user.key.name)
              }}</span>
              <span class="modern-access-map-user-name">
                <strong>{{ user.key.name }}</strong>
                <span class="modern-access-map-mono">
                  <AppIcon :icon="KeyRound" size="xs" />{{ user.key.masked_key }}
                </span>
              </span>
              <AppBadge v-if="user.key.status === 'disabled'" tone="neutral" size="xs">{{
                t('accessMap.user.disabled')
              }}</AppBadge>
              <AppBadge v-else-if="user.key.expired" tone="danger" size="xs">{{
                t('accessMap.user.expired')
              }}</AppBadge>
              <AppBadge v-else tone="brand" variant="plain" size="xs">{{
                user.allPools
                  ? t('accessMap.user.allPools')
                  : t('accessMap.user.pools', { count: user.poolIds.length }, user.poolIds.length)
              }}</AppBadge>
            </span>
            <span v-if="!user.channels.length" class="modern-access-map-muted">{{
              t('accessMap.user.noPools')
            }}</span>
            <span
              v-for="channel in user.channels"
              :key="channel.channelId"
              class="modern-access-map-channel"
            >
              <AppChannelIcon :icon="channel.channelIcon" :name="channel.channelName" size="sm" />
              <span class="modern-access-map-channel-name">{{ channel.channelName }}</span>
              <span
                class="modern-access-map-runway"
                :class="`tone-${runwayTone(channel.runway)}`"
                >{{ runwayText(channel.runway) }}</span
              >
              <span class="modern-access-map-muted modern-access-map-channel-count">{{
                t('accessMap.user.channelAccounts', {
                  available: channel.available,
                  total: channel.total,
                })
              }}</span>
            </span>
          </AppPressable>
        </div>

        <div v-if="!readOnly" class="modern-access-map-gutter" aria-hidden="true" />

        <div class="modern-access-map-column modern-access-map-column--pools">
          <div class="modern-access-map-column-head">
            <AppIcon :icon="Layers2" size="sm" />
            <strong
              >{{ t('accessMap.columns.pools') }} · {{ t('accessMap.columns.accounts') }}</strong
            >
            <span>{{ t('accessMap.columnHints.pools') }}</span>
          </div>
          <template v-for="channel in channels" :key="channel.id">
            <article
              v-for="(pool, index) in channel.pools"
              :key="pool.id"
              class="modern-access-map-card modern-access-map-pool"
              :class="[
                poolState(pool.id),
                { 'is-pinned': sameFocus(pinned, { kind: 'pool', id: pool.id }) },
              ]"
              :style="{ '--i': index + 2 }"
              :data-link="`pool:${pool.id}`"
            >
              <AppPressable
                class="modern-access-map-pool-head"
                :pressed="sameFocus(pinned, { kind: 'pool', id: pool.id })"
                @mouseenter="hovered = { kind: 'pool', id: pool.id }"
                @focus="hovered = { kind: 'pool', id: pool.id }"
                @blur="hovered = null"
                @click="toggle({ kind: 'pool', id: pool.id })"
              >
                <AppChannelIcon
                  :icon="pool.group.channelIcon"
                  :mark="pool.group.channelMark"
                  :name="pool.group.channelName"
                  size="md"
                  surface
                />
                <span class="modern-access-map-pool-title">
                  <strong>{{ pool.group.name }}</strong>
                  <span class="modern-access-map-muted"
                    >{{ pool.group.channelName }} ·
                    {{
                      pool.accountKeys.length
                        ? t('accessMap.pool.accounts', {
                            available: pool.available,
                            total: pool.accountKeys.length,
                          })
                        : t('accessMap.pool.noAccounts')
                    }}
                    <template v-if="!readOnly">
                      ·
                      {{
                        t(
                          'accessMap.pool.users',
                          { count: pool.userIds.length },
                          pool.userIds.length,
                        )
                      }}</template
                    ></span
                  >
                </span>
                <AppBadge v-if="!pool.group.enabled" tone="neutral" size="xs">{{
                  t('accessMap.pool.paused')
                }}</AppBadge>
                <span class="modern-access-map-runway" :class="`tone-${runwayTone(pool.runway)}`">{{
                  runwayText(pool.runway)
                }}</span>
              </AppPressable>
              <div
                v-if="pool.remaining !== undefined"
                class="modern-access-map-meter"
                :class="`tone-${remainingTone(pool.remaining)}`"
                role="meter"
                :aria-label="t('accessMap.pool.remaining', { percent: percent(pool.remaining) })"
                :aria-valuenow="Math.round(pool.remaining)"
                aria-valuemin="0"
                aria-valuemax="100"
              >
                <span :style="{ '--fill': pool.remaining / 100 }" />
              </div>

              <ul class="modern-access-map-accounts">
                <li
                  v-for="(account, accountIndex) in accountsByPool.get(pool.id) ?? []"
                  :key="account.key"
                  :style="{ '--i': accountIndex }"
                >
                  <AppPressable
                    class="modern-access-map-account"
                    :class="[
                      accountState(account.key),
                      `runway-${account.runway.kind}`,
                      { 'is-pinned': sameFocus(pinned, { kind: 'account', key: account.key }) },
                    ]"
                    :pressed="sameFocus(pinned, { kind: 'account', key: account.key })"
                    @mouseenter="hovered = { kind: 'account', key: account.key }"
                    @focus="hovered = { kind: 'account', key: account.key }"
                    @blur="hovered = null"
                    @click="toggle({ kind: 'account', key: account.key })"
                  >
                    <span class="modern-access-map-account-head">
                      <span
                        class="modern-access-map-dot"
                        :class="`tone-${runwayTone(account.runway)}`"
                        aria-hidden="true"
                      />
                      <strong class="modern-access-map-account-name">{{
                        account.source.credential.label
                      }}</strong>
                      <AppBadge
                        v-if="account.source.credential.observation?.plan"
                        tone="brand"
                        variant="outline"
                        size="xs"
                        >{{ account.source.credential.observation.plan }}</AppBadge
                      >
                      <span
                        class="modern-access-map-runway"
                        :class="`tone-${runwayTone(account.runway)}`"
                        >{{ runwayText(account.runway) }}</span
                      >
                    </span>
                    <span v-if="account.windows.length" class="modern-access-map-windows">
                      <AppTooltip
                        v-for="window in account.windows.filter(
                          (item) => item.quota.scope === 'account',
                        )"
                        :key="window.quota.id"
                        :label="windowDetail(window) || windowTitle(window)"
                      >
                        <span
                          class="modern-access-map-window"
                          :class="{ 'is-scoped': window.quota.scope !== 'account' }"
                        >
                          <span class="modern-access-map-window-label">
                            <span>{{ windowTitle(window) }}</span>
                            <span>{{
                              t('accessMap.account.remaining', {
                                percent: percent(window.remaining),
                              })
                            }}</span>
                          </span>
                          <span
                            class="modern-access-map-meter modern-access-map-meter--sm"
                            :class="`tone-${window.forecast?.exhausted ? 'danger' : remainingTone(window.remaining)}`"
                          >
                            <span :style="{ '--fill': (window.remaining ?? 0) / 100 }" />
                          </span>
                          <span class="modern-access-map-window-detail">{{
                            windowDetail(window)
                          }}</span>
                        </span>
                      </AppTooltip>
                    </span>
                  </AppPressable>
                </li>
              </ul>
            </article>
          </template>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.modern-access-map {
  display: grid;
  gap: var(--modern-space-5);
  padding: var(--modern-page-toolbar-top) 0 var(--modern-content-bottom);
}
.modern-access-map-intro {
  display: grid;
  gap: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
.modern-access-map-intro > p {
  color: var(--modern-text);
  font-size: var(--modern-font-size-body);
}
.modern-access-map-hint {
  display: flex;
  align-items: center;
  gap: var(--modern-space-3);
  min-height: var(--modern-control-sm);
}

/* Summary tiles */
.modern-access-map-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr)) minmax(0, 1.6fr);
  gap: var(--modern-space-3);
}
.modern-access-map-stat {
  display: grid;
  align-content: start;
  gap: var(--modern-space-1);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-4);
  color: var(--modern-muted);
  font: inherit;
  font-size: var(--modern-font-size-small);
  text-align: left;
  animation: modern-access-map-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 2);
}
.modern-access-map-stat:nth-child(2) {
  --i: 1;
}
.modern-access-map-stat:nth-child(3) {
  --i: 2;
}
.modern-access-map-stat:nth-child(4) {
  --i: 3;
}
.modern-access-map-stat strong {
  color: var(--modern-text);
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  letter-spacing: var(--modern-tracking-title);
  line-height: var(--modern-leading-title);
  font-variant-numeric: tabular-nums;
}
.modern-access-map-stat small {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-regular);
}
.modern-access-map-stat em {
  color: var(--modern-muted);
  font-style: normal;
}
.modern-access-map-stat--next {
  background: var(--modern-key-card-tint);
  border-color: var(--modern-tooltip-border);
}
.modern-access-map-stat--next.modern-pressable {
  cursor: pointer;
  transition:
    transform var(--modern-motion-medium) var(--modern-motion-ease-spring),
    box-shadow var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-access-map-stat--next.modern-pressable:hover {
  transform: translateY(var(--modern-motion-lift));
  box-shadow: var(--modern-shadow-lift);
}
.modern-access-map-stat--next strong {
  overflow: hidden;
  font-size: var(--modern-font-size-section);
  letter-spacing: normal;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-access-map-stat--next em {
  color: var(--modern-accent);
  font-weight: var(--modern-weight-medium);
}

/* Board */
.modern-access-map-board {
  position: relative;
  isolation: isolate;
  display: grid;
  grid-template-columns: var(--modern-access-map-users) var(--modern-link-gutter) minmax(0, 1fr);
  align-items: start;
}
.modern-access-map-board--pools-only {
  grid-template-columns: minmax(0, 1fr);
}
.modern-access-map-column {
  display: grid;
  gap: var(--modern-space-3);
  min-width: 0;
}
.modern-access-map-column-head {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  min-height: var(--modern-control-sm);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-access-map-column-head strong {
  color: var(--modern-text);
  font-size: var(--modern-font-size-secondary);
  font-weight: var(--modern-weight-semibold);
}
.modern-access-map-column-head > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.modern-access-map-card {
  display: grid;
  gap: var(--modern-space-3);
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  padding: var(--modern-space-3) var(--modern-space-4);
  color: var(--modern-text);
  font: inherit;
  text-align: left;
  animation: modern-access-map-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger));
  transition:
    opacity var(--modern-motion-medium) var(--modern-motion-ease-out),
    transform var(--modern-motion-medium) var(--modern-motion-ease-spring),
    border-color var(--modern-motion-medium) var(--modern-motion-ease-out),
    box-shadow var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-access-map-card.is-linked {
  border-color: var(--modern-tooltip-border);
  box-shadow: var(--modern-shadow-lift);
  transform: translateY(var(--modern-motion-lift));
}
.modern-access-map-card.is-dimmed {
  opacity: var(--modern-dimmed-opacity);
}
.modern-access-map-card.is-pinned {
  border-color: var(--modern-accent);
  box-shadow: var(--modern-shadow-focus), var(--modern-shadow-lift);
}
.modern-access-map-card.modern-pressable:hover:not(.is-dimmed) {
  border-color: var(--modern-control-border-hover);
}

/* Users */
.modern-access-map-user-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--modern-space-3);
}
.modern-access-map-avatar {
  display: inline-grid;
  place-items: center;
  width: var(--modern-control-md);
  height: var(--modern-control-md);
  border-radius: var(--modern-radius-round);
  background: var(--modern-accent-soft);
  color: var(--modern-accent);
  font-size: var(--modern-font-size-small);
  font-weight: var(--modern-weight-semibold);
  letter-spacing: var(--modern-tracking-label);
}
.modern-access-map-user-name {
  display: grid;
  min-width: 0;
}
.modern-access-map-user-name strong,
.modern-access-map-pool-title strong {
  overflow: hidden;
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-semibold);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-access-map-mono {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1);
  color: var(--modern-muted);
  font-family: var(--modern-font-mono);
  font-size: var(--modern-font-size-caption);
}
.modern-access-map-channel {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  column-gap: var(--modern-space-2);
  row-gap: 0;
  font-size: var(--modern-font-size-small);
}
.modern-access-map-channel-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-access-map-channel-count {
  grid-column: 2 / -1;
  font-size: var(--modern-font-size-caption);
}
.modern-access-map-muted {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}

/* Runway pills */
.modern-access-map-runway {
  border-radius: var(--modern-radius-small);
  padding: 0 var(--modern-space-2);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  line-height: var(--modern-control-xxs);
  white-space: nowrap;
}
.tone-success.modern-access-map-runway {
  background: var(--modern-success-soft);
  color: var(--modern-success);
}
.tone-warning.modern-access-map-runway {
  background: var(--modern-warning-soft);
  color: var(--modern-warning);
}
.tone-danger.modern-access-map-runway {
  background: var(--modern-danger-soft);
  color: var(--modern-danger);
}
.tone-info.modern-access-map-runway {
  background: var(--modern-info-soft);
  color: var(--modern-info);
}
.tone-neutral.modern-access-map-runway {
  background: var(--modern-subtle);
  color: var(--modern-muted);
}

/* Pools */
.modern-access-map-pool {
  padding: 0;
  overflow: hidden;
}
.modern-access-map-pool-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: center;
  gap: var(--modern-space-3);
  border: 0;
  background: transparent;
  padding: var(--modern-space-3) var(--modern-space-4) 0;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.modern-access-map-pool-title {
  display: grid;
  min-width: 0;
}
.modern-access-map-pool > .modern-access-map-meter {
  margin: 0 var(--modern-space-4);
}

/* Meters fill with a spring when they first appear and when values change. */
.modern-access-map-meter {
  height: var(--modern-progress-bar-sm);
  overflow: hidden;
  border-radius: var(--modern-radius-small);
  background: var(--modern-progress-track);
}
.modern-access-map-meter--sm {
  height: var(--modern-loading-line);
}
.modern-access-map-meter > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--modern-status-success);
  transform: scaleX(var(--fill, 0));
  transform-origin: left center;
  animation: modern-access-map-fill var(--modern-motion-draw) var(--modern-motion-ease-out) both;
  transition: transform var(--modern-motion-slow) var(--modern-motion-ease-spring);
}
.tone-warning > span {
  background: var(--modern-status-warning);
}
.tone-danger > span {
  background: var(--modern-status-danger);
}
.tone-neutral > span {
  background: var(--modern-status-neutral);
}

/* Accounts */
.modern-access-map-accounts {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(var(--modern-key-card-min), 1fr));
  gap: var(--modern-space-2);
  margin: 0;
  padding: var(--modern-space-2) var(--modern-space-3) var(--modern-space-3);
  list-style: none;
}
.modern-access-map-accounts > li {
  display: grid;
  min-width: 0;
  animation: modern-access-map-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger));
}
.modern-access-map-account {
  display: grid;
  align-content: start;
  gap: var(--modern-space-2);
  min-width: 0;
  border: var(--modern-line-width) solid transparent;
  border-radius: var(--modern-radius-control);
  background: var(--modern-subtle);
  padding: var(--modern-space-2) var(--modern-space-3);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    opacity var(--modern-motion-medium) var(--modern-motion-ease-out),
    border-color var(--modern-motion-medium) var(--modern-motion-ease-out),
    background var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-access-map-account:hover {
  border-color: var(--modern-control-border-hover);
}
.modern-access-map-account.is-dimmed {
  opacity: var(--modern-dimmed-opacity);
}
.modern-access-map-account.is-linked,
.modern-access-map-account.is-pinned {
  border-color: var(--modern-tooltip-border);
  background: var(--modern-surface);
}
.modern-access-map-account.is-pinned {
  border-color: var(--modern-accent);
}
.modern-access-map-account.runway-exhausted .modern-access-map-account-name,
.modern-access-map-account.runway-offline .modern-access-map-account-name {
  color: var(--modern-muted);
}
.modern-access-map-account-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  column-gap: var(--modern-space-2);
  row-gap: var(--modern-space-1);
}
.modern-access-map-account-head > .modern-access-map-runway {
  grid-column: 2 / -1;
  justify-self: start;
}
.modern-access-map-account-name {
  overflow: hidden;
  font-size: var(--modern-font-size-secondary);
  font-weight: var(--modern-weight-medium);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-access-map-dot {
  width: var(--modern-space-2);
  height: var(--modern-space-2);
  border-radius: var(--modern-radius-round);
  background: var(--modern-status-neutral);
}
.modern-access-map-dot.tone-success {
  background: var(--modern-status-success);
  box-shadow: var(--modern-shadow-focus);
  animation: modern-access-map-pulse var(--modern-motion-loading) var(--modern-motion-ease) infinite
    alternate;
}
.modern-access-map-dot.tone-warning {
  background: var(--modern-status-warning);
}
.modern-access-map-dot.tone-danger {
  background: var(--modern-status-danger);
}
.modern-access-map-dot.tone-info {
  background: var(--modern-info);
}
.modern-access-map-windows {
  display: grid;
  gap: var(--modern-space-2);
}
.modern-access-map-window {
  display: grid;
  gap: var(--modern-space-1);
}
.modern-access-map-window.is-scoped {
  opacity: var(--modern-link-dimmed-opacity);
}
.modern-access-map-window-label {
  display: flex;
  justify-content: space-between;
  gap: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-variant-numeric: tabular-nums;
}
.modern-access-map-window-detail {
  overflow: hidden;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  text-overflow: ellipsis;
  white-space: nowrap;
}

@keyframes modern-access-map-rise {
  from {
    opacity: 0;
    transform: translateY(var(--modern-motion-rise));
  }
}
@keyframes modern-access-map-fill {
  from {
    transform: scaleX(0);
  }
}
@keyframes modern-access-map-pulse {
  to {
    box-shadow: none;
  }
}

@media (max-width: 1150px) {
  .modern-access-map-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 760px) {
  .modern-access-map-board {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--modern-space-6);
  }
  .modern-access-map-links,
  .modern-access-map-gutter {
    display: none;
  }
}
</style>
