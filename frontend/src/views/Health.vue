<template>
  <div class="page-view health-container">
    <section class="page-hero animate-slide-up">
      <div class="page-hero-body">
        <div class="page-hero-copy">
          <span class="page-hero-eyebrow">Diagnostics</span>
          <h1>System health</h1>
          <p class="page-hero-sub">
            Historical CPU and memory utilization
            <span v-if="isPartialData" class="coverage-hint">
              · Showing {{ formatDuration(availableHours) }} of data
            </span>
          </p>
          <p v-if="systemInfo" class="system-info-hint" style="margin-top: 6px; font-size: 0.9em; opacity: 0.8;">
            <AppIcon name="server" style="width: 14px; height: 14px; display: inline-block; vertical-align: middle; margin-right: 4px; margin-top: -2px;" />
            System Environment: Docker v{{ systemInfo.docker_version }} · Compose v{{ systemInfo.compose_version }}
          </p>
        </div>
        <div class="page-hero-actions">
          <select v-model="activeFilter" @change="handleFilterChange" class="premium-input" style="width: auto;">
            <option v-for="f in filters" :key="f.value" :value="f.value">
              {{ f.label }}
            </option>
            <option value="custom">Custom Range...</option>
          </select>
        </div>
      </div>
      <div class="page-hero-mesh" aria-hidden="true"></div>
    </section>

    <section class="page-metrics animate-slide-up" style="animation-delay: 0.05s">
      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon">
            <AppIcon name="activity" />
          </div>
          <span class="badge badge-dim">Average</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Avg CPU load</span>
          <span class="stat-value">{{ avgCpu }}%</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon error">
            <AppIcon name="bell" />
          </div>
          <span class="badge badge-error">Peak</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Peak CPU spike</span>
          <span class="stat-value">{{ maxCpu }}%</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon success">
            <AppIcon name="server" />
          </div>
          <span class="badge badge-success">Memory</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Avg memory</span>
          <span class="stat-value">{{ avgMem }} GB</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon warning">
            <AppIcon name="containers" />
          </div>
          <span class="badge badge-warning">Max</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Peak memory</span>
          <span class="stat-value">{{ maxMem }} GB</span>
        </div>
      </div>
      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon" style="color: #3b82f6; background: rgba(59, 130, 246, 0.1);">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-6 h-6 text-blue-400"><path d="m21 16-4 4-4-4"/><path d="M17 20V4"/><path d="m3 8 4-4 4 4"/><path d="M7 4v16"/></svg>
          </div>
          <span class="badge" style="color: #3b82f6; border-color: #3b82f6;">Network</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Avg Rx / Tx</span>
          <span class="stat-value">{{ avgNet }}</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon warning">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-6 h-6 text-amber-400"><line x1="22" x2="2" y1="12" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" x2="6.01" y1="16" y2="16"/><line x1="10" x2="10.01" y1="16" y2="16"/></svg>
          </div>
          <span class="badge badge-warning">Disk I/O</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Avg R / W</span>
          <span class="stat-value">{{ avgDisk }}</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon" style="color: #8b5cf6; background: rgba(139, 92, 246, 0.1);">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-6 h-6 text-purple-400"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
          </div>
          <span class="badge" style="color: #8b5cf6; border-color: #8b5cf6;">Storage</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">System Wide</span>
          <span class="stat-value">{{ formatBytes(sysStorageUsed) }} / {{ formatBytes(sysStorageTotal) }}</span>
        </div>
      </div>

      <div class="page-metric-card">
        <div class="stat-header">
          <div class="stat-icon success">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-6 h-6"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/></svg>
          </div>
          <span class="badge badge-success">Fleet</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Running Containers</span>
          <span class="stat-value">{{ runningCount }} / {{ containers.length }}</span>
        </div>
      </div>
    </section>

    <section class="health-grid animate-slide-up" style="animation-delay: 0.1s">
      <div class="chart-section">
        <div class="chart-header">
          <h4>CPU Utilization History</h4>
          <span class="unit-badge">% Load</span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartData.cpu.labels.length"
            :data="chartData.cpu"
            :options="cpuChartOptions"
          />
          <div v-else class="chart-placeholder">
            <div class="shimmer"></div>
          </div>
        </div>
      </div>

      <div class="chart-section">
        <div class="chart-header">
          <h4>Memory Consumption</h4>
          <span class="unit-badge">Gigabytes</span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartData.mem.labels.length"
            :data="chartData.mem"
            :options="memChartOptions"
          />
          <div v-else class="chart-placeholder">
            <div class="shimmer"></div>
          </div>
        </div>
      </div>

      <div class="chart-section">
        <div class="chart-header">
          <h4>Network Traffic (Rx/Tx)</h4>
          <span class="unit-badge">MB/s</span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartData.net.labels.length"
            :data="chartData.net"
            :options="netChartOptions"
          />
          <div v-else class="chart-placeholder">
            <div class="shimmer"></div>
          </div>
        </div>
      </div>

      <div class="chart-section">
        <div class="chart-header">
          <h4>Disk I/O</h4>
          <span class="unit-badge">MB/s</span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartData.disk.labels.length"
            :data="chartData.disk"
            :options="diskChartOptions"
          />
          <div v-else class="chart-placeholder">
            <div class="shimmer"></div>
          </div>
        </div>
      </div>
    </section>

    <!-- Custom Range Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showCustomModal" class="modal-overlay">
          <div class="modal-content shadow-2xl">
            <div class="modal-header">
              <h3>Custom Range</h3>
            </div>
            <div class="modal-body">
              <div class="input-group">
                <label>Start Date</label>
                <input type="date" v-model="tempStart" :max="today" class="premium-input" />
              </div>
              <div class="input-group">
                <label>End Date</label>
                <input type="date" v-model="tempEnd" :max="today" class="premium-input" />
              </div>
              <p v-if="modalError" class="error-text">{{ modalError }}</p>
            </div>
            <div class="modal-actions">
              <button @click="showCustomModal = false" class="modal-btn cancel">Cancel</button>
              <button @click="applyCustomRange" class="modal-btn confirm">Apply Range</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import AppIcon from "../components/AppIcon.vue";
import { ref, onMounted, onUnmounted, watch, computed } from "vue";
import { useRoute, useRouter } from "vue-router";
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
import { secureStorage } from "../utils/storage";
import { sharedState, formatBytes } from "../utils/sharedState";
import { apiFetch } from "../utils/apiFetch";
import { useContainers } from "../composables/useContainers";

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

const isDark = computed(() => sharedState.theme === 'dark');
const sysStorageUsed = ref(0);
const sysStorageTotal = ref(0);
const systemInfo = ref(null);

const { containers, runningCount } = useContainers();

const route = useRoute();
const router = useRouter();

const filters = [
  { label: "Last 1 hour", value: 1 },
  { label: "Last 6 hours", value: 6 },
  { label: "Last 12 hours", value: 12 },
  { label: "Last 24 hours", value: 24 },
  { label: "Last 2 days", value: 48 },
  { label: "Last 7 days", value: 168 },
  { label: "Last 14 days", value: 336 },
  { label: "Last 30 days", value: 720 },
];

const activeFilter = ref(24);
const customStart = ref("");
const customEnd = ref("");
const tempStart = ref("");
const tempEnd = ref("");
const showCustomModal = ref(false);
const modalError = ref("");
const today = new Date().toISOString().split("T")[0];

const history = ref([]);

const handleFilterChange = () => {
  if (activeFilter.value === 'custom') {
    showCustomModal.value = true;
  } else {
    fetchData();
  }
};

const availableHours = computed(() => {
  if (history.value.length === 0) return 0;
  const timestamps = history.value.map(h => new Date(h.timestamp).getTime());
  const oldest = Math.min(...timestamps);
  const now = new Date().getTime();
  return Math.max(0, (now - oldest) / (1000 * 60 * 60));
});

const isPartialData = computed(() => {
  if (activeFilter.value === 'custom') return false;
  // If we have less than 95% of the requested range, call it partial
  return availableHours.value < (activeFilter.value * 0.95);
});

const formatDuration = (hours) => {
  if (hours < 1) return `${Math.round(hours * 60)}m`;
  if (hours < 24) return `${hours.toFixed(1)}h`;
  return `${(hours / 24).toFixed(1)}d`;
};
const chartData = ref({
  cpu: { labels: [], datasets: [] },
  mem: { labels: [], datasets: [] },
  net: { labels: [], datasets: [] },
  disk: { labels: [], datasets: [] },
});

const activeHistory = computed(() => {
  let start, end;
  if (activeFilter.value === "custom") {
    if (!customStart.value || !customEnd.value) return history.value;
    start = new Date(customStart.value);
    end = new Date(customEnd.value);
    end.setHours(23, 59, 59);
  } else {
    end = new Date();
    start = new Date(end.getTime() - activeFilter.value * 60 * 60 * 1000);
  }
  return history.value.filter((h) => {
    const t = new Date(h.timestamp);
    return t >= start && t <= end;
  });
});

const avgCpu = computed(() => {
  if (!activeHistory.value.length) return 0;
  return (activeHistory.value.reduce((acc, h) => acc + h.cpu, 0) / activeHistory.value.length).toFixed(1);
});

const maxCpu = computed(() => {
  if (!activeHistory.value.length) return 0;
  return Math.max(...activeHistory.value.map((h) => h.cpu)).toFixed(1);
});

const avgMem = computed(() => {
  if (!activeHistory.value.length) return 0;
  return (activeHistory.value.reduce((acc, h) => acc + h.memory, 0) / (activeHistory.value.length * 1024 * 1024 * 1024)).toFixed(2);
});

const maxMem = computed(() => {
  if (!activeHistory.value.length) return 0;
  return (Math.max(...activeHistory.value.map((h) => h.memory)) / (1024 * 1024 * 1024)).toFixed(2);
});

const avgNet = computed(() => {
  if (!activeHistory.value.length) return "0.0 / 0.0 MB/s";
  const rx = activeHistory.value.reduce((acc, h) => acc + (h.net_rx || 0), 0) / activeHistory.value.length;
  const tx = activeHistory.value.reduce((acc, h) => acc + (h.net_tx || 0), 0) / activeHistory.value.length;
  return `${(rx / (1024 * 1024)).toFixed(1)} / ${(tx / (1024 * 1024)).toFixed(1)} MB/s`;
});

const avgDisk = computed(() => {
  if (!activeHistory.value.length) return "0.0 / 0.0 MB/s";
  const r = activeHistory.value.reduce((acc, h) => acc + (h.disk_read || 0), 0) / activeHistory.value.length;
  const w = activeHistory.value.reduce((acc, h) => acc + (h.disk_write || 0), 0) / activeHistory.value.length;
  return `${(r / (1024 * 1024)).toFixed(1)} / ${(w / (1024 * 1024)).toFixed(1)} MB/s`;
});

const applyCustomRange = () => {
  if (!tempStart.value || !tempEnd.value) {
    modalError.value = "Select both dates";
    return;
  }
  customStart.value = tempStart.value;
  customEnd.value = tempEnd.value;
  activeFilter.value = "custom";
  showCustomModal.value = false;
  fetchData();
  updateUrl();
};

const syncStateFromUrl = () => {
  const { range, start, end } = route.query;
  if (range) {
    activeFilter.value = range === "custom" ? "custom" : parseInt(range);
  }
  if (start) customStart.value = start;
  if (end) customEnd.value = end;
};

const updateUrl = () => {
  const query = { ...route.query };
  query.range = activeFilter.value;
  if (activeFilter.value === "custom") {
    query.start = customStart.value;
    query.end = customEnd.value;
  } else {
    delete query.start;
    delete query.end;
  }
  router.replace({ query });
};

const makeChartOptions = (unit) => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 1000, easing: "easeOutQuart" },
  interaction: { mode: "index", intersect: false },
  scales: {
    y: {
      beginAtZero: true,
      grid: { color: isDark.value ? "rgba(255, 255, 255, 0.03)" : "rgba(0, 0, 0, 0.03)" },
      ticks: {
        color: isDark.value ? "rgba(255, 255, 255, 0.4)" : "rgba(0, 0, 0, 0.4)",
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
const netChartOptions = computed(() => makeChartOptions("MB/s"));
const diskChartOptions = computed(() => makeChartOptions("MB/s"));

watch(
  () => sharedState.theme,
  () => {
    updateCharts();
  },
);

const fetchData = async () => {
  try {
    let endpoint = "/api/system/history";
    if (activeFilter.value === "custom") {
      endpoint += `?from=${customStart.value}T00:00:00Z&to=${customEnd.value}T23:59:59Z`;
    } else {
      endpoint += `?duration=${activeFilter.value}h`;
    }

    const token = secureStorage.getItem("token");
    const [resHist, resStore, resInfo] = await Promise.all([
      apiFetch(endpoint, {
        headers: { Authorization: `Bearer ${token}` },
      }),
      apiFetch("/api/system/storage", {
        headers: { Authorization: `Bearer ${token}` },
      }),
      apiFetch("/api/system/info", {
        headers: { Authorization: `Bearer ${token}` },
      })
    ]);
    
    if (resHist.ok) {
      const data = await resHist.json();
      // Ensure history is sorted ascending (oldest first) for easier processing
      history.value = data.sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));
      updateCharts();
    }
    
    if (resStore.ok) {
      const storeData = await resStore.json();
      sysStorageUsed.value = storeData.used_bytes;
      sysStorageTotal.value = storeData.total_bytes;
    }

    if (resInfo.ok) {
      systemInfo.value = await resInfo.json();
    }
  } catch (err) {
    console.error(err);
  }
};

const updateCharts = () => {
  const now = new Date();
  let rangeHours = activeFilter.value;
  let startTime;
  
  if (activeFilter.value === "custom") {
    const start = new Date(customStart.value);
    const end = new Date(customEnd.value);
    end.setHours(23, 59, 59);
    rangeHours = (end.getTime() - start.getTime()) / (1000 * 60 * 60);
    startTime = start;
  } else {
    startTime = new Date(now.getTime() - rangeHours * 60 * 60 * 1000);
  }
  
  // 1. Generate Fixed Timeline Bins (e.g., 60 bins for any range)
  const binCount = 60;
  const binSizeMs = (rangeHours * 60 * 60 * 1000) / binCount;
  const timeline = [];
  
  for (let i = 0; i <= binCount; i++) {
    const t = new Date(startTime.getTime() + i * binSizeMs);
    const isToday = t.toDateString() === now.toDateString();
    const label = isToday
      ? t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : t.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      
    timeline.push({
      time: t,
      label: label,
      cpu: null,
      mem: null,
      net_rx: null,
      net_tx: null,
      disk_read: null,
      disk_write: null
    });
  }

  // 2. Map actual history to bins
  // Since history is sorted ascending, we can efficiently map it
  history.value.forEach(h => {
    const hTime = new Date(h.timestamp);
    if (hTime < startTime) return;
    
    const binIndex = Math.floor((hTime.getTime() - startTime.getTime()) / binSizeMs);
    if (binIndex >= 0 && binIndex <= binCount) {
      // Use latest value in the bin
      timeline[binIndex].cpu = h.cpu;
      timeline[binIndex].mem = h.memory;
      timeline[binIndex].net_rx = h.net_rx;
      timeline[binIndex].net_tx = h.net_tx;
      timeline[binIndex].disk_read = h.disk_read;
      timeline[binIndex].disk_write = h.disk_write;
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
      spanGaps: true // Connect gaps if any, or keep false to show missing data
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

  const toMB = (bytes) => bytes ? bytes / (1024 * 1024) : null;

  chartData.value.net = {
    labels,
    datasets: [
      {
        label: "Download (Rx)",
        data: timeline.map(t => toMB(t.net_rx)),
        borderColor: "#3b82f6",
        backgroundColor: "rgba(59, 130, 246, 0.1)",
        fill: true,
        borderWidth: 3,
        spanGaps: true
      },
      {
        label: "Upload (Tx)",
        data: timeline.map(t => toMB(t.net_tx)),
        borderColor: "#8b5cf6",
        backgroundColor: "rgba(139, 92, 246, 0.1)",
        fill: true,
        borderWidth: 3,
        spanGaps: true
      }
    ]
  };

  chartData.value.disk = {
    labels,
    datasets: [
      {
        label: "Read",
        data: timeline.map(t => toMB(t.disk_read)),
        borderColor: "#f59e0b",
        backgroundColor: "rgba(245, 158, 11, 0.1)",
        fill: true,
        borderWidth: 3,
        spanGaps: true
      },
      {
        label: "Write",
        data: timeline.map(t => toMB(t.disk_write)),
        borderColor: "#ef4444",
        backgroundColor: "rgba(239, 68, 68, 0.1)",
        fill: true,
        borderWidth: 3,
        spanGaps: true
      }
    ]
  };
};

watch([activeFilter, customStart, customEnd], () => {
  updateCharts();
  updateUrl();
});

onMounted(() => {
  syncStateFromUrl();
  fetchData();
});
</script>

<style scoped>
.health-container {
  gap: 1.25rem;
}

.coverage-hint {
  color: var(--warning);
  font-weight: 700;
}

.system-info-hint {
  color: var(--text-mute);
  font-weight: 500;
}

.page-filter-pill.is-partial {
  opacity: 0.65;
}

.health-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}

.chart-section {
  padding: 1.25rem;
  border-radius: var(--radius-2xl);
  border: 1px solid var(--border);
  background: var(--bg-card);
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.chart-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.chart-header h4 {
  margin: 0;
  font-size: 0.9rem;
  font-weight: 900;
  color: var(--text-main);
}

.unit-badge {
  font-size: 0.65rem;
  font-weight: 900;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  padding: 0.25rem 0.6rem;
  background: var(--bg-input);
  border-radius: 6px;
}

.chart-container {
  height: 350px;
  position: relative;
}

.chart-placeholder {
  height: 100%;
  background: var(--bg-input);
  border-radius: 16px;
  overflow: hidden;
}

@media (max-width: 1200px) {
  .health-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .chart-container {
    height: 280px;
  }
}

@media (max-width: 480px) {
  .chart-container {
    height: 240px;
  }
}
</style>
