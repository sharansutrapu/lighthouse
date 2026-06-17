<template>
  <div class="page-view audit-view animate-fade-in">
    <section class="page-hero">
      <div class="page-hero-body">
        <div class="page-hero-copy">
          <span class="page-hero-eyebrow">Security</span>
          <h1>Audit logs</h1>
          <p class="page-hero-sub">Track administrative actions and system security events.</p>
        </div>
        <div class="page-hero-stats">
          <div class="page-hero-stat">
            <span class="page-hero-stat-val">{{ logCount }}</span>
            <span class="page-hero-stat-lbl">Events</span>
          </div>
        </div>
      </div>
      <div class="page-hero-mesh" aria-hidden="true"></div>
    </section>

    <section class="page-panel">
      <AuditManager :token="token" embedded @update-count="logCount = $event" />
    </section>
  </div>
</template>

<script setup>
import { ref } from "vue";
import AuditManager from "../components/AuditManager.vue";
import { secureStorage } from "../utils/storage";

const token = secureStorage.getItem("token");
const logCount = ref(0);
</script>
