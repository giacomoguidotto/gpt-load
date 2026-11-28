<script setup lang="ts">
import { ArrowDownRight, ArrowUpRight, TriangleAlert } from '@lucide/vue'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { getDashboard } from '@modern/api/dashboard'
import type { GroupRow } from '@modern/api/groups'
import type { HomeKey } from '@modern/api/home'
import { getUsage, type UsageItem, type UsageReport } from '@modern/api/usage'
import {
  AppButton,
  AppIcon,
  AppPanel,
  AppSegmentedControl,
  AppSparkline,
  AppStackedBars,
} from '@modern/components/ui'
import { dateFormatter, numberFormatter } from '@modern/components/ui/intl-formatters'
import {
  formatCompactNumber,
  formatNanoUSD,
  formatRelativeInstant,
} from '@modern/components/ui/format'
import { useClock } from '@modern/components/ui/clock'
import { useApiClient } from '@shared/http/client-context'
import HomeRunway from './HomeRunway.vue'
import HomeSectionLink from './HomeSectionLink.vue'

const props = defineProps<{
  groups: ReadonlyMap<number, GroupRow>
  keys: readonly HomeKey[]
  admin: boolean
}>()

const { t, locale } = useI18n()
const client = useApiClient()
const now = useClock()

const ranges = { '1h': 60 * 60 * 1000, '24h': 24 * 60 * 60 * 1000, '7d': 7 * 24 * 60 * 60 * 1000 }
type Range = keyof typeof ranges
const range = ref<Range>('24h')
// The window is fixed when it is chosen or refreshed, so every panel and the
// comparison period describe the same moment.
const anchor = ref(Math.floor(Date.now() / 60000) * 60000)
const window = computed(() => ({ from: anchor.value - ranges[range.value], to: anchor.value }))
const previous = computed(() => ({
  from: window.value.from - ranges[range.value],
  to: window.value.from,
}))

const usage = useQuery({
  queryKey: computed(() => ['modern', 'dashboard', 'usage', window.value.from, window.value.to]),
  queryFn: ({ signal }) =>
    getUsage(
      client,
      { from_ms: String(window.value.from), to_ms: String(window.value.to) },
      signal,
    ),
})
const prior = useQuery({
  queryKey: computed(() => [
    'modern',
    'dashboard',
    'usage',
    previous.value.from,
    previous.value.to,
  ]),
  queryFn: ({ signal }) =>
    getUsage(
      client,
      { from_ms: String(previous.value.from), to_ms: String(previous.value.to) },
      signal,
    ),
})
const performance = useQuery({
  queryKey: computed(() => [
    'modern',
    'dashboard',
    'performance',
    window.value.from,
    window.value.to,
  ]),
  queryFn: ({ signal }) => getDashboard(client, window.value, signal),
})
const runway = ref<InstanceType<typeof HomeRunway>>()

async function refresh(): Promise<void> {
  anchor.value = Math.floor(Date.now() / 60000) * 60000
  await Promise.all([
    usage.refetch(),
    prior.refetch(),
    performance.refetch(),
    runway.value?.refetch(),
  ])
}
function selectRange(value: string): void {
  range.value = value as Range
  anchor.value = Math.floor(Date.now() / 60000) * 60000
}
defineExpose({
  refresh,
  pending: computed(
    () => usage.isFetching.value || prior.isFetching.value || performance.isFetching.value,
  ),
  updatedAt: computed(() => performance.data.value?.observedAt),
})

/* Formatting */
const integerFormat = computed(() => numberFormatter(locale.value, { maximumFractionDigits: 0 }))
const percentFormat = computed(() =>
  numberFormatter(locale.value, { style: 'percent', maximumFractionDigits: 1 }),
)
function compact(value: number): string {
  return value < 10_000
    ? integerFormat.value.format(value)
    : formatCompactNumber(value, locale.value)
}
function milliseconds(value: number | null | undefined): string {
  if (value == null) return '—'
  return value < 1000
    ? `${integerFormat.value.format(value)} ms`
    : `${numberFormatter(locale.value, { maximumFractionDigits: value < 10_000 ? 2 : 1 }).format(value / 1000)} s`
}
function cost(value: string | undefined): string {
  return value === undefined ? '—' : formatNanoUSD(value, locale.value, 'narrowSymbol', 2)
}
function bucketLabel(start: number, end: number): string {
  const format = dateFormatter(locale.value, {
    month: range.value === '7d' ? 'short' : undefined,
    day: range.value === '7d' ? 'numeric' : undefined,
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  return `${format.format(start)} – ${format.format(end)}`
}
function humanize(code: string): string {
  if (!code) return t('home.dashboard.unknownError')
  const words = code.replace(/[_-]+/gu, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/* KPIs */
interface Kpi {
  id: string
  label: string
  value: string
  detail?: string
  delta?: { text: string; direction: 'up' | 'down'; tone: 'good' | 'bad' | 'neutral' }
  series?: number[]
  tone?: 'accent' | 'info' | 'cost'
}
function change(
  current: number,
  before: number | undefined,
  better: 'up' | 'down' | 'none',
): Kpi['delta'] {
  if (before === undefined || before === 0 || current === before) return undefined
  const ratio = (current - before) / before
  const direction = ratio > 0 ? 'up' : 'down'
  return {
    text: percentFormat.value.format(Math.abs(ratio)),
    direction,
    tone: better === 'none' ? 'neutral' : direction === better ? 'good' : 'bad',
  }
}
const successRate = (report?: UsageReport) =>
  report && report.summary.request_count
    ? report.summary.success_count / report.summary.request_count
    : undefined
const kpis = computed<Kpi[]>(() => {
  const current = usage.data.value
  const before = prior.data.value
  const latency = performance.data.value?.latency
  const series = current?.series ?? []
  const rate = successRate(current)
  const beforeRate = successRate(before)
  return [
    {
      id: 'requests',
      label: t('home.dashboard.kpi.requests'),
      value: current ? compact(current.summary.request_count) : '—',
      delta: current
        ? change(current.summary.request_count, before?.summary.request_count, 'none')
        : undefined,
      series: series.map((point) => point.request_count),
      tone: 'accent',
    },
    {
      id: 'success',
      label: t('home.dashboard.kpi.success'),
      value: rate === undefined ? '—' : percentFormat.value.format(rate),
      detail: current
        ? t('home.dashboard.kpi.failures', { count: compact(current.summary.failure_count) })
        : undefined,
      delta:
        rate !== undefined && beforeRate !== undefined && Math.abs(rate - beforeRate) >= 0.0005
          ? {
              text: t('home.dashboard.kpi.points', {
                value: numberFormatter(locale.value, { maximumFractionDigits: 1 }).format(
                  Math.abs(rate - beforeRate) * 100,
                ),
              }),
              direction: rate > beforeRate ? 'up' : 'down',
              tone: rate > beforeRate ? 'good' : 'bad',
            }
          : undefined,
      series: series.map((point) =>
        point.request_count ? point.success_count / point.request_count : 1,
      ),
      tone: 'info',
    },
    {
      id: 'latency',
      label: t('home.dashboard.kpi.latency'),
      value: milliseconds(latency?.samples ? latency.p95 : null),
      detail: latency?.samples
        ? t('home.dashboard.kpi.median', { value: milliseconds(latency.p50) })
        : undefined,
    },
    {
      id: 'ttft',
      label: t('home.dashboard.kpi.firstToken'),
      value: milliseconds(latency?.firstResponseP50),
      detail:
        latency?.firstResponseP95 != null
          ? t('home.dashboard.kpi.p95', { value: milliseconds(latency.firstResponseP95) })
          : undefined,
    },
    {
      id: 'tokens',
      label: t('home.dashboard.kpi.tokens'),
      value: current ? compact(current.summary.total_tokens) : '—',
      detail: current
        ? t('home.dashboard.kpi.cached', {
            value: percentFormat.value.format(
              current.summary.total_tokens
                ? current.summary.cache_read_tokens / current.summary.total_tokens
                : 0,
            ),
          })
        : undefined,
      delta: current
        ? change(current.summary.total_tokens, before?.summary.total_tokens, 'none')
        : undefined,
      series: series.map((point) => point.total_tokens),
      tone: 'info',
    },
    {
      id: 'cost',
      label: t('home.dashboard.kpi.cost'),
      value: cost(current?.summary.estimated_cost_nano_usd),
      delta: current
        ? change(
            Number(current.summary.estimated_cost_nano_usd),
            before ? Number(before.summary.estimated_cost_nano_usd) : undefined,
            'down',
          )
        : undefined,
      series: series.map((point) => Number(point.estimated_cost_nano_usd)),
      tone: 'cost',
    },
  ]
})

/* Panels */
const traffic = computed(() =>
  (usage.data.value?.series ?? []).map((point) => ({
    label: `${bucketLabel(point.bucket_start_ms, point.bucket_end_ms)} · ${t(
      'home.dashboard.trafficPoint',
      {
        ok: compact(point.success_count),
        failed: compact(point.failure_count),
      },
    )}`,
    values: [point.success_count, point.failure_count],
  })),
)
const errorTotal = computed(
  () => performance.data.value?.errors.reduce((sum, cause) => sum + cause.count, 0) ?? 0,
)
const errorCauses = computed(() => performance.data.value?.errors.slice(0, 6) ?? [])
const keyNames = computed(() => new Map(props.keys.map((key) => [key.id, key.name])))
function leaders(items: UsageItem[] | undefined, by: 'cost' | 'tokens') {
  const rows = (items ?? []).slice(0, 5)
  const value = (item: UsageItem) =>
    by === 'cost' ? Number(item.estimated_cost_nano_usd) : item.total_tokens
  const peak = Math.max(1, ...rows.map(value))
  return rows.map((item) => ({ item, share: value(item) / peak }))
}
const topUsers = computed(() =>
  leaders(usage.data.value?.distributions.access_key?.cost.items, 'cost'),
)
const topModels = computed(() =>
  leaders(usage.data.value?.distributions.model?.tokens.items, 'tokens'),
)
const slowestPeak = computed(() =>
  Math.max(1, ...(performance.data.value?.models.map((model) => model.p95) ?? [])),
)
const rangeOptions = computed(() =>
  (Object.keys(ranges) as Range[]).map((value) => ({
    value,
    label: t(`home.dashboard.range.${value}`),
  })),
)
const failed = computed(() => usage.isError.value || performance.isError.value)
</script>

<template>
  <section class="modern-dashboard" :aria-label="t('home.dashboard.title')">
    <header class="modern-dashboard-toolbar">
      <div class="modern-dashboard-heading">
        <span class="modern-dashboard-live" aria-hidden="true" />
        <strong>{{ t('home.dashboard.title') }}</strong>
        <span>{{
          t('home.dashboard.window', {
            from: dateFormatter(locale, {
              month: 'short',
              day: 'numeric',
              hour: '2-digit',
              minute: '2-digit',
              hour12: false,
            }).format(window.from),
          })
        }}</span>
      </div>
      <AppSegmentedControl
        :label="t('home.dashboard.rangeLabel')"
        :model-value="range"
        :options="rangeOptions"
        size="sm"
        @update:model-value="selectRange"
      />
    </header>

    <div v-if="failed" class="modern-dashboard-failed" role="status">
      <AppIcon :icon="TriangleAlert" size="sm" />
      <span>{{ t('home.dashboard.failed') }}</span>
      <AppButton size="xs" variant="text" @click="refresh">{{ t('ui.retry') }}</AppButton>
    </div>

    <div class="modern-dashboard-kpis">
      <article
        v-for="(kpi, index) in kpis"
        :key="kpi.id"
        class="modern-dashboard-kpi"
        :style="{ '--i': index }"
      >
        <span class="modern-dashboard-kpi-label">{{ kpi.label }}</span>
        <strong class="modern-dashboard-kpi-value">{{ kpi.value }}</strong>
        <span class="modern-dashboard-kpi-meta">
          <span
            v-if="kpi.delta"
            class="modern-dashboard-delta"
            :class="`is-${kpi.delta.tone}`"
            :aria-label="t('home.dashboard.vsPrevious', { value: kpi.delta.text })"
          >
            <AppIcon
              :icon="kpi.delta.direction === 'up' ? ArrowUpRight : ArrowDownRight"
              size="xs"
            />{{ kpi.delta.text }}
          </span>
          <span v-if="kpi.detail">{{ kpi.detail }}</span>
        </span>
        <AppSparkline
          v-if="kpi.series && kpi.series.length > 1"
          class="modern-dashboard-kpi-spark"
          :label="kpi.label"
          :values="kpi.series"
          :tone="kpi.tone"
          size="sm"
        />
      </article>
    </div>

    <div class="modern-dashboard-grid">
      <AppPanel class="modern-dashboard-traffic" :title="t('home.dashboard.traffic')" compact>
        <template #actions>
          <HomeSectionLink :to="{ name: 'modern-usage' }" :label="t('pages.usage.title')" />
        </template>
        <AppStackedBars
          v-if="traffic.length"
          :label="t('home.dashboard.traffic')"
          :bars="traffic"
          :tones="['success', 'danger']"
          :legend="[t('home.dashboard.succeeded'), t('home.dashboard.failedRequests')]"
        />
        <p v-else class="modern-dashboard-empty">{{ t('home.dashboard.noTraffic') }}</p>
      </AppPanel>

      <AppPanel :title="t('home.dashboard.errors')" compact>
        <template #actions>
          <HomeSectionLink
            :to="{ name: 'modern-logs', query: { status: 'error' } }"
            :label="t('pages.logs.title')"
          />
        </template>
        <p v-if="performance.data.value && !errorCauses.length" class="modern-dashboard-empty">
          {{ t('home.dashboard.noErrors') }}
        </p>
        <ul v-else class="modern-dashboard-bars">
          <li
            v-for="(cause, index) in errorCauses"
            :key="cause.status + cause.errorCode"
            :style="{ '--i': index }"
          >
            <span class="modern-dashboard-bar-label">
              <span>{{ humanize(cause.errorCode) }}</span>
              <span
                >{{ compact(cause.count) }} ·
                {{ percentFormat.format(cause.count / errorTotal) }}</span
              >
            </span>
            <span class="modern-dashboard-bar is-danger"
              ><span :style="{ '--fill': cause.count / (errorCauses[0]?.count || 1) }"
            /></span>
          </li>
        </ul>
        <p v-if="performance.data.value?.errorsSampled" class="modern-dashboard-note">
          {{ t('home.dashboard.errorsSampled') }}
        </p>
      </AppPanel>

      <AppPanel :title="t('home.dashboard.latency')" compact>
        <p
          v-if="performance.data.value && !performance.data.value.models.length"
          class="modern-dashboard-empty"
        >
          {{ t('home.dashboard.noTraffic') }}
        </p>
        <div v-else class="modern-dashboard-table" role="table">
          <div class="modern-dashboard-row is-head" role="row">
            <span role="columnheader">{{ t('home.dashboard.model') }}</span>
            <span role="columnheader">p50</span>
            <span role="columnheader">p95</span>
            <span role="columnheader">{{ t('home.dashboard.firstToken') }}</span>
          </div>
          <div
            v-for="(model, index) in performance.data.value?.models ?? []"
            :key="model.model"
            class="modern-dashboard-row"
            role="row"
            :style="{ '--i': index }"
          >
            <span role="cell" class="modern-dashboard-model">
              <span>{{ model.model }}</span>
              <span class="modern-dashboard-bar is-info"
                ><span :style="{ '--fill': model.p95 / slowestPeak }"
              /></span>
            </span>
            <span role="cell">{{ milliseconds(model.p50) }}</span>
            <span role="cell">{{ milliseconds(model.p95) }}</span>
            <span role="cell">{{ milliseconds(model.firstResponseP50) }}</span>
          </div>
        </div>
        <p v-if="performance.data.value?.latency.samples" class="modern-dashboard-note">
          {{
            t('home.dashboard.latencySample', {
              count: compact(performance.data.value.latency.samples),
            })
          }}
        </p>
      </AppPanel>

      <HomeRunway ref="runway" :groups="groups" />

      <AppPanel v-if="admin" :title="t('home.dashboard.topUsers')" compact>
        <template #actions>
          <HomeSectionLink
            :to="{ name: 'modern-access-keys' }"
            :label="t('pages.accessKeys.title')"
          />
        </template>
        <p v-if="usage.data.value && !topUsers.length" class="modern-dashboard-empty">
          {{ t('home.dashboard.noTraffic') }}
        </p>
        <ul class="modern-dashboard-bars">
          <li
            v-for="({ item, share }, index) in topUsers"
            :key="item.access_key_id"
            :style="{ '--i': index }"
          >
            <span class="modern-dashboard-bar-label">
              <span>{{ keyNames.get(item.access_key_id ?? 0) ?? `#${item.access_key_id}` }}</span>
              <span
                >{{ cost(item.estimated_cost_nano_usd) }} · {{ compact(item.request_count) }}
                {{ t('home.dashboard.req') }}</span
              >
            </span>
            <span class="modern-dashboard-bar is-accent"
              ><span :style="{ '--fill': share }"
            /></span>
          </li>
        </ul>
      </AppPanel>

      <AppPanel :title="t('home.dashboard.topModels')" compact>
        <template #actions>
          <HomeSectionLink :to="{ name: 'modern-models' }" :label="t('pages.models.title')" />
        </template>
        <p v-if="usage.data.value && !topModels.length" class="modern-dashboard-empty">
          {{ t('home.dashboard.noTraffic') }}
        </p>
        <ul class="modern-dashboard-bars">
          <li
            v-for="({ item, share }, index) in topModels"
            :key="item.model"
            :style="{ '--i': index }"
          >
            <span class="modern-dashboard-bar-label">
              <span>{{ item.model }}</span>
              <span
                >{{ compact(item.total_tokens) }} {{ t('home.dashboard.tok') }} ·
                {{ cost(item.estimated_cost_nano_usd) }}</span
              >
            </span>
            <span class="modern-dashboard-bar is-info"><span :style="{ '--fill': share }" /></span>
          </li>
        </ul>
      </AppPanel>

      <AppPanel class="modern-dashboard-recent" :title="t('home.dashboard.recent')" compact>
        <template #actions>
          <HomeSectionLink
            :to="{ name: 'modern-logs', query: { status: 'error' } }"
            :label="t('home.dashboard.allFailures')"
          />
        </template>
        <p
          v-if="performance.data.value && !performance.data.value.recent.length"
          class="modern-dashboard-empty"
        >
          {{ t('home.dashboard.noErrors') }}
        </p>
        <ul class="modern-dashboard-feed">
          <li
            v-for="(failure, index) in performance.data.value?.recent ?? []"
            :key="failure.requestId"
            :style="{ '--i': index }"
          >
            <RouterLink
              class="modern-dashboard-feed-item"
              :to="{ name: 'modern-logs', query: { request_id: failure.requestId } }"
            >
              <span class="modern-dashboard-feed-code">{{
                failure.statusCode || failure.status
              }}</span>
              <span class="modern-dashboard-feed-main">
                <strong>{{ humanize(failure.errorCode) }}</strong>
                <span>{{ failure.errorSummary || failure.model }}</span>
              </span>
              <span class="modern-dashboard-feed-meta">
                <span>{{ failure.model }}</span>
                <span
                  >{{ groups.get(failure.groupId)?.name ?? '—' }} ·
                  {{ keyNames.get(failure.accessKeyId) ?? '—' }}</span
                >
              </span>
              <time :datetime="new Date(failure.completedAt).toISOString()">{{
                formatRelativeInstant(failure.completedAt, now, locale)
              }}</time>
            </RouterLink>
          </li>
        </ul>
      </AppPanel>
    </div>
  </section>
</template>

<style scoped>
.modern-dashboard {
  display: grid;
  gap: var(--modern-space-4);
  min-width: 0;
}
.modern-dashboard-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--modern-space-3);
}
.modern-dashboard-heading {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-dashboard-heading strong {
  color: var(--modern-text);
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-semibold);
}
.modern-dashboard-live {
  width: var(--modern-space-2);
  height: var(--modern-space-2);
  border-radius: var(--modern-radius-round);
  background: var(--modern-status-success);
  box-shadow: var(--modern-shadow-focus);
  animation: modern-dashboard-pulse var(--modern-motion-loading) var(--modern-motion-ease) infinite
    alternate;
}
.modern-dashboard-failed {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  border-radius: var(--modern-radius-control);
  background: var(--modern-warning-soft);
  padding: var(--modern-space-2) var(--modern-space-3);
  color: var(--modern-warning);
  font-size: var(--modern-font-size-small);
}

/* KPI tiles */
.modern-dashboard-kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(var(--modern-kpi-min), 1fr));
  gap: var(--modern-space-3);
}
.modern-dashboard-kpi {
  position: relative;
  display: grid;
  align-content: start;
  gap: var(--modern-space-1);
  overflow: hidden;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-panel);
  background: var(--modern-surface);
  box-shadow: var(--modern-shadow-panel);
  padding: var(--modern-space-4) var(--modern-space-4) var(--modern-space-3);
  animation: modern-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 1.5);
  transition:
    transform var(--modern-motion-medium) var(--modern-motion-ease-spring),
    box-shadow var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-dashboard-kpi:hover {
  transform: translateY(var(--modern-motion-lift));
  box-shadow: var(--modern-shadow-lift);
}
.modern-dashboard-kpi-label {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-dashboard-kpi-value {
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  letter-spacing: var(--modern-tracking-title);
  line-height: var(--modern-leading-title);
  font-variant-numeric: tabular-nums;
}
.modern-dashboard-kpi-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--modern-space-2);
  min-height: var(--modern-control-xxs);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}
.modern-dashboard-kpi-spark {
  margin-top: var(--modern-space-1);
}
.modern-dashboard-delta {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-0-5);
  border-radius: var(--modern-radius-small);
  padding: 0 var(--modern-space-1-5);
  background: var(--modern-subtle);
  color: var(--modern-muted);
  font-weight: var(--modern-weight-medium);
  font-variant-numeric: tabular-nums;
}
.modern-dashboard-delta.is-good {
  background: var(--modern-success-soft);
  color: var(--modern-success);
}
.modern-dashboard-delta.is-bad {
  background: var(--modern-danger-soft);
  color: var(--modern-danger);
}

/* Panel grid */
.modern-dashboard-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--modern-space-4);
  /* Panels in a row share its height, so the grid reads as one surface. */
  align-items: stretch;
}
.modern-dashboard-traffic,
.modern-dashboard-recent {
  grid-column: span 2;
}
.modern-dashboard-empty,
.modern-dashboard-note {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-dashboard-note {
  margin-top: var(--modern-space-3);
  font-size: var(--modern-font-size-caption);
}

/* Horizontal bars */
.modern-dashboard-bars {
  display: grid;
  gap: var(--modern-space-3);
  margin: 0;
  padding: 0;
  list-style: none;
}
.modern-dashboard-bars > li {
  display: grid;
  gap: var(--modern-space-1);
  animation: modern-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 2);
}
.modern-dashboard-bar-label {
  display: flex;
  justify-content: space-between;
  gap: var(--modern-space-3);
  font-size: var(--modern-font-size-small);
}
.modern-dashboard-bar-label > :first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-dashboard-bar-label > :last-child {
  flex: none;
  color: var(--modern-muted);
  font-variant-numeric: tabular-nums;
}
.modern-dashboard-bar {
  display: block;
  height: var(--modern-progress-bar-sm);
  overflow: hidden;
  border-radius: var(--modern-radius-small);
  background: var(--modern-progress-track);
}
.modern-dashboard-bar > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--modern-accent);
  transform: scaleX(var(--fill, 0));
  transform-origin: left center;
  animation: modern-dashboard-fill var(--modern-motion-draw) var(--modern-motion-ease-out) both;
  transition: transform var(--modern-motion-slow) var(--modern-motion-ease-spring);
}
.modern-dashboard-bar.is-danger > span {
  background: var(--modern-status-danger);
}
.modern-dashboard-bar.is-info > span {
  background: var(--modern-chart-input);
}

/* Latency table */
.modern-dashboard-table {
  display: grid;
}
.modern-dashboard-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) repeat(3, auto);
  align-items: center;
  gap: var(--modern-space-3);
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  padding: var(--modern-space-2) 0;
  font-size: var(--modern-font-size-small);
  font-variant-numeric: tabular-nums;
  animation: modern-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 2);
}
.modern-dashboard-row > :not(:first-child) {
  min-width: var(--modern-control-nav);
  text-align: right;
}
.modern-dashboard-row.is-head {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  animation: none;
}
.modern-dashboard-model {
  display: grid;
  gap: var(--modern-space-1);
  min-width: 0;
}
.modern-dashboard-model > :first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Failure feed */
.modern-dashboard-feed {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}
.modern-dashboard-feed > li {
  animation: modern-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 2);
}
.modern-dashboard-feed-item {
  display: grid;
  grid-template-columns: auto minmax(0, 2fr) minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--modern-space-3);
  border-bottom: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-control);
  padding: var(--modern-space-2);
  color: inherit;
  text-decoration: none;
  transition: background var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-dashboard-feed-item:hover {
  background: var(--modern-subtle);
}
.modern-dashboard-feed-code {
  border-radius: var(--modern-radius-small);
  background: var(--modern-danger-soft);
  padding: 0 var(--modern-space-2);
  color: var(--modern-danger);
  font-family: var(--modern-font-mono);
  font-size: var(--modern-font-size-caption);
  line-height: var(--modern-control-xxs);
}
.modern-dashboard-feed-main,
.modern-dashboard-feed-meta {
  display: grid;
  min-width: 0;
  font-size: var(--modern-font-size-small);
}
.modern-dashboard-feed-main > *,
.modern-dashboard-feed-meta > * {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-dashboard-feed-main strong {
  font-weight: var(--modern-weight-medium);
}
.modern-dashboard-feed-main > span,
.modern-dashboard-feed-meta,
.modern-dashboard-feed-item time {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}

@keyframes modern-dashboard-fill {
  from {
    transform: scaleX(0);
  }
}
@keyframes modern-dashboard-pulse {
  to {
    box-shadow: none;
  }
}

@media (max-width: 1150px) {
  .modern-dashboard-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .modern-dashboard-traffic,
  .modern-dashboard-recent {
    grid-column: 1 / -1;
  }
}
@media (max-width: 760px) {
  .modern-dashboard-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .modern-dashboard-feed-item {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }
  .modern-dashboard-feed-meta {
    display: none;
  }
}
</style>
