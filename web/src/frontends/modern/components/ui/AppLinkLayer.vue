<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { AppLink } from './types'

// Draws curved connectors between elements inside the positioned parent; see
// AppLink for how ends are named.

const props = defineProps<{ links: AppLink[] }>()

interface DrawnLink extends AppLink {
  path: string
}

const root = ref<SVGSVGElement | null>(null)
const drawn = ref<DrawnLink[]>([])
const size = ref({ width: 0, height: 0 })
let observer: ResizeObserver | undefined
let frame = 0

function anchor(container: Element, name: string): DOMRect | undefined {
  return container.querySelector(`[data-link="${CSS.escape(name)}"]`)?.getBoundingClientRect()
}

function measure(): void {
  frame = 0
  const container = root.value?.parentElement
  if (!container) return
  const box = container.getBoundingClientRect()
  size.value = { width: box.width, height: box.height }
  const result: DrawnLink[] = []
  for (const link of props.links) {
    const from = anchor(container, link.from)
    const to = anchor(container, link.to)
    if (!from || !to) continue
    const x1 = from.right - box.left
    const y1 = from.top + Math.min(from.height / 2, 28) - box.top
    const x2 = to.left - box.left
    const y2 = to.top + Math.min(to.height / 2, 28) - box.top
    const bend = Math.max(24, (x2 - x1) * 0.5)
    result.push({
      ...link,
      path: `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`,
    })
  }
  // Active links are drawn last so they sit above the idle ones.
  drawn.value = result.sort((a, b) => Number(Boolean(a.active)) - Number(Boolean(b.active)))
}

function schedule(): void {
  if (!frame) frame = requestAnimationFrame(measure)
}

onMounted(() => {
  const container = root.value?.parentElement
  if (!container) return
  observer = new ResizeObserver(schedule)
  observer.observe(container)
  for (const child of container.children) observer.observe(child)
  window.addEventListener('resize', schedule)
  void nextTick(schedule)
})
onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('resize', schedule)
  if (frame) cancelAnimationFrame(frame)
})
watch(
  () => props.links,
  () => void nextTick(schedule),
  { deep: true },
)
defineExpose({ refresh: schedule })
</script>

<template>
  <svg
    ref="root"
    class="modern-link-layer"
    :width="size.width"
    :height="size.height"
    :viewBox="`0 0 ${size.width} ${size.height}`"
    aria-hidden="true"
  >
    <path
      v-for="link in drawn"
      :key="link.id + (link.active ? ':on' : '')"
      :d="link.path"
      pathLength="1"
      :class="{
        'modern-link-layer__path--active': link.active,
        'modern-link-layer__path--dimmed': link.dimmed,
      }"
    />
  </svg>
</template>

<style scoped>
.modern-link-layer {
  position: absolute;
  inset: 0;
  overflow: visible;
  pointer-events: none;
  z-index: var(--modern-layer-underlay);
}
path {
  fill: none;
  stroke: var(--modern-link-idle);
  stroke-width: var(--modern-link-stroke);
  transition:
    stroke var(--modern-motion-medium) var(--modern-motion-ease-out),
    opacity var(--modern-motion-medium) var(--modern-motion-ease-out);
}
.modern-link-layer__path--dimmed {
  opacity: var(--modern-link-dimmed-opacity);
}
.modern-link-layer__path--active {
  stroke: var(--modern-link-active);
  stroke-width: var(--modern-link-stroke-active);
  stroke-dasharray: 1;
  stroke-dashoffset: 1;
  animation: modern-link-draw var(--modern-motion-draw) var(--modern-motion-ease-out) forwards;
}
@keyframes modern-link-draw {
  to {
    stroke-dashoffset: 0;
  }
}
</style>
