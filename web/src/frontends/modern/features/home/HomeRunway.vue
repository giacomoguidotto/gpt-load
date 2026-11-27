<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { capacityKey, getCapacity } from '@modern/api/capacity'
import type { GroupRow } from '@modern/api/groups'
import { AppButton, AppChannelIcon, AppPanel, AppProgressBar } from '@modern/components/ui'
import { useClock } from '@modern/components/ui/clock'
import { formatRemainingDuration } from '@modern/components/ui/format'
import { buildAccessMap, type MapAccount } from '@modern/features/access-map/access-map-model'
import { useApiClient } from '@shared/http/client-context'
import HomeSectionLink from './HomeSectionLink.vue'

const props = defineProps<{ groups: ReadonlyMap<number, GroupRow> }>()
const { t, n, locale } = useI18n()
const client = useApiClient()
const now = useClock()
const capacity = useQuery({
  queryKey: capacityKey,
  queryFn: ({ signal }) => getCapacity(client, signal),
})
defineExpose({ refetch: () => capacity.refetch() })

const map = computed(() =>
  capacity.data.value
    ? buildAccessMap([...props.groups.values()], [], capacity.data.value.accounts)
    : undefined,
)
const metered = computed(
  () => map.value?.accounts.filter((account) => account.runway.kind !== 'unmetered') ?? [],
)
const available = computed(
  () =>
    metered.value.filter(
      (account) => account.runway.kind !== 'exhausted' && account.runway.kind !== 'offline',
    ).length,
)
// The accounts that run out soonest, then those already used up that come
// back soonest: the two things worth acting on.
const soonest = computed(() => {
  const at = (account: MapAccount) =>
    account.runway.kind === 'runs-out' || account.runway.kind === 'exhausted'
      ? (account.runway.at ?? Number.MAX_SAFE_INTEGER)
      : Number.MAX_SAFE_INTEGER
  const runningOut = metered.value
    .filter((account) => account.runway.kind === 'runs-out')
    .sort((a, b) => at(a) - at(b))
  return runningOut.slice(0, 4)
})
function runwayText(account: MapAccount): string {
  if (account.runway.kind !== 'runs-out') return ''
  return account.runway.at <= now.value
    ? t('accessMap.runway.runsOutNow')
    : t('accessMap.runway.runsOut', {
        time: formatRemainingDuration(account.runway.at - now.value, locale.value),
      })
}
</script>

<template>
  <AppPanel :title="t('home.runway.title')" compact>
    <template #actions>
      <HomeSectionLink :to="{ name: 'modern-access-map' }" :label="t('pages.accessMap.title')" />
    </template>
    <div v-if="capacity.isError.value && !map" class="modern-home-runway-state" role="status">
      <span>{{ t('home.runway.failed') }}</span>
      <AppButton size="xs" variant="text" @click="capacity.refetch()">{{
        t('ui.retry')
      }}</AppButton>
    </div>
    <p v-else-if="!map" class="modern-home-runway-state">{{ t('ui.loading') }}</p>
    <p v-else-if="!metered.length" class="modern-home-runway-state">
      {{ t('home.runway.none') }}
    </p>
    <div v-else class="modern-home-runway">
      <div class="modern-home-runway-total">
        <strong
          >{{ n(available) }}<small>/{{ n(metered.length) }}</small></strong
        >
        <span>{{ t('home.runway.available') }}</span>
      </div>
      <AppProgressBar
        size="sm"
        :label="t('home.runway.available')"
        :value="(available / metered.length) * 100"
        :tone="
          available / metered.length < 0.3
            ? 'danger'
            : available / metered.length < 0.6
              ? 'warning'
              : 'success'
        "
      />
      <p v-if="!soonest.length" class="modern-home-runway-state">
        {{ t('accessMap.summary.nextNone') }}
      </p>
      <ul v-else class="modern-home-runway-list">
        <li v-for="(account, index) in soonest" :key="account.key" :style="{ '--i': index }">
          <AppChannelIcon
            :icon="groups.get(account.poolId)?.channelIcon"
            :name="groups.get(account.poolId)?.channelName"
            size="sm"
          />
          <span class="modern-home-runway-name">
            <strong>{{ account.source.credential.label }}</strong>
            <span>{{ groups.get(account.poolId)?.name }}</span>
          </span>
          <span class="modern-home-runway-when">{{ runwayText(account) }}</span>
        </li>
      </ul>
    </div>
  </AppPanel>
</template>

<style scoped>
.modern-home-runway {
  display: grid;
  gap: var(--modern-space-3);
}
.modern-home-runway-total {
  display: flex;
  align-items: baseline;
  gap: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-home-runway-total strong {
  color: var(--modern-text);
  font-size: var(--modern-font-size-title);
  font-weight: var(--modern-weight-semibold);
  letter-spacing: var(--modern-tracking-title);
  font-variant-numeric: tabular-nums;
}
.modern-home-runway-total small {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-section);
  font-weight: var(--modern-weight-regular);
}
.modern-home-runway-list {
  display: grid;
  gap: var(--modern-space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.modern-home-runway-list > li {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--modern-space-2);
  animation: modern-rise var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) * 2);
}
.modern-home-runway-name {
  display: grid;
  min-width: 0;
  font-size: var(--modern-font-size-small);
}
.modern-home-runway-name > * {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-home-runway-name strong {
  font-weight: var(--modern-weight-medium);
}
.modern-home-runway-name span {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}
.modern-home-runway-when {
  border-radius: var(--modern-radius-small);
  background: var(--modern-warning-soft);
  padding: 0 var(--modern-space-2);
  color: var(--modern-warning);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-medium);
  line-height: var(--modern-control-xxs);
  white-space: nowrap;
}
.modern-home-runway-state {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--modern-space-2);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
</style>
