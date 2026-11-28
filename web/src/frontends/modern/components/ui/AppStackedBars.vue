<script setup lang="ts">
import { computed, ref } from 'vue'

/**
 * A stacked bar chart for a time series. Each bar stacks its segments from
 * the bottom in order; segment colours come from `tones`. Bars grow in with a
 * stagger when the chart mounts.
 */
const props = defineProps<{
  label: string
  bars: readonly { label: string; values: readonly number[] }[]
  tones: readonly ('success' | 'danger' | 'warning' | 'info' | 'neutral')[]
  legend?: readonly string[]
}>()

const hovered = ref<number>()
const peak = computed(() =>
  Math.max(1, ...props.bars.map((bar) => bar.values.reduce((sum, value) => sum + value, 0))),
)
const gap = computed(() => (props.bars.length > 60 ? 0.15 : 0.3))
const width = computed(() => 100 / Math.max(props.bars.length, 1))

function segments(values: readonly number[]) {
  let offset = 0
  return values.map((value, index) => {
    const height = (value / peak.value) * 100
    const segment = { y: 100 - offset - height, height, tone: props.tones[index] ?? 'neutral' }
    offset += height
    return segment
  })
}
</script>

<template>
  <figure class="modern-stacked-bars" :aria-label="label">
    <svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" :aria-label="label">
      <g
        v-for="(bar, index) in bars"
        :key="index"
        class="modern-stacked-bars__bar"
        :class="{
          'is-hovered': hovered === index,
          'is-dimmed': hovered !== undefined && hovered !== index,
        }"
        :style="{ '--i': index }"
        @mouseenter="hovered = index"
        @mouseleave="hovered = undefined"
      >
        <rect
          class="modern-stacked-bars__hit"
          :x="index * width"
          y="0"
          :width="width"
          height="100"
        />
        <rect
          v-for="(segment, segmentIndex) in segments(bar.values)"
          :key="segmentIndex"
          :class="`tone-${segment.tone}`"
          :x="index * width + (width * gap) / 2"
          :y="segment.y"
          :width="width * (1 - gap)"
          :height="segment.height"
        />
      </g>
    </svg>
    <figcaption class="modern-stacked-bars__caption">
      <span v-if="hovered !== undefined" class="modern-stacked-bars__readout">{{
        bars[hovered]?.label
      }}</span>
      <span v-for="(name, index) in legend ?? []" :key="name" class="modern-stacked-bars__key">
        <i :class="`tone-${tones[index] ?? 'neutral'}`" aria-hidden="true" />{{ name }}
      </span>
    </figcaption>
  </figure>
</template>

<style scoped>
.modern-stacked-bars {
  display: grid;
  gap: var(--modern-space-2);
  margin: 0;
}
svg {
  width: 100%;
  height: var(--modern-chart-height);
  overflow: visible;
}
.modern-stacked-bars__bar {
  transform-box: fill-box;
  transform-origin: bottom;
  animation: modern-stacked-bars-grow var(--modern-motion-slow) var(--modern-motion-ease-out) both;
  animation-delay: calc(var(--i, 0) * var(--modern-motion-stagger) / 3);
  transition: opacity var(--modern-motion-fast) var(--modern-motion-ease-out);
}
.modern-stacked-bars__bar.is-dimmed {
  opacity: var(--modern-dimmed-opacity);
}
.modern-stacked-bars__hit {
  fill: transparent;
}
rect.tone-success,
i.tone-success {
  fill: var(--modern-status-success);
  background: var(--modern-status-success);
}
rect.tone-danger,
i.tone-danger {
  fill: var(--modern-status-danger);
  background: var(--modern-status-danger);
}
rect.tone-warning,
i.tone-warning {
  fill: var(--modern-status-warning);
  background: var(--modern-status-warning);
}
rect.tone-info,
i.tone-info {
  fill: var(--modern-info);
  background: var(--modern-info);
}
rect.tone-neutral,
i.tone-neutral {
  fill: var(--modern-status-neutral);
  background: var(--modern-status-neutral);
}
.modern-stacked-bars__caption {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--modern-space-3);
  min-height: var(--modern-control-xxs);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
}
.modern-stacked-bars__readout {
  margin-right: auto;
  color: var(--modern-text);
  font-variant-numeric: tabular-nums;
}
.modern-stacked-bars__key {
  display: inline-flex;
  align-items: center;
  gap: var(--modern-space-1);
}
.modern-stacked-bars__key i {
  width: var(--modern-space-2);
  height: var(--modern-space-2);
  border-radius: var(--modern-radius-small);
}
@keyframes modern-stacked-bars-grow {
  from {
    transform: scaleY(0);
  }
}
</style>
