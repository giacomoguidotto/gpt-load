<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue'
import { onBeforeUnmount, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from './AppButton.vue'
import AppIcon from './AppIcon.vue'

// A load that outlives this delay is no longer normal latency: say so, so a
// stalled server is not mistaken for a page that is still working.
const slowLoadingDelayMS = 8000

const props = defineProps<{
  title: string
  description?: string
  icon?: Component
  loading?: boolean
  error?: boolean
  onRetry?: () => unknown
}>()
const { t } = useI18n()
const slow = ref(false)
let slowTimer: ReturnType<typeof setTimeout> | undefined

watch(
  () => props.loading,
  (loading) => {
    clearTimeout(slowTimer)
    slow.value = false
    if (loading) slowTimer = setTimeout(() => (slow.value = true), slowLoadingDelayMS)
  },
  { immediate: true },
)
onBeforeUnmount(() => clearTimeout(slowTimer))
</script>

<template>
  <div class="modern-collection-state" :role="error ? 'alert' : 'status'">
    <span v-if="loading || icon" class="modern-collection-state-icon">
      <AppIcon
        v-if="loading || icon"
        :icon="loading ? LoaderCircle : icon!"
        size="lg"
        :class="{ 'modern-spin': loading }"
      />
    </span>
    <strong>{{ title }}</strong>
    <p v-if="description">{{ description }}</p>
    <p v-if="loading && slow">{{ t('ui.loadingSlow') }}</p>
    <div v-if="$slots.default" class="modern-collection-state-actions"><slot /></div>
    <div v-else-if="loading && slow && onRetry" class="modern-collection-state-actions">
      <AppButton @click="onRetry()">{{ t('ui.retry') }}</AppButton>
    </div>
  </div>
</template>

<style scoped>
.modern-collection-state {
  display: grid;
  justify-items: center;
  gap: var(--modern-space-3);
  padding: var(--modern-space-12) var(--modern-space-6);
  text-align: center;
  color: var(--modern-muted);
}
.modern-collection-state strong {
  color: var(--modern-text);
  font-size: var(--modern-font-size-body);
  font-weight: var(--modern-weight-medium);
}
.modern-collection-state-icon {
  display: inline-flex;
  width: var(--modern-touch-target);
  height: var(--modern-touch-target);
  align-items: center;
  justify-content: center;
  border-radius: var(--modern-radius-panel);
  background: var(--modern-subtle);
  margin-bottom: var(--modern-space-1);
}
.modern-collection-state p {
  max-width: 48ch;
  font-size: var(--modern-font-size-secondary);
}
.modern-collection-state-actions {
  margin-top: var(--modern-space-2);
}
</style>
