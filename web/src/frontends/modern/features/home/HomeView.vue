<script setup lang="ts">
import { useMobileLayout } from '@modern/app/use-mobile-layout'
import { DialogRoot } from 'reka-ui'
import { Cable, Route as RouteIcon } from '@lucide/vue'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getHome } from '@modern/api/home'
import { getGroupWorkspace, groupQueryKey } from '@modern/api/groups'
import { getHealth } from '@modern/api/health'
import { getLogAccessKeys } from '@modern/api/logs'
import { useApiClient } from '@shared/http/client-context'
import { useAuthSession } from '@modern/features/auth/auth-session'
import { usePageRefresh } from '@modern/app/page-refresh'
import {
  AppButton,
  AppCollectionState,
  AppNotice,
  AppDialogContent,
  AppDialogHeader,
} from '@modern/components/ui'
import RouteInspector from '@modern/features/inspector/RouteInspector.vue'
import { compareHealthIssues, healthIssues } from '@modern/features/health/health-display'
import HomeAccessKey from './HomeAccessKey.vue'
import HomeAttention from './HomeAttention.vue'
import HomeSetupGuide from './HomeSetupGuide.vue'
import HomeStatusBar from './HomeStatusBar.vue'
import HomeDashboard from './HomeDashboard.vue'
import GatewayConnection from './GatewayConnection.vue'

const { t, locale } = useI18n()
const isMobile = useMobileLayout()
const connectionOpen = ref(false)
const client = useApiClient()
const session = useAuthSession()
const route = useRoute()
function closeMobileTool(): void {
  connectionOpen.value = false
  inspectorOpen.value = false
}
const inspector = ref<InstanceType<typeof RouteInspector>>()
const admin = computed(() => session.state.principalType === 'admin')
const baseQuery = useQuery({
  queryKey: ['modern', 'home', 'base'],
  queryFn: ({ signal }) => getHome(client, signal),
})
const base = computed(() => baseQuery.data.value)
const groups = useQuery({
  queryKey: groupQueryKey,
  queryFn: ({ signal }) => getGroupWorkspace(client, signal),
  enabled: admin,
})
const groupMap = computed(() => new Map(groups.data.value?.items.map((group) => [group.id, group])))
const keys = useQuery({
  queryKey: ['modern', 'log-access-key-options'],
  queryFn: ({ signal }) => getLogAccessKeys(client, signal),
  enabled: admin,
})
const dashboard = ref<InstanceType<typeof HomeDashboard>>()
const health = useQuery({
  queryKey: ['modern', 'health'],
  queryFn: ({ signal }) => getHealth(client, signal),
  enabled: admin,
})
// 初始化只看配置是否齐全，不把停用、冷却等运行状态当成尚未配置。
const showSetup = computed(() => {
  if (!admin.value || !groups.data.value || !keys.data.value) return false
  return (
    !groups.data.value.items.some((group) => group.credentials.total > 0 && group.modelCount > 0) ||
    !keys.data.value.length
  )
})
const emptyProject = computed(
  () => admin.value && groups.data.value?.items.length === 0 && keys.data.value?.length === 0,
)
const attentionIssues = computed(() => {
  if (!admin.value || !health.data.value || !groups.data.value) return undefined
  return healthIssues(health.data.value, groups.data.value.items, (key, values) =>
    t(key, values ?? {}),
  ).sort((left, right) => compareHealthIssues(left, right, locale.value))
})
const attention = computed(() => attentionIssues.value?.length ?? 0)
// 深链接 /monitor/inspector 会重定向到首页并带上 inspect_* 参数，那种情况直接展开。
const inspectorOpen = ref(
  route.hash === '#route-inspector' ||
    Object.keys(route.query).some((key) => key.startsWith('inspect_')),
)
async function refreshOptions(): Promise<void> {
  await Promise.all([groups.refetch(), keys.refetch()])
}
async function refreshHealth(): Promise<void> {
  await Promise.all([health.refetch(), groups.refetch()])
}
async function refresh(): Promise<void> {
  await Promise.all([
    baseQuery.refetch(),
    dashboard.value?.refresh(),
    ...(admin.value ? [refreshOptions(), health.refetch(), inspector.value?.refresh()] : []),
  ])
}
usePageRefresh({
  refresh,
  pending: () =>
    baseQuery.isFetching.value ||
    Boolean(dashboard.value?.pending) ||
    groups.isFetching.value ||
    keys.isFetching.value ||
    health.isFetching.value ||
    Boolean(inspector.value?.pending),
  updatedAt: () =>
    Math.max(
      base.value?.observedAt ?? 0,
      dashboard.value?.updatedAt ?? 0,
      health.data.value?.observedAt ?? 0,
    ) || undefined,
})
// A link to #route-inspector opens the inspector dialog.
watch(
  () => route.hash,
  (hash) => {
    if (hash === '#route-inspector' && admin.value) inspectorOpen.value = true
  },
  { immediate: true },
)
</script>

<template>
  <div class="modern-home-workspace">
    <AppCollectionState
      v-if="!base"
      :loading="baseQuery.isPending.value"
      :error="baseQuery.isError.value"
      :title="t(baseQuery.isError.value ? 'home.baseFailed' : 'ui.loading')"
    >
      <AppButton v-if="baseQuery.isError.value" @click="refresh">{{ t('ui.retry') }}</AppButton>
    </AppCollectionState>
    <template v-else>
      <div v-if="baseQuery.isError.value" class="modern-home-error" role="status">
        <span>{{ t('home.baseFailed') }}</span>
        <AppButton size="xs" @click="baseQuery.refetch()">{{ t('ui.retry') }}</AppButton>
      </div>
      <HomeStatusBar :mobile="isMobile" :base="base" :admin="admin" :attention="attention" />
      <AppNotice v-if="admin && (groups.isError.value || keys.isError.value)" tone="warning">
        <div class="modern-home-options-error">
          <span>{{ t('home.setupFailed') }}</span>
          <AppButton size="xs" variant="text" @click="refreshOptions">{{
            t('ui.retry')
          }}</AppButton>
        </div>
      </AppNotice>
      <HomeSetupGuide
        v-if="showSetup && groups.data.value && keys.data.value"
        :groups="groups.data.value.items"
        :key-count="keys.data.value.length"
      />
      <div class="modern-home-tools">
        <AppButton :icon="Cable" size="sm" @click="connectionOpen = true">{{
          t('home.dashboard.connect')
        }}</AppButton>
        <AppButton v-if="admin" :icon="RouteIcon" size="sm" @click="inspectorOpen = true">{{
          t('home.dashboard.inspect')
        }}</AppButton>
      </div>
      <HomeAttention
        v-if="admin && (attention || health.isError.value || groups.isError.value)"
        :issues="attentionIssues"
        :groups="groupMap"
        :failed="health.isError.value || groups.isError.value"
        @retry="refreshHealth"
      />
      <HomeAccessKey v-if="!admin && base.currentKey" :row="base.currentKey" />
      <HomeDashboard ref="dashboard" :groups="groupMap" :keys="base.keys" :admin="admin" />
      <DialogRoot
        :open="connectionOpen || inspectorOpen"
        @update:open="
          (open) => {
            if (!open) {
              connectionOpen = false
              inspectorOpen = false
            }
          }
        "
      >
        <AppDialogContent
          placement="editor"
          :title="connectionOpen ? t('home.connection') : t('pages.inspector.title')"
          :description="connectionOpen ? t('home.connection') : t('pages.inspector.title')"
        >
          <AppDialogHeader
            :title="connectionOpen ? t('home.connection') : t('pages.inspector.title')"
            :close-label="t('ui.close')"
            @close="closeMobileTool"
          />
          <div class="modern-home-mobile-tool-body">
            <GatewayConnection v-if="connectionOpen" :keys="base.keys" :admin="admin" />
            <template v-else-if="admin">
              <p v-if="emptyProject" class="modern-home-inspector-empty">
                {{ t('home.setup.inspectorLater') }}
              </p>
              <RouteInspector
                v-else
                ref="inspector"
                :groups="groups.data.value"
                :groups-loading="groups.isFetching.value"
                :groups-failed="groups.isError.value"
                :access-keys="keys.data.value"
                :keys-loading="keys.isFetching.value"
                :keys-failed="keys.isError.value"
                @retry-options="refreshOptions"
              />
            </template>
          </div>
        </AppDialogContent>
      </DialogRoot>
    </template>
  </div>
</template>

<style scoped>
.modern-home-mobile {
  display: grid;
  gap: var(--modern-space-4);
  min-width: 0;
}
.modern-home-mobile-tools {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--modern-space-2);
}
.modern-home-mobile-tool-body {
  min-height: 0;
  overflow-y: auto;
  padding: var(--modern-space-3);
}

.modern-home-workspace {
  display: grid;
  flex: none;
  align-content: start;
  min-width: 0;
  gap: var(--modern-page-gap);
  padding-block: var(--modern-space-5);
}
.modern-home-error {
  display: flex;
  align-items: center;
  gap: var(--modern-space-3);
  color: var(--modern-danger);
  font-size: var(--modern-font-size-secondary);
}
.modern-home-options-error {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--modern-space-3);
}
.modern-home-tools {
  display: flex;
  flex-wrap: wrap;
  gap: var(--modern-space-2);
}
.modern-home-inspector-empty {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-secondary);
}
</style>
