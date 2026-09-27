<template>
  <div class="nodes-view animate-fade-in">
    <section class="nodes-hero">
      <div>
        <span class="hero-eyebrow">Cluster topology</span>
        <h1>Nodes</h1>
        <p>Connection state and workload distribution across the LightHouse control plane.</p>
      </div>
      <button class="refresh-btn" type="button" :disabled="loading" @click="fetchNodes">
        <AppIcon name="refresh" :class="{ spinning: loading }" />
        Refresh
      </button>
    </section>

    <section class="node-summary" aria-label="Node summary">
      <div>
        <span>Known nodes</span>
        <strong>{{ nodes.length }}</strong>
      </div>
      <div>
        <span>Connected</span>
        <strong class="success-text">{{ connectedCount }}</strong>
      </div>
      <div>
        <span>Unavailable</span>
        <strong :class="{ 'error-text': offlineCount > 0 }">{{ offlineCount }}</strong>
      </div>
      <div>
        <span>Workloads</span>
        <strong>{{ workloadCount }}</strong>
      </div>
    </section>

    <section class="nodes-table-wrap">
      <table class="premium-table nodes-table">
        <thead>
          <tr>
            <th>Node</th>
            <th>Role</th>
            <th>Status</th>
            <th>Workloads</th>
            <th>Last seen</th>
            <th>Capabilities</th>
          </tr>
        </thead>
        <tbody v-if="loading && nodes.length === 0">
          <tr><td colspan="6"><div class="loading-row">Loading topology...</div></td></tr>
        </tbody>
        <tbody v-else>
          <tr v-for="node in nodes" :key="node.id">
            <td data-label="Node">
              <div class="node-name">
                <span class="node-icon"><AppIcon name="server" /></span>
                <div>
                  <strong>{{ node.id || "local" }}</strong>
                  <span v-if="node.id === sharedState.localNodeId">Current control plane</span>
                </div>
              </div>
            </td>
            <td data-label="Role"><span class="role-badge">{{ node.role }}</span></td>
            <td data-label="Status">
              <span :class="['node-status', node.connected ? 'online' : 'offline']">
                <span class="status-dot"></span>{{ node.connected ? "Connected" : "Unavailable" }}
              </span>
            </td>
            <td data-label="Workloads">{{ node.container_count }}</td>
            <td data-label="Last seen">{{ formatLastSeen(node) }}</td>
            <td data-label="Capabilities">
              <div class="capability-list">
                <span v-for="capability in node.capabilities" :key="capability">{{ capability }}</span>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="error" class="nodes-error">{{ error }}</p>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import AppIcon from "../components/AppIcon.vue";
import { apiFetch } from "../utils/apiFetch";
import { sharedState } from "../utils/sharedState";

const nodes = ref([]);
const loading = ref(true);
const error = ref("");
let refreshTimer = null;

const connectedCount = computed(() => nodes.value.filter((node) => node.connected).length);
const offlineCount = computed(() => nodes.value.length - connectedCount.value);
const workloadCount = computed(() => nodes.value.reduce((total, node) => total + (node.container_count || 0), 0));

async function fetchNodes() {
  loading.value = true;
  error.value = "";
  try {
    const response = await apiFetch("/api/admin/nodes");
    if (!response.ok) throw new Error("Unable to load node topology");
    nodes.value = await response.json();
  } catch (fetchError) {
    error.value = fetchError.message || "Unable to load node topology";
  } finally {
    loading.value = false;
  }
}

function formatLastSeen(node) {
  if (node.role === "hub") return "Now";
  if (!node.last_seen) return "Never";
  return new Date(node.last_seen).toLocaleString();
}

onMounted(() => {
  fetchNodes();
  refreshTimer = setInterval(fetchNodes, 10000);
});

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer);
});
</script>

<style scoped>
.nodes-view {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  padding-bottom: 2rem;
}

.nodes-hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  padding: 1.5rem 1.75rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  background: var(--bg-card);
}

.hero-eyebrow {
  color: var(--accent);
  font-size: 0.72rem;
  font-weight: 800;
  text-transform: uppercase;
}

.nodes-hero h1 {
  margin: 0.35rem 0;
  color: var(--text-main);
  font-size: 1.75rem;
}

.nodes-hero p {
  margin: 0;
  color: var(--text-dim);
}

.node-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--bg-card);
}

.node-summary > div {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  padding: 1rem 1.25rem;
  border-right: 1px solid var(--border);
}

.node-summary > div:last-child { border-right: 0; }
.node-summary span { color: var(--text-mute); font-size: 0.7rem; font-weight: 800; text-transform: uppercase; }
.node-summary strong { color: var(--text-main); font-size: 1.45rem; }
.success-text { color: var(--success) !important; }
.error-text, .nodes-error { color: var(--error) !important; }

.nodes-table-wrap {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--bg-card);
}

.node-name { display: flex; align-items: center; gap: 0.7rem; }
.node-name > div { display: flex; flex-direction: column; gap: 0.2rem; }
.node-name strong { color: var(--text-main); }
.node-name span { color: var(--text-mute); font-size: 0.7rem; }
.node-icon { display: grid; place-items: center; width: 34px; height: 34px; color: var(--accent); background: var(--accent-soft); border-radius: 6px; }
.node-icon :deep(svg) { width: 17px; height: 17px; }
.role-badge { color: var(--accent); font-size: 0.68rem; font-weight: 800; text-transform: uppercase; }
.node-status { display: inline-flex; align-items: center; gap: 0.4rem; font-size: 0.72rem; font-weight: 700; }
.node-status.online { color: var(--success); }
.node-status.offline { color: var(--error); }
.status-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.capability-list { display: flex; flex-wrap: wrap; gap: 0.3rem; }
.capability-list span { padding: 0.18rem 0.4rem; border: 1px solid var(--border); border-radius: 4px; color: var(--text-dim); font-size: 0.62rem; }
.loading-row, .nodes-error { padding: 2rem; text-align: center; }

@media (max-width: 850px) {
  .nodes-hero { align-items: flex-start; flex-direction: column; }
  .node-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .node-summary > div:nth-child(2) { border-right: 0; }
  .node-summary > div:nth-child(-n+2) { border-bottom: 1px solid var(--border); }
  .nodes-table thead { display: none; }
  .nodes-table tbody tr { display: block; margin: 0.75rem; padding: 0.85rem; border: 1px solid var(--border); border-radius: var(--radius-md); }
  .nodes-table tbody td { display: flex; justify-content: space-between; gap: 1rem; padding: 0.55rem 0; border: 0; text-align: right; }
  .nodes-table tbody td::before { content: attr(data-label); color: var(--text-mute); font-size: 0.65rem; font-weight: 800; text-transform: uppercase; }
  .node-name { text-align: left; }
  .capability-list { justify-content: flex-end; }
}
</style>
