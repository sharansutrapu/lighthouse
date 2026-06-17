<template>
  <svg
    v-if="icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    :width="size"
    :height="size"
    aria-hidden="true"
  >
    <component
      v-for="(el, index) in icon"
      :is="el.tag"
      :key="index"
      v-bind="el.attrs"
    />
  </svg>
</template>

<script setup>
import { computed } from "vue";
import { icons } from "../utils/icons";

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: null },
  strokeWidth: { type: [Number, String], default: 2.5 },
});

const icon = computed(() => icons[props.name] ?? null);
</script>
