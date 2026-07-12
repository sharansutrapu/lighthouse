<template>
  <div class="detail-view animate-fade-in">
    <div v-if="loading && !container" class="detail-loading">
      <div class="shimmer-block"></div>
      <div class="shimmer-block wide"></div>
    </div>

    <template v-else-if="container">
      <section class="detail-hero">
        <div class="hero-top">
          <router-link to="/containers" class="back-link">
            <AppIcon name="chevronLeft" :size="16" :stroke-width="3" />
            <span>Containers</span>
          </router-link>
          
          <div class="hero-top-right">
            <div
              class="state-pill"
              :class="container.state === 'running' ? 'is-running' : 'is-stopped'"
            >
              <span class="pulse-dot"></span>
              {{ container.state }}
            </div>
          </div>
        </div>

        <div class="hero-main">
          <div
            class="hero-icon"
            :class="container.state === 'running' ? 'running' : 'stopped'"
          >
            <AppIcon name="containers" :size="28" />
          </div>
          <div class="hero-copy">
            <h1>{{ container.name }}</h1>
            <div class="hero-meta">
              <button
                type="button"
                class="id-chip"
                @click="copyText(container.id, 'Container ID')"
              >
                <span>{{ container.id }}</span>
                <AppIcon name="copy" :size="14" />
              </button>
              <span class="meta-dot">·</span>
              <span class="meta-image">{{ container.image }}</span>
            </div>
            <p class="hero-status">{{ container.status }}</p>
          </div>
        </div>

        <div class="action-rail">
          <button
            type="button"
            class="action-chip logs"
            @click="goToLogs(container.id)"
          >
            <AppIcon name="logs" :size="16" />
            <span>Logs</span>
          </button>
          <button
            v-if="
              userCanShell(sharedState.currentUser) &&
              container.state === 'running'
            "
            type="button"
            class="action-chip shell"
            @click="goToShell(container.id)"
          >
            <AppIcon name="terminal" :size="16" />
            <span>Shell</span>
          </button>
          <button
            type="button"
            class="action-chip start"
            @click="triggerConfirm(container.id, 'start')"
          >
            <AppIcon name="play" :size="16" />
            <span>Start</span>
          </button>
          <button
            type="button"
            class="action-chip restart"
            @click="triggerConfirm(container.id, 'restart')"
          >
            <AppIcon name="refresh" :size="16" />
            <span>Restart</span>
          </button>
          <button
            type="button"
            class="action-chip stop"
            @click="triggerConfirm(container.id, 'stop')"
          >
            <AppIcon name="stop" :size="16" />
            <span>Stop</span>
          </button>
          <button
            v-if="!container.is_platform && userCanDelete(sharedState.currentUser)"
            type="button"
            class="action-chip delete"
            @click="triggerConfirm(container.id, 'remove')"
          >
            <AppIcon name="trash" :size="16" />
            <span>Delete</span>
          </button>
          <button
            type="button"
            class="action-chip"
            @click="triggerScan"
          >
            <AppIcon name="shield" :size="16" />
            <span>Scan Image</span>
          </button>
        </div>
      </section>

      <section v-if="container.state === 'running'" class="stats-grid">
        <article class="stat-card">
          <span class="stat-label">CPU</span>
          <span class="stat-value">{{ liveStats.cpu.toFixed(1) }}%</span>
          <div class="stat-bar">
            <div
              class="stat-bar-fill accent"
              :style="{ width: `${Math.min(liveStats.cpu, 100)}%` }"
            ></div>
          </div>
        </article>
        <article class="stat-card">
          <span class="stat-label">Memory</span>
          <span class="stat-value">{{ formatBytes(liveStats.memory) }}</span>
          <span v-if="memLimit" class="stat-sub"
            >of {{ formatBytes(memLimit) }}</span
          >
        </article>
        <article class="stat-card">
          <span class="stat-label">CPU limit</span>
          <span class="stat-value">{{ cpuLimitLabel }}</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Memory limit</span>
          <span class="stat-value">{{ memLimit > 0 ? formatBytes(memLimit) : 'No Limit' }}</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Storage</span>
          <span class="stat-value sm">{{ formatBytes(resolvedInspect?.SizeRw || container?.size_rw) }} RW</span>
          <span class="stat-sub">{{ formatBytes(resolvedInspect?.SizeRootFs || container?.size_root_fs) }} Total</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Network (Rx / Tx)</span>
          <span class="stat-value sm">{{ formatBytes(liveStats.net_rx) }}/s ↓</span>
          <span class="stat-sub">{{ formatBytes(liveStats.net_tx) }}/s ↑</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Disk I/O (R / W)</span>
          <span class="stat-value sm">{{ formatBytes(liveStats.disk_read) }}/s R</span>
          <span class="stat-sub">{{ formatBytes(liveStats.disk_write) }}/s W</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Created</span>
          <span class="stat-value sm">{{ formatDate(container.created) }}</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Processes</span>
          <span class="stat-value">{{ liveStats.pids }}</span>
        </article>
        <article class="stat-card">
          <span class="stat-label">Restarts</span>
          <span class="stat-value">{{ resolvedInspect?.RestartCount || 0 }}</span>
        </article>
      </section>

      <section class="detail-panels">
        <article class="panel">
          <div class="panel-head">
            <h2>Overview</h2>
          </div>
          <div class="kv-grid">
            <div class="kv-item">
              <span class="kv-label">Image</span>
              <span class="kv-value mono">{{
                overview.image || container.image
              }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Command</span>
              <span class="kv-value mono">{{ overview.command || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Restart policy</span>
              <span class="kv-value">{{ overview.restartPolicy || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Platform</span>
              <span class="kv-value">{{ overview.platform || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Started</span>
              <span class="kv-value">{{ overview.startedAt || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Finished</span>
              <span class="kv-value">{{ overview.finishedAt || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Disk RW (Used)</span>
              <span class="kv-value">{{ formatBytes(resolvedInspect?.SizeRw || container?.size_rw) || "—" }}</span>
            </div>
            <div class="kv-item">
              <span class="kv-label">Disk RootFs</span>
              <span class="kv-value">{{ formatBytes(resolvedInspect?.SizeRootFs || container?.size_root_fs) || "—" }}</span>
            </div>
            <div class="kv-item full-width" style="grid-column: 1 / -1; margin-top: 1rem;">
              <span class="kv-label">Vulnerability Scan</span>
              <div v-if="scanResults.loading" class="scan-loading" style="padding: 1rem; text-align: center; color: var(--text-mute);">
                <span class="pulse-dot"></span> Scanning in progress...
              </div>
              <div v-else-if="scanResults.data" class="scan-data" style="margin-top: 0.5rem; background: var(--bg-main); padding: 1rem; border-radius: 8px;">
                <div v-for="(res, idx) in scanResults.data.Results" :key="idx" class="scan-target" style="margin-bottom: 1rem;">
                  <h4 style="margin: 0 0 0.5rem 0; font-size: 0.9rem; color: var(--text-main);">{{ res.Target }}</h4>
                  <p v-if="!res.Vulnerabilities || res.Vulnerabilities.length === 0" class="no-vuln" style="color: var(--success); font-size: 0.85rem;">
                    No vulnerabilities found.
                  </p>
                  <div v-else class="vuln-table-wrapper" style="overflow-x: auto;">
                    <table class="vuln-table" style="width: 100%; border-collapse: collapse; font-size: 0.8rem; text-align: left;">
                      <thead>
                        <tr style="border-bottom: 1px solid var(--border); color: var(--text-mute);">
                          <th style="padding: 0.5rem;">ID</th>
                          <th style="padding: 0.5rem;">PkgName</th>
                          <th style="padding: 0.5rem;">Severity</th>
                          <th style="padding: 0.5rem;">Title</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="v in res.Vulnerabilities" :key="v.VulnerabilityID" style="border-bottom: 1px solid var(--border);">
                          <td style="padding: 0.5rem;"><a :href="v.PrimaryURL" target="_blank" style="color: var(--accent);">{{ v.VulnerabilityID }}</a></td>
                          <td style="padding: 0.5rem;">{{ v.PkgName }} ({{ v.InstalledVersion }})</td>
                          <td style="padding: 0.5rem;">
                            <span :class="['vuln-badge', v.Severity.toLowerCase()]" style="padding: 0.2rem 0.5rem; border-radius: 4px; font-weight: bold; font-size: 0.7rem;">{{ v.Severity }}</span>
                          </td>
                          <td style="padding: 0.5rem; max-width: 200px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;" :title="v.Title">{{ v.Title }}</td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
              <div v-else class="scan-empty" style="color: var(--text-mute); font-size: 0.85rem; padding: 0.5rem 0;">
                No scan results available yet.
              </div>
            </div>
          </div>
        </article>

        <article class="panel">
          <div class="panel-head">
            <h2>Network ports</h2>
          </div>
          <div v-if="ports.length" class="port-list">
            <div v-for="port in ports" :key="port" class="port-row mono">
              {{ port }}
            </div>
          </div>
          <div v-else class="empty-state">No exposed ports</div>
        </article>

        <article class="panel">
          <div class="panel-head">
            <h2>Live Events</h2>
            <span class="badge badge-dim">{{ containerEvents.length }}</span>
          </div>
          <div v-if="containerEvents.length" class="events-list">
            <div v-for="evt in containerEvents" :key="evt.id" class="event-row">
              <span class="event-time">{{ evt.time }}</span>
              <span class="event-action" :class="evt.action">{{ evt.action }}</span>
            </div>
          </div>
          <div v-else class="empty-state">Waiting for events...</div>
        </article>

        <article class="panel panel-wide">
          <div class="panel-head">
            <h2>Environment</h2>
            <div class="env-search">
              <AppIcon name="search" :size="14" />
              <input
                v-model="envQuery"
                type="text"
                placeholder="Filter variables..."
              />
            </div>
          </div>
          <div v-if="filteredEnv.length" class="env-list">
            <div v-for="item in filteredEnv" :key="item.key" class="env-row">
              <span class="env-key mono">{{ item.key }}</span>
              <span class="env-val mono">{{ revealedEnvs.includes(item.key) ? item.value : '••••••••' }}</span>
              <button type="button" class="env-toggle" @click="toggleEnv(item.key)" :title="revealedEnvs.includes(item.key) ? 'Hide' : 'Reveal'">
                <svg v-if="revealedEnvs.includes(item.key)" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                  <line x1="1" y1="1" x2="23" y2="23"></line>
                </svg>
                <svg v-else viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                  <circle cx="12" cy="12" r="3"></circle>
                </svg>
              </button>
            </div>
          </div>
          <p v-else class="panel-empty">
            No environment variables match your filter.
          </p>
        </article>

        <!-- Historical Performance -->
        <article class="panel panel-wide" v-if="history.length > 0">
          <div class="panel-head" style="display: flex; justify-content: space-between; align-items: center;">
            <h2>Historical Performance</h2>
            <div class="page-filter-pills" style="margin: 0;">
              <button
                v-for="f in historyFilters"
                :key="f.label"
                @click="activeHistoryFilter = f.value"
                :class="[
                  'page-filter-pill',
                  { active: activeHistoryFilter === f.value }
                ]"
                :data-tooltip="f.note"
              >
                {{ f.short }}
              </button>
            </div>
          </div>
          <div class="charts-grid">
            <div class="chart-section">
              <div class="chart-header">
                <h4>CPU Usage</h4>
                <span class="unit-badge">%</span>
              </div>
              <div class="chart-container">
                <Line
                  v-if="chartData.cpu.labels.length"
                  :data="chartData.cpu"
                  :options="cpuChartOptions"
                />
              </div>
            </div>

            <div class="chart-section">
              <div class="chart-header">
                <h4>Memory Usage</h4>
                <span class="unit-badge">GB</span>
              </div>
              <div class="chart-container">
                <Line
                  v-if="chartData.mem.labels.length"
                  :data="chartData.mem"
                  :options="memChartOptions"
                />
              </div>
            </div>
          </div>
        </article>
      </section>
    </template>

    <div v-else class="detail-empty">
      <h2>Container not found</h2>
      <p>This container may have been removed or you may not have access.</p>
      <router-link to="/containers" class="back-btn"
        >Back to containers</router-link
      >
    </div>

    <Teleport to="body">
      <Transition name="fade">
        <div
          v-if="showConfirm"
          class="modal-overlay"
          @click.self="showConfirm = false"
        >
          <div class="modal-content shadow-2xl">
            <div :class="['modal-icon', actionClass]">
              <AppIcon
                v-if="pendingAction === 'start'"
                name="play"
                :size="28"
                :stroke-width="2.5"
              />
              <AppIcon
                v-else-if="pendingAction === 'restart'"
                name="refresh"
                :size="28"
                :stroke-width="2.5"
              />
              <AppIcon v-else name="alert" :size="28" />
            </div>
            <div class="modal-text-center">
              <h3>Confirm operation</h3>
              <p>
                Are you sure you want to <strong>{{ pendingAction }}</strong>
                <strong>{{ container?.name }}</strong
                >?
              </p>
            </div>
            <div class="modal-actions">
              <button
                type="button"
                class="modal-btn cancel"
                @click="showConfirm = false"
                :disabled="isActionLoading"
              >
                Cancel
              </button>
              <button
                type="button"
                :class="['modal-btn confirm', actionClass]"
                @click="executeConfirmAction"
                :disabled="isActionLoading"
              >
                <span v-if="isActionLoading">Processing...</span>
                <span v-else>Confirm</span>
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Global Action Loader -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="globalActionLoading" class="modal-overlay" style="z-index: 9999; background: rgba(0,0,0,0.6);">
          <div class="loader-content" style="display: flex; flex-direction: column; align-items: center; gap: 1rem;">
            <div class="spinner" style="width: 48px; height: 48px; border: 4px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 1s linear infinite;"></div>
            <h3 style="color: white; margin: 0;">Processing Action...</h3>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppIcon from "../components/AppIcon.vue";
import { useContainers } from "../composables/useContainers";
import { apiFetch } from "../utils/apiFetch";
import { sharedState, showToast, formatBytes } from "../utils/sharedState";
import { createAuthenticatedWebSocket } from "../utils/wsAuth";
import { secureStorage } from "../utils/storage";
import { Line } from "vue-chartjs";
import {
  Chart as ChartJS,
  Title,
  Tooltip,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  CategoryScale,
  Filler,
} from "chart.js";

ChartJS.register(
  Title,
  Tooltip,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  CategoryScale,
  Filler,
);
import {
  userCanStart,
  userCanStop,
  userCanRestart,
  userCanDelete,
  userCanShell,
} from "../utils/sharedState";

const route = useRoute();
const router = useRouter();
const containerId = computed(() => route.params.id);

const {
  containers,
  loading,
  fetchContainers,
  goToLogs,
  goToShell,
  showConfirm,
  pendingAction,
  actionClass,
  triggerConfirm,
  executeAction: executeConfirmAction,
  formatDate,
  isActionLoading: globalActionLoading,
} = useContainers({ autoPoll: true });

const scanResults = ref({
  loading: false,
  data: null
});
const containerEvents = ref([]);
let eventsWs = null;

const fetchScanResults = async () => {
  if (!container.value || !container.value.image) return;
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch(`/api/images/scans?image=${encodeURIComponent(container.value.image)}`, {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      const txt = await res.text();
      scanResults.value.data = JSON.parse(txt);
    }
  } catch(e) {}
};

const triggerScan = async () => {
  scanResults.value.loading = true;
  scanResults.value.data = null;
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch(`/api/containers/${container.value.id}/scan`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      showToast("Success", "Scan started", "success");
      // Poll for results
      let retries = 0;
      const poll = setInterval(async () => {
        await fetchScanResults();
        if (scanResults.value.data) {
          scanResults.value.loading = false;
          clearInterval(poll);
        } else {
          retries++;
          if (retries > 30) {
            clearInterval(poll);
            scanResults.value.loading = false;
            showToast("Timeout", "Scan is taking too long.", "warning");
          }
        }
      }, 10000);
    }
  } catch(e) {
    scanResults.value.loading = false;
    showToast("Error", "Failed to start scan", "error");
  }
};

const inspectData = ref(null);
const inspectLoading = ref(true);
const isActionLoading = ref(false);
const liveStats = ref({ cpu: 0, memory: 0, net_rx: 0, net_tx: 0, disk_read: 0, disk_write: 0, pids: 0 });
const envQuery = ref("");
const revealedEnvs = ref([]);

const toggleEnv = (key) => {
  if (revealedEnvs.value.includes(key)) {
    revealedEnvs.value = revealedEnvs.value.filter((k) => k !== key);
  } else {
    revealedEnvs.value.push(key);
  }
};
let statsTimer = null;

const container = computed(() => {
  const id = containerId.value;
  return containers.value.find(
    (c) => c.id === id || c.id.startsWith(id) || id.startsWith(c.id),
  );
});

const resolvedInspect = computed(() => {
  const data = inspectData.value;
  if (!data) return {};
  if (data.Container && typeof data.Container === "object") {
    return { ...data, ...data.Container };
  }
  return data;
});

const memLimit = computed(
  () =>
    container.value?.memLimit || resolvedInspect.value?.HostConfig?.Memory || 0,
);

const cpuLimitLabel = computed(() => {
  const limit =
    container.value?.cpuLimit || resolvedInspect.value?.HostConfig?.NanoCpus;
  if (!limit) return "No Limit";
  const cpus = typeof limit === "number" && limit > 100 ? limit / 1e9 : limit;
  return `${cpus} CPU${cpus === 1 ? "" : "s"}`;
});

const overview = computed(() => {
  const cfg = resolvedInspect.value?.Config || {};
  const state = resolvedInspect.value?.State || {};
  const host = resolvedInspect.value?.HostConfig || {};
  const cmd = Array.isArray(cfg.Cmd) ? cfg.Cmd.join(" ") : cfg.Cmd;
  return {
    image: cfg.Image,
    command: cmd || "—",
    restartPolicy: host.RestartPolicy?.Name,
    platform: resolvedInspect.value?.Platform,
    startedAt: formatInspectTime(state.StartedAt),
    finishedAt: formatInspectTime(state.FinishedAt),
  };
});

const envVars = computed(() => {
  const env = resolvedInspect.value?.Config?.Env || [];
  return env.map((line) => {
    const idx = line.indexOf("=");
    if (idx === -1) return { key: line, value: "" };
    return { key: line.slice(0, idx), value: line.slice(idx + 1) };
  });
});

const filteredEnv = computed(() => {
  const q = envQuery.value.trim().toLowerCase();
  if (!q) return envVars.value;
  return envVars.value.filter(
    (item) =>
      item.key.toLowerCase().includes(q) ||
      item.value.toLowerCase().includes(q),
  );
});

const ports = computed(() => {
  const bindings = resolvedInspect.value?.NetworkSettings?.Ports || {};
  const rows = [];
  for (const [containerPort, hostBindings] of Object.entries(bindings)) {
    if (!hostBindings?.length) {
      rows.push(containerPort);
      continue;
    }
    for (const binding of hostBindings) {
      rows.push(
        `${binding.HostIp || "0.0.0.0"}:${binding.HostPort} → ${containerPort}`,
      );
    }
  }
  return rows.sort();
});

function formatInspectTime(value) {
  if (!value || value === "0001-01-01T00:00:00Z") return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

// History & Charts
const history = ref([]);
const chartData = ref({
  cpu: { labels: [], datasets: [] },
  mem: { labels: [], datasets: [] },
});

const isDark = computed(() => sharedState.theme === "dark");

const makeChartOptions = (unit) => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: "index", intersect: false },
  scales: {
    y: {
      beginAtZero: true,
      grid: {
        color: isDark.value ? "rgba(255, 255, 255, 0.05)" : "rgba(0, 0, 0, 0.05)",
      },
      ticks: {
        color: isDark.value ? "rgba(255, 255, 255, 0.3)" : "rgba(0, 0, 0, 0.3)",
        font: { size: 10, family: "JetBrains Mono" },
      },
    },
    x: {
      grid: { display: false },
      ticks: {
        color: isDark.value ? "rgba(255, 255, 255, 0.3)" : "rgba(0, 0, 0, 0.3)",
        font: { size: 9, family: "JetBrains Mono" },
        maxRotation: 0,
        maxTicksLimit: 8,
      },
    },
  },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: isDark.value ? "#0f172a" : "#ffffff",
      titleColor: isDark.value ? "rgba(255,255,255,0.5)" : "rgba(0,0,0,0.5)",
      bodyColor: isDark.value ? "#fff" : "#000",
      borderColor: "var(--border)",
      borderWidth: 1,
      padding: 12,
      cornerRadius: 12,
      displayColors: false,
      callbacks: {
        label: (item) => `${item.formattedValue} ${unit}`,
      },
    },
  },
  elements: {
    point: { radius: 0, hoverRadius: 6 },
    line: { tension: 0.4 },
  },
});

const cpuChartOptions = computed(() => makeChartOptions("%"));
const memChartOptions = computed(() => makeChartOptions("GB"));

watch(() => sharedState.theme, () => {
  updateCharts();
});

const updateCharts = () => {
  const now = new Date();
  const rangeHours = 1; // last 1 hour
  const startTime = new Date(now.getTime() - rangeHours * 60 * 60 * 1000);
  
  const binCount = 60;
  const binSizeMs = (rangeHours * 60 * 60 * 1000) / binCount;
  const timeline = [];
  
  for (let i = 0; i <= binCount; i++) {
    const t = new Date(startTime.getTime() + i * binSizeMs);
    timeline.push({
      time: t,
      label: t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      cpu: null,
      mem: null,
    });
  }

  history.value.forEach(h => {
    const hTime = new Date(h.timestamp);
    if (hTime < startTime) return;
    
    const binIndex = Math.floor((hTime.getTime() - startTime.getTime()) / binSizeMs);
    if (binIndex >= 0 && binIndex <= binCount) {
      timeline[binIndex].cpu = h.cpu;
      timeline[binIndex].mem = h.memory;
    }
  });

  const labels = timeline.map(t => t.label);
  
  chartData.value.cpu = {
    labels,
    datasets: [{
      label: "CPU Load",
      data: timeline.map(t => t.cpu),
      borderColor: "#0891b2",
      backgroundColor: "rgba(8, 145, 178, 0.12)",
      fill: true,
      borderWidth: 3,
      spanGaps: true
    }]
  };

  chartData.value.mem = {
    labels,
    datasets: [{
      label: "Memory Usage",
      data: timeline.map(t => t.mem ? t.mem / (1024 * 1024 * 1024) : null),
      borderColor: "#10b981",
      backgroundColor: "rgba(16, 185, 129, 0.1)",
      fill: true,
      borderWidth: 3,
      spanGaps: true
    }]
  };
};

const historyFilters = [
  { label: "1H", short: "1H", note: "Last hour", value: "1h" },
  { label: "6H", short: "6H", note: "Last 6 hours", value: "6h" },
  { label: "12H", short: "12H", note: "Last 12 hours", value: "12h" },
  { label: "24H", short: "24H", note: "Last 24 hours", value: "24h" },
];
const activeHistoryFilter = ref("1h");

watch(activeHistoryFilter, () => {
  fetchHistoryData();
});

async function fetchHistoryData() {
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch(`/api/containers/${containerId.value}/history?duration=${activeHistoryFilter.value}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      const data = await res.json();
      history.value = data.sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));
      updateCharts();
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
}

async function fetchInspect() {
  inspectLoading.value = true;
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch(`/api/containers/${containerId.value}/inspect`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      inspectData.value = await res.json();
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  } finally {
    inspectLoading.value = false;
  }
}

async function fetchLiveStats() {
  if (!container.value || container.value.state !== "running") return;
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch(
      `/api/containers/${containerId.value}/stats-now`,
      {
        headers: { Authorization: `Bearer ${token}` },
      },
    );
    if (res.ok) {
      const data = await res.json();
      liveStats.value = {
        cpu: Number(data.cpu) || 0,
        memory: Number(data.memory) || 0,
        net_rx: Number(data.net_rx) || 0,
        net_tx: Number(data.net_tx) || 0,
        disk_read: Number(data.disk_read) || 0,
        disk_write: Number(data.disk_write) || 0,
        pids: Number(data.pids) || 0,
      };
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
}

function startStatsPolling() {
  stopStatsPolling();
  fetchLiveStats();
  statsTimer = setInterval(fetchLiveStats, 5000);
}

function stopStatsPolling() {
  if (statsTimer) {
    clearInterval(statsTimer);
    statsTimer = null;
  }
}

const connectEventsWS = () => {
  if (eventsWs) eventsWs.close();
  eventsWs = createAuthenticatedWebSocket("/ws/events");
  eventsWs.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.Type === 'container' && data.Actor && data.Actor.ID && data.Actor.ID.startsWith(containerId.value)) {
        containerEvents.value.unshift({
          id: data.timeNano || Date.now() + Math.random(),
          action: data.Action,
          time: new Date((data.timeNano / 1000000) || (data.time * 1000) || Date.now()).toLocaleTimeString()
        });
        if (containerEvents.value.length > 50) containerEvents.value.pop();
      }
    } catch (e) {
      console.error("Failed to parse event:", e); showToast('Error', 'An error occurred. Check console for details.', 'error');
    }
  };
};

async function confirmAction() {
  const action = pendingAction.value;
  isActionLoading.value = true;
  try {
    await executeConfirmAction();
    if (action === "remove") {
      router.push("/containers");
      return;
    }
    await fetchInspect();
  } finally {
    isActionLoading.value = false;
  }
}

const executeAction = async (action) => {
  try {
    const token = secureStorage.getItem("token");
    const formData = new FormData();
    formData.append("action", action);
    const res = await apiFetch(`/api/containers/${containerId.value}/action`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      body: formData,
    });
    if (res.ok) {
      showToast("Success", `Action ${action} executed.`, "success");
      await fetchContainers();
      await fetchInspect();
    } else {
      showToast("Error", "Action failed.", "error");
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
    showToast("Error", "Action failed.", "error");
  }
};

function copyText(text, label) {
  navigator.clipboard.writeText(text).then(() => {
    showToast("Copied", `${label} copied to clipboard.`, "success");
  });
}

watch(container, (value) => {
  if (value?.state === "running") startStatsPolling();
  else stopStatsPolling();
});

onMounted(async () => {
  await fetchContainers();
  await fetchInspect();
  await fetchHistoryData();
  if (container.value?.state === "running") startStatsPolling();
});

onUnmounted(() => {
  stopStatsPolling();
});
</script>

<style scoped>
.detail-view {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  padding-bottom: 2rem;
}

.detail-loading {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.shimmer-block {
  height: 120px;
  border-radius: var(--radius-2xl);
  background: linear-gradient(
    90deg,
    var(--bg-input) 25%,
    var(--bg-subtle) 50%,
    var(--bg-input) 75%
  );
  background-size: 200% 100%;
  animation: shimmer 1.2s infinite;
}

.shimmer-block.wide {
  height: 280px;
}

@keyframes shimmer {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}

.detail-hero {
  padding: 1.35rem 1.5rem;
  border-radius: var(--radius-2xl);
  border: 1px solid var(--border);
  background: linear-gradient(
    135deg,
    var(--bg-card) 0%,
    rgba(var(--accent-rgb), 0.04) 100%
  );
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.hero-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}

.back-link {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: var(--text-dim);
  text-decoration: none;
  font-size: 0.85rem;
  font-weight: 700;
}

.back-link:hover {
  color: var(--accent);
}

.state-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.35rem 0.7rem;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: 1px solid var(--border);
}

.state-pill.is-running {
  color: var(--success);
  border-color: rgba(var(--success-rgb), 0.35);
  background: rgba(var(--success-rgb), 0.08);
}

.state-pill.is-stopped {
  color: var(--text-mute);
}

.pulse-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: currentColor;
}

.state-pill.is-running .pulse-dot {
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}

.hero-main {
  display: flex;
  gap: 1rem;
  align-items: flex-start;
}

.hero-icon {
  width: 56px;
  height: 56px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  flex-shrink: 0;
}

.hero-icon.running {
  color: var(--success);
  background: rgba(var(--success-rgb), 0.08);
}

.hero-icon.stopped {
  color: var(--text-mute);
  background: var(--bg-input);
}

.hero-copy h1 {
  margin: 0 0 0.35rem;
  font-size: clamp(1.35rem, 2.5vw, 1.85rem);
  font-weight: 800;
  letter-spacing: -0.03em;
  color: var(--text-main);
}

.hero-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.45rem;
  margin-bottom: 0.35rem;
}

.id-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.25rem 0.55rem;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-dim);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  cursor: pointer;
}

.id-chip:hover {
  border-color: var(--border-active);
  color: var(--accent);
}

.meta-dot,
.meta-image {
  color: var(--text-mute);
  font-size: 0.85rem;
}

.hero-status {
  margin: 0;
  color: var(--text-dim);
  font-size: 0.9rem;
}

.action-rail {
  display: flex;
  flex-wrap: wrap;
  gap: 0.55rem;
  margin-top: 1.25rem;
  padding-top: 1.15rem;
  border-top: 1px solid var(--border-subtle);
}

.action-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0.55rem 0.9rem;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-main);
  font-size: 0.8rem;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s ease;
}

.action-chip:hover {
  transform: translateY(-1px);
}

.action-chip.logs:hover {
  color: var(--accent);
  border-color: rgba(var(--accent-rgb), 0.35);
  background: var(--accent-soft);
}

.action-chip.shell:hover {
  background: rgba(139, 92, 246, 0.15);
  color: #8b5cf6;
  border-color: rgba(139, 92, 246, 0.4);
}

.action-chip.start:hover {
  color: var(--success);
  border-color: rgba(var(--success-rgb), 0.35);
  background: rgba(var(--success-rgb), 0.08);
}

.action-chip.stop:hover,
.action-chip.delete:hover {
  color: var(--error);
  border-color: rgba(var(--error-rgb), 0.35);
  background: rgba(var(--error-rgb), 0.08);
}

.action-chip.restart:hover {
  color: var(--accent);
  border-color: rgba(var(--accent-rgb), 0.35);
  background: var(--accent-soft);
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0.85rem;
}

.stat-card {
  padding: 1rem 1.1rem;
  border-radius: var(--radius-xl);
  border: 1px solid var(--border);
  background: var(--bg-card);
}

.stat-label {
  display: block;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-mute);
  margin-bottom: 0.35rem;
}

.stat-value {
  display: block;
  font-size: 1.35rem;
  font-weight: 800;
  color: var(--text-main);
  font-variant-numeric: tabular-nums;
}

.stat-value.sm {
  font-size: 0.95rem;
  font-weight: 700;
}

.stat-sub {
  display: block;
  margin-top: 0.2rem;
  font-size: 0.78rem;
  color: var(--text-dim);
}

.stat-bar {
  margin-top: 0.65rem;
  height: 5px;
  border-radius: 999px;
  background: var(--bg-input);
  overflow: hidden;
}

.stat-bar-fill {
  height: 100%;
  border-radius: inherit;
}

.stat-bar-fill.accent {
  background: linear-gradient(90deg, var(--accent), #22d3ee);
}

.detail-panels {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.85rem;
}

.panel {
  border-radius: var(--radius-xl);
  border: 1px solid var(--border);
  background: var(--bg-card);
  padding: 1rem 1.1rem;
}

.panel-wide {
  grid-column: 1 / -1;
}

.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  margin-bottom: 0.85rem;
}

.panel-head h2 {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 800;
  color: var(--text-main);
}

.env-search {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0.45rem 0.7rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-input);
  min-width: 220px;
}

.env-search input {
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-main);
  font-size: 0.82rem;
  width: 100%;
}

.kv-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem 1rem;
}

.kv-label {
  display: block;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--text-mute);
  margin-bottom: 0.2rem;
}

.kv-value {
  color: var(--text-main);
  font-size: 0.88rem;
  word-break: break-word;
}

.mono {
  font-family: var(--font-mono);
}

.port-list,
.env-list {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
  max-height: 280px;
  overflow: auto;
}

.port-row,
.env-row {
  padding: 0.55rem 0.7rem;
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  border: 1px solid var(--border-subtle);
  font-size: 0.82rem;
}

.env-row {
  display: grid;
  grid-template-columns: minmax(100px, 40%) 1fr auto;
  gap: 0.75rem;
  align-items: center;
}

.env-key {
  color: var(--accent);
  font-weight: 600;
}

.env-val {
  color: var(--text-main);
  word-break: break-all;
  flex: 1;
}

.env-toggle {
  background: transparent;
  border: none;
  color: var(--text-mute);
  cursor: pointer;
  padding: 0.25rem;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.env-toggle:hover {
  background: var(--bg-input);
  color: var(--text-main);
}

.panel-empty {
  margin: 0;
  color: var(--text-mute);
  font-size: 0.88rem;
}

.detail-empty {
  padding: 3rem 1rem;
  text-align: center;
}

.detail-empty h2 {
  margin: 0 0 0.5rem;
}

.detail-empty p {
  color: var(--text-dim);
  margin: 0 0 1rem;
}

.back-btn {
  display: inline-flex;
  padding: 0.65rem 1rem;
  border-radius: var(--radius-md);
  background: var(--accent);
  color: #fff;
  text-decoration: none;
  font-weight: 700;
}

.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(2, 6, 23, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 1rem;
}

.modal-content {
  width: min(420px, 100%);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  overflow: hidden;
  transition: all 0.2s;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
}

/* Charts */
.charts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
  gap: 1.5rem;
  padding: 1.25rem;
}

.chart-section {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.chart-header h4 {
  margin: 0;
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text-main);
}

.unit-badge {
  font-size: 0.75rem;
  font-weight: 700;
  color: var(--text-dim);
  background: var(--bg-subtle);
  padding: 0.1rem 0.5rem;
  border-radius: 12px;
}

.chart-container {
  height: 250px;
  position: relative;
  background: var(--bg-card);
  border-radius: var(--radius-xl);
  border: 1px solid var(--border);
  padding: 1rem;
}

.modal-icon {
  width: 52px;
  height: 52px;
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 1rem;
  background: var(--bg-input);
}

.modal-icon.success {
  color: var(--success);
}
.modal-icon.warning {
  color: var(--warning);
}
.modal-icon.error {
  color: var(--error);
}

.modal-text-center {
  text-align: center;
  margin-bottom: 1rem;
}

.modal-text-center h3 {
  margin: 0 0 0.5rem;
}

.modal-text-center p {
  margin: 0;
  color: var(--text-dim);
}

.modal-actions {
  display: flex;
  gap: 0.65rem;
}

.modal-btn {
  flex: 1;
  padding: 0.7rem 1rem;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  font-weight: 700;
  cursor: pointer;
}

.modal-btn.cancel {
  background: var(--bg-input);
  color: var(--text-main);
}

.modal-btn.confirm {
  color: #fff;
  border: none;
}

.modal-btn.confirm.success {
  background: var(--success);
}
.modal-btn.confirm.warning {
  background: var(--warning);
}
.modal-btn.confirm.error {
  background: var(--error);
}

@media (max-width: 960px) {
  .stats-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .detail-panels,
  .kv-grid,
  .header-actions,
  .layout-main-split {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .hero-main {
    flex-direction: column;
  }

  .stats-grid {
    grid-template-columns: 1fr;
  }

  .env-row {
    grid-template-columns: 1fr;
  }

  .panel-head {
    flex-direction: column;
    align-items: stretch;
  }

  .env-search {
    min-width: 0;
    width: 100%;
  }
}

.events-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  max-height: 300px;
  overflow-y: auto;
  padding: 0.5rem 0;
}

.event-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1rem;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-family: var(--font-mono);
  font-size: 0.85rem;
}

.event-time {
  color: var(--text-mute);
}

.event-action {
  font-weight: 600;
  text-transform: uppercase;
  color: var(--accent);
}

.event-action.die, .event-action.kill, .event-action.oom {
  color: var(--danger);
}

.event-action.start, .event-action.restart {
  color: var(--success);
}
</style>
