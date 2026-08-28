<template>
  <div :data-theme="sharedState.theme">
    <MainLayout v-if="route.meta.layout === 'main'">
      <router-view v-slot="{ Component }">
        <Transition name="fade" mode="out-in">
          <component :is="Component" />
        </Transition>
      </router-view>
    </MainLayout>
    <router-view v-else v-slot="{ Component }">
      <Transition name="fade" mode="out-in">
        <component :is="Component" />
      </Transition>
    </router-view>
  </div>
</template>

<script setup>
// Root component: picks the "main" (sidebar) layout or a bare layout (e.g.
// Login) based on the active route's meta.layout, and keeps the OS-theme
// listener alive for the lifetime of the app.
import { onMounted, onUnmounted } from 'vue';
import { useRoute } from 'vue-router';
import MainLayout from './components/MainLayout.vue';
import { sharedState, initThemeListener } from './utils/sharedState';

const route = useRoute();
let removeThemeListener = null;

onMounted(() => {
  removeThemeListener = initThemeListener();
});

onUnmounted(() => {
  removeThemeListener?.();
});
</script>

<style>
/* Transitions */
.fade-enter-active {
  transition: opacity 0.22s cubic-bezier(0.23, 1, 0.32, 1), transform 0.22s cubic-bezier(0.23, 1, 0.32, 1);
}
.fade-leave-active {
  transition: opacity 0.12s ease;
}
.fade-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.fade-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.001ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.001ms !important;
    scroll-behavior: auto !important;
  }
}
</style>
