<script setup lang="ts">
// An unstyled button for a whole card or row that acts as one control. The
// page styles it through its own class; this keeps native button semantics.
withDefaults(defineProps<{ pressed?: boolean }>(), { pressed: undefined })
</script>

<template>
  <button type="button" class="modern-pressable" :aria-pressed="pressed"><slot /></button>
</template>

<!-- Zero-specificity reset so the page's own card styles always win. -->
<style>
:where(.modern-pressable) {
  border: 0;
  background: transparent;
  padding: 0;
  color: inherit;
  font: inherit;
  text-align: inherit;
  cursor: pointer;
}
:where(.modern-pressable):focus-visible {
  outline: var(--modern-focus-width) solid var(--modern-accent);
  outline-offset: var(--modern-focus-offset);
}
</style>
