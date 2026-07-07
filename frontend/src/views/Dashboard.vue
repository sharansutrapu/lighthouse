<template>
  <div class="dashboard">
    <!-- Hero -->
    <section class="dash-hero animate-slide-up">
      <div class="hero-body">
        <div class="hero-copy">
          <span class="hero-eyebrow">
            <span class="live-dot" aria-hidden="true"></span>
            Live overview
          </span>
          <h1 class="hero-title">{{ greeting }}, {{ username }}</h1>
          <p class="hero-sub">
            {{ containers.length }} container{{ containers.length === 1 ? "" : "s" }} across your fleet
            <span v-if="runningCount" class="hero-accent"> · {{ runningCount }} running</span>
          </p>
        </div>
        <div class="hero-actions">
          <router-link to="/health" class="dash-action-btn">
            <AppIcon name="activity" />
            Diagnostics
          </router-link>
          <router-link to="/containers" class="dash-action-btn">
            <AppIcon name="containers" />
            All containers
          </router-link>
          <button class="dash-action-btn primary" @click="refresh" :disabled="isRefreshing">
            <AppIcon name="refresh" :class="{ spinning: isRefreshing }" />
            Refresh
          </button>
        </div>
      </div>
      <div class="hero-mesh" aria-hidden="true"></div>
    </section>

    <!-- Metrics -->
    <section class="metrics-bento animate-slide-up" style="animation-delay: 0.05s">
      <article class="metric-card variant-fleet">
        <div class="metric-glow" aria-hidden="true"></div>
        <div class="metric-top">
          <div class="metric-icon">
            <AppIcon name="containers" />
          </div>
          <span class="metric-badge">Fleet</span>
        </div>
        <div class="metric-body">
          <div class="metric-main">
            <span class="metric-value">{{ containers.length }}</span>
            <span class="metric-label">Total containers</span>
          </div>
          <div class="metric-visual">
            <div class="donut" :style="{ '--pct': runningRatio }">
              <svg viewBox="0 0 36 36">
                <circle class="donut-track" cx="18" cy="18" r="15.5" />
                <circle class="donut-fill success" cx="18" cy="18" r="15.5" />
              </svg>
              <span class="donut-label">{{ runningRatio }}%</span>
            </div>
          </div>
        </div>
        <div class="metric-footer">
          <div class="metric-bar">
            <div class="metric-bar-fill success" :style="{ width: runningRatio + '%' }"></div>
          </div>
          <span>{{ runningCount }} running · {{ stoppedCount }} stopped</span>
        </div>
      </article>

      <article class="metric-card variant-live">
        <div class="metric-glow" aria-hidden="true"></div>
        <div class="metric-top">
          <div class="metric-icon success">
            <AppIcon name="activity" />
          </div>
          <span class="metric-badge success">Live</span>
        </div>
        <div class="metric-body">
          <div class="metric-main">
            <span class="metric-value">{{ runningCount }}</span>
            <span class="metric-label">Running now</span>
          </div>
          <div class="metric-visual">
            <div class="pulse-stack">
              <span v-for="n in 4" :key="n" class="pulse-bar" :style="{ height: pulseHeights[n - 1] + '%', animationDelay: n * 0.12 + 's' }"></span>
            </div>
          </div>
        </div>
        <div class="metric-footer success-text">
          {{ runningRatio }}% of fleet is healthy
        </div>
      </article>

      <article class="metric-card variant-idle">
        <div class="metric-glow" aria-hidden="true"></div>
        <div class="metric-top">
          <div class="metric-icon muted">
            <AppIcon name="stopOutline" />
          </div>
          <span class="metric-badge dim">Idle</span>
        </div>
        <div class="metric-body">
          <div class="metric-main">
            <span class="metric-value">{{ stoppedCount }}</span>
            <span class="metric-label">Stopped</span>
          </div>
          <div class="metric-visual">
            <div class="donut muted" :style="{ '--pct': stoppedRatio }">
              <svg viewBox="0 0 36 36">
                <circle class="donut-track" cx="18" cy="18" r="15.5" />
                <circle class="donut-fill muted" cx="18" cy="18" r="15.5" />
              </svg>
              <span class="donut-label">{{ stoppedRatio }}%</span>
            </div>
          </div>
        </div>
        <div class="metric-footer" :class="{ warn: stoppedCount > 0 }">
          {{ stoppedCount ? `${stoppedCount} container${stoppedCount === 1 ? '' : 's'} offline` : "All services operational" }}
        </div>
      </article>

      <article class="metric-card variant-host">
        <div class="metric-glow" aria-hidden="true"></div>
        <div class="metric-top">
          <div class="metric-icon">
            <AppIcon name="server" />
          </div>
          <span class="metric-badge">Host</span>
        </div>
        <div class="metric-body host-body">
          <div class="host-stat">
            <div class="host-stat-head">
              <span>CPU</span>
              <strong :style="{ color: statColor(cpuPercent) }">{{ cpuPercent.toFixed(1) }}%</strong>
            </div>
            <div class="host-track">
              <div class="host-fill" :style="{ width: cpuPercent + '%', background: statColor(cpuPercent) }"></div>
            </div>
            <span class="host-meta" v-if="sharedState.systemStats?.cores">
              {{ sharedState.systemStats.cores }} core{{ sharedState.systemStats.cores > 1 ? "s" : "" }}
            </span>
          </div>
          <div class="host-stat">
            <div class="host-stat-head">
              <span>Memory</span>
              <strong>{{ formatBytes(memUsed) }}</strong>
            </div>
            <div class="host-track">
              <div class="host-fill accent" :style="{ width: memPercent + '%' }"></div>
            </div>
            <span class="host-meta">{{ memPercent.toFixed(0) }}% of {{ formatBytes(memTotal) }}</span>
          </div>
        </div>
        <div class="metric-footer">
          {{ hostStatusLabel }}
        </div>
      </article>
    </section>

    <!-- Top Consumers -->
    <section class="top-consumers animate-slide-up" style="animation-delay: 0.08s" v-if="topConsumers.length > 0">
      <div class="panel-toolbar" style="margin-bottom: 0.75rem;">
        <div class="toolbar-left">
          <h2>Top Consumers</h2>
          <p class="toolbar-sub">Containers using the most resources</p>
        </div>
      </div>
      <div class="consumers-grid">
        <div v-for="c in topConsumers" :key="c.id" class="metric-card consumer-card">
          <div class="consumer-header">
            <strong class="consumer-name">{{ c.name }}</strong>
            <span class="metric-badge" :class="c.state === 'running' ? 'success' : 'dim'">{{ c.state }}</span>
          </div>
          <div class="host-body">
            <div class="host-stat">
              <div class="host-stat-head">
                <span>CPU</span>
                <strong :style="{ color: statColor(c.cpu || 0) }">{{ (c.cpu || 0).toFixed(1) }}%</strong>
              </div>
              <div class="host-track">
                <div class="host-fill" :style="{ width: Math.min(100, c.cpu || 0) + '%', background: statColor(c.cpu || 0) }"></div>
              </div>
            </div>
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Memory</span>
                <strong>{{ formatBytes(c.memory || 0) }}</strong>
              </div>
              <div class="host-track">
                <div class="host-fill accent" :style="{ width: Math.min(100, ((c.memory || 0) / (memTotal || 1)) * 100) + '%' }"></div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Vulnerability Tiles -->
    <section class="top-consumers animate-slide-up" style="animation-delay: 0.09s" v-if="vulnerableContainers.length > 0">
      <div class="panel-toolbar" style="margin-bottom: 0.75rem;">
        <div class="toolbar-left">
          <h2>Security Risks</h2>
          <p class="toolbar-sub">Containers with highest vulnerability counts</p>
        </div>
      </div>
      <div class="consumers-grid">
        <div v-for="c in vulnerableContainers" :key="c.id" class="metric-card consumer-card">
          <div class="consumer-header">
            <strong class="consumer-name">{{ c.name.replace(/^\//, '') }}</strong>
            <span class="metric-badge" :class="c.state === 'running' ? 'success' : 'dim'">{{ c.state }}</span>
          </div>
          <div class="host-body">
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Critical</span>
                <strong style="color: var(--error)">{{ c.vulns.CRITICAL }}</strong>
              </div>
              <div class="host-track">
                <div class="host-fill" style="background: var(--error)" :style="{ width: Math.min(100, c.vulns.CRITICAL * 10) + '%' }"></div>
              </div>
            </div>
            <div class="host-stat">
              <div class="host-stat-head">
                <span>High</span>
                <strong style="color: var(--warning)">{{ c.vulns.HIGH }}</strong>
              </div>
              <div class="host-track">
                <div class="host-fill" style="background: var(--warning)" :style="{ width: Math.min(100, c.vulns.HIGH * 5) + '%' }"></div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Storage & Networking -->
    <section class="top-consumers animate-slide-up" style="animation-delay: 0.095s">
      <div class="panel-toolbar" style="margin-bottom: 0.75rem;">
        <div class="toolbar-left">
          <h2>Storage & Networking</h2>
          <p class="toolbar-sub">Docker engine resources overview</p>
        </div>
      </div>
      <div class="consumers-grid">
        <!-- Images Tile -->
        <div class="metric-card consumer-card">
          <div class="consumer-header">
            <strong class="consumer-name">Images</strong>
            <span class="metric-badge success">{{ images.length }} total</span>
          </div>
          <div class="host-body">
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Disk Usage</span>
                <strong>{{ formatBytes(totalImageSize) }}</strong>
              </div>
            </div>
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Dangling</span>
                <strong :style="{ color: danglingImagesCount > 0 ? 'var(--warning)' : 'inherit' }">{{ danglingImagesCount }} unused</strong>
              </div>
            </div>
            <div style="margin-top: auto; padding-top: 1rem;">
              <router-link to="/images" class="dash-action-btn" style="width: 100%; justify-content: center;">
                Manage Images
              </router-link>
            </div>
          </div>
        </div>

        <!-- Volumes Tile -->
        <div class="metric-card consumer-card">
          <div class="consumer-header">
            <strong class="consumer-name">Volumes</strong>
            <span class="metric-badge accent">{{ volumes.length }} total</span>
          </div>
          <div class="host-body">
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Local Driver</span>
                <strong>{{ localVolumesCount }} volume(s)</strong>
              </div>
            </div>
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Other Drivers</span>
                <strong>{{ volumes.length - localVolumesCount }} volume(s)</strong>
              </div>
            </div>
            <div style="margin-top: auto; padding-top: 1rem;">
              <router-link to="/volumes" class="dash-action-btn" style="width: 100%; justify-content: center;">
                Manage Volumes
              </router-link>
            </div>
          </div>
        </div>

        <!-- Networks Tile -->
        <div class="metric-card consumer-card">
          <div class="consumer-header">
            <strong class="consumer-name">Networks</strong>
            <span class="metric-badge success">{{ networks.length }} total</span>
          </div>
          <div class="host-body">
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Bridge Driver</span>
                <strong>{{ bridgeNetworksCount }} network(s)</strong>
              </div>
            </div>
            <div class="host-stat">
              <div class="host-stat-head">
                <span>Other Drivers</span>
                <strong>{{ networks.length - bridgeNetworksCount }} network(s)</strong>
              </div>
            </div>
            <div style="margin-top: auto; padding-top: 1rem;">
              <router-link to="/networks" class="dash-action-btn" style="width: 100%; justify-content: center;">
                Manage Networks
              </router-link>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Container table -->
    <section class="table-panel animate-slide-up" style="animation-delay: 0.1s">
      <div class="panel-toolbar">
        <div class="toolbar-left">
          <h2>Anomalous containers</h2>
          <p class="toolbar-sub">Containers that are currently stopped or failing</p>
        </div>
        <div class="toolbar-right">
          <div class="filter-pills">
            <button
              class="filter-pill active"
            >
              Stopped / Failed
              <span class="pill-count">{{ stoppedCount }}</span>
            </button>
          </div>
          <div class="search-box glass">
            <AppIcon name="search" :size="16" />
            <input type="text" v-model="searchQuery" placeholder="Search..." />
          </div>
        </div>
      </div>
      <ContainerTable :state-filter="stateFilter" :search-query="searchQuery" embedded />
    </section>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from "vue";
import { showToast } from "../utils/sharedState";
import AppIcon from "../components/AppIcon.vue";
import ContainerTable from "../components/ContainerTable.vue";
import { useContainers } from "../composables/useContainers";
import { sharedState, formatBytes } from "../utils/sharedState";
import { secureStorage } from "../utils/storage";
import { apiFetch } from "../utils/apiFetch";

const stateFilter = ref("stopped");
const searchQuery = ref("");

const { containers, loading, runningCount, stoppedCount, fetchContainers } = useContainers();

const username = computed(
  () => sharedState.currentUser?.username || "there",
);

const greeting = computed(() => {
  const hour = new Date().getHours();
  if (hour < 12) return "Good morning";
  if (hour < 17) return "Good afternoon";
  return "Good evening";
});

const runningRatio = computed(() => {
  if (!containers.value.length) return 0;
  return Math.round((runningCount.value / containers.value.length) * 100);
});

const stoppedRatio = computed(() => {
  if (!containers.value.length) return 0;
  return Math.round((stoppedCount.value / containers.value.length) * 100);
});

const pulseHeights = computed(() => {
  const base = Math.max(35, Math.min(95, runningRatio.value));
  return [base * 0.55, base * 0.85, base, base * 0.7];
});

const hostStatusLabel = computed(() => {
  const load = Math.max(cpuPercent.value, memPercent.value);
  if (load > 80) return "High resource pressure";
  if (load > 50) return "Moderate system load";
  return "System running smoothly";
});

const cpuPercent = computed(() =>
  parseFloat(sharedState.systemStats?.cpu || 0),
);

const memUsed = computed(() => sharedState.systemStats?.memory || 0);
const memTotal = computed(() => sharedState.systemStats?.total_memory || 1);

const memPercent = computed(() => {
  if (!memTotal.value) return 0;
  return Math.min(100, (memUsed.value / memTotal.value) * 100);
});

const filters = computed(() => [
  { label: "All", value: "all", count: containers.value.length },
  { label: "Running", value: "running", count: runningCount.value },
  { label: "Stopped", value: "stopped", count: stoppedCount.value },
]);

const topConsumers = computed(() => {
  const sorted = [...containers.value].sort((a, b) => {
    const aScore = (a.cpu || 0) + ((a.memory || 0) / (memTotal.value || 1)) * 100;
    const bScore = (b.cpu || 0) + ((b.memory || 0) / (memTotal.value || 1)) * 100;
    return bScore - aScore;
  });
  return sorted.slice(0, 3);
});

const vulnScanData = ref({});
const vulnerableContainers = computed(() => {
  const list = [];
  for (const c of containers.value) {
    if (vulnScanData.value[c.id]) {
      const v = vulnScanData.value[c.id];
      if (v.counts.CRITICAL > 0 || v.counts.HIGH > 0) {
        list.push({ ...c, vulns: v.counts });
      }
    }
  }
  return list.sort((a, b) => {
    if (b.vulns.CRITICAL !== a.vulns.CRITICAL) return b.vulns.CRITICAL - a.vulns.CRITICAL;
    return b.vulns.HIGH - a.vulns.HIGH;
  }).slice(0, 3);
});

const parseScanResults = (data) => {
  let counts = { CRITICAL: 0, HIGH: 0, MEDIUM: 0, LOW: 0, UNKNOWN: 0 };
  if (data && data.Results) {
    for (const r of data.Results) {
      if (r.Vulnerabilities) {
        for (const v of r.Vulnerabilities) {
          if (counts[v.Severity] !== undefined) counts[v.Severity]++;
        }
      }
    }
  }
  return { counts };
};

const loadScans = async () => {
  const token = secureStorage.getItem('token');
  const promises = containers.value.map(async (c) => {
    try {
      const res = await apiFetch(`/api/images/scans?image=${encodeURIComponent(c.image)}`, {
        headers: { Authorization: `Bearer ${token}` }
      });
      if (res.ok) {
        const text = await res.text();
        if (text && text.trim() !== '') {
          vulnScanData.value[c.id] = parseScanResults(JSON.parse(text));
        }
      }
    } catch(e) {}
  });
  await Promise.all(promises);
};

watch(containers, (newVal) => {
  if (newVal && newVal.length > 0 && Object.keys(vulnScanData.value).length === 0) {
    loadScans();
  }
}, { immediate: true });

const statColor = (val) => {
  if (val > 80) return "var(--error)";
  if (val > 50) return "var(--warning)";
  return "var(--accent)";
};

// Docker Engine Resources State
const images = ref([]);
const volumes = ref([]);
const networks = ref([]);

const totalImageSize = computed(() => {
  return images.value.reduce((acc, img) => acc + (img.Size || 0), 0);
});

const danglingImagesCount = computed(() => {
  return images.value.filter(img => !img.RepoTags || img.RepoTags.length === 0 || img.RepoTags.includes('<none>:<none>')).length;
});

const localVolumesCount = computed(() => {
  return volumes.value.filter(v => v.Driver === 'local').length;
});

const bridgeNetworksCount = computed(() => {
  return networks.value.filter(n => n.Driver === 'bridge').length;
});

const fetchEngineResources = async () => {
  try {
    const [imgRes, volRes, netRes] = await Promise.all([
      apiFetch('/api/images'),
      apiFetch('/api/volumes'),
      apiFetch('/api/networks')
    ]);
    
    if (imgRes.ok) {
      const data = await imgRes.json();
      images.value = Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []);
    }
    if (volRes.ok) {
      const data = await volRes.json();
      volumes.value = Array.isArray(data?.Volumes) ? data.Volumes : (Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []));
    }
    if (netRes.ok) {
      const data = await netRes.json();
      networks.value = Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []);
    }
  } catch (err) {
    console.error('Failed to fetch engine resources', err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
};

onMounted(() => {
  fetchEngineResources();
});

const isRefreshing = ref(false);

const refresh = async () => {
  if (isRefreshing.value) return;
  isRefreshing.value = true;
  
  await Promise.all([
    fetchContainers(),
    fetchEngineResources()
  ]);
  
  // Give a minimum 500ms spin time so the user actually sees the feedback
  setTimeout(() => {
    isRefreshing.value = false;
  }, 500);
};
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  padding-bottom: 2rem;
}

/* Hero */
.dash-hero {
  position: relative;
  border-radius: var(--radius-2xl);
  border: 1px solid var(--border);
  background:
    linear-gradient(135deg, var(--bg-card) 0%, var(--bg-card) 55%, rgba(var(--accent-rgb), 0.04) 100%);
  padding: 1.5rem 1.75rem;
  overflow: hidden;
  isolation: isolate;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.hero-body {
  position: relative;
  z-index: 1;
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 1.5rem;
  flex-wrap: wrap;
}

.hero-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--accent);
  margin-bottom: 0.5rem;
}

.live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--success);
  box-shadow: 0 0 0 3px rgba(var(--success-rgb), 0.25);
  animation: pulse 2s ease infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.7; transform: scale(0.92); }
}

.hero-title {
  font-size: clamp(1.5rem, 3vw, 2rem);
  font-weight: 800;
  letter-spacing: -0.03em;
  color: var(--text-main);
  margin: 0 0 0.35rem;
}

.hero-sub {
  font-size: 0.92rem;
  color: var(--text-dim);
  margin: 0;
}

.hero-accent {
  color: var(--success);
  font-weight: 600;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.65rem;
}

.dash-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 1rem;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-dim);
  font-size: 0.8rem;
  font-weight: 700;
  text-decoration: none;
  transition: all 0.2s ease;
}

.dash-action-btn svg {
  width: 16px;
  height: 16px;
}

.dash-action-btn:hover {
  border-color: rgba(var(--accent-rgb), 0.35);
  color: var(--accent);
  background: var(--accent-soft);
  transform: translateY(-1px);
}

.dash-action-btn.primary {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}

.dash-action-btn.primary:hover {
  background: var(--accent-hover);
  color: #fff;
}

.dash-action-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
}

.spinning {
  animation: spin 0.9s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.hero-mesh {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse 80% 60% at 100% 0%, rgba(var(--accent-rgb), 0.18), transparent 55%),
    radial-gradient(ellipse 50% 40% at 0% 100%, rgba(var(--success-rgb), 0.08), transparent 50%);
  pointer-events: none;
}

/* Bento metrics */
.metrics-bento {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 0.75rem;
  align-items: stretch;
}

.metrics-bento .metric-card:nth-child(1) { animation-delay: 0.05s; }
.metrics-bento .metric-card:nth-child(2) { animation-delay: 0.1s; }
.metrics-bento .metric-card:nth-child(3) { animation-delay: 0.15s; }
.metrics-bento .metric-card:nth-child(4) { animation-delay: 0.2s; }

.metric-card {
  position: relative;
  padding: 0.85rem 0.95rem 0.75rem;
  border-radius: var(--radius-lg);
  background: var(--bg-card);
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  min-height: 128px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
  transition: transform 0.25s ease, box-shadow 0.25s ease, border-color 0.25s ease;
  overflow: hidden;
}

.metric-glow {
  position: absolute;
  width: 88px;
  height: 88px;
  border-radius: 50%;
  top: -32px;
  right: -24px;
  pointer-events: none;
  opacity: 0.5;
  filter: blur(22px);
}

.variant-fleet .metric-glow { background: rgba(var(--accent-rgb), 0.22); }
.variant-live .metric-glow { background: rgba(var(--success-rgb), 0.25); }
.variant-idle .metric-glow { background: rgba(148, 163, 184, 0.18); }
.variant-host .metric-glow { background: rgba(var(--accent-rgb), 0.18); }

.metric-card:hover {
  transform: translateY(-2px);
  border-color: var(--border-active);
  box-shadow: 0 16px 32px -14px var(--shadow);
}

.metric-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.55rem;
  position: relative;
  z-index: 1;
}

.metric-icon {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--accent-soft);
  color: var(--accent);
  border: 1px solid rgba(var(--accent-rgb), 0.12);
}

.metric-icon svg,
.metric-icon :deep(svg) {
  width: 15px;
  height: 15px;
}

.metric-icon.success {
  background: rgba(var(--success-rgb), 0.1);
  color: var(--success);
  border-color: rgba(var(--success-rgb), 0.15);
}

.metric-icon.muted {
  background: var(--bg-subtle);
  color: var(--text-mute);
  border-color: var(--border-subtle);
}

.metric-badge {
  font-size: 0.58rem;
  font-weight: 800;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  padding: 0.2rem 0.5rem;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  border: 1px solid rgba(var(--accent-rgb), 0.12);
}

.metric-badge.success {
  background: rgba(var(--success-rgb), 0.1);
  color: var(--success);
  border-color: rgba(var(--success-rgb), 0.15);
}

.metric-badge.dim {
  background: var(--bg-subtle);
  color: var(--text-mute);
  border-color: var(--border-subtle);
}

.metric-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex: 1;
  position: relative;
  z-index: 1;
}

.metric-main {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  min-width: 0;
}

.metric-value {
  font-size: 1.65rem;
  font-weight: 800;
  letter-spacing: -0.04em;
  color: var(--text-main);
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.metric-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--text-dim);
}

.metric-visual {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.metric-footer {
  margin-top: auto;
  padding-top: 0.5rem;
  font-size: 0.68rem;
  font-weight: 600;
  color: var(--text-mute);
  position: relative;
  z-index: 1;
}

.metric-footer.success-text {
  color: var(--success);
}

.metric-footer.warn {
  color: var(--warning);
}

.metric-bar {
  height: 3px;
  border-radius: 999px;
  overflow: hidden;
  background: var(--bg-subtle);
  margin-bottom: 0.35rem;
}

.metric-bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.6s cubic-bezier(0.23, 1, 0.32, 1);
}

.metric-bar-fill.success {
  background: linear-gradient(90deg, var(--success), rgba(var(--success-rgb), 0.55));
}

/* Donut chart */
.donut {
  --pct: 0;
  position: relative;
  width: 44px;
  height: 44px;
}

.donut svg {
  width: 100%;
  height: 100%;
  transform: rotate(-90deg);
}

.donut-track {
  fill: none;
  stroke: var(--bg-subtle);
  stroke-width: 3;
}

.donut-fill {
  fill: none;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-dasharray: calc(var(--pct) * 0.974) 97.4;
  transition: stroke-dasharray 0.6s cubic-bezier(0.23, 1, 0.32, 1);
}

.donut-fill.success { stroke: var(--success); }
.donut-fill.muted { stroke: var(--text-mute); opacity: 0.55; }

.donut-label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.6rem;
  font-weight: 800;
  color: var(--text-dim);
  font-variant-numeric: tabular-nums;
}

/* Live pulse bars */
.pulse-stack {
  display: flex;
  align-items: flex-end;
  gap: 3px;
  height: 36px;
  width: 36px;
}

.pulse-bar {
  flex: 1;
  border-radius: 4px 4px 2px 2px;
  background: linear-gradient(180deg, var(--success), rgba(var(--success-rgb), 0.45));
  animation: pulseBar 1.6s ease-in-out infinite;
  min-height: 18%;
}

@keyframes pulseBar {
  0%, 100% { transform: scaleY(1); opacity: 0.85; }
  50% { transform: scaleY(0.72); opacity: 1; }
}

/* Host card */
.host-body {
  flex-direction: column;
  align-items: stretch;
  gap: 0.45rem;
  padding-top: 0;
}

.host-stat {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.host-stat-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 0.5rem;
}

.host-stat-head span {
  font-size: 0.62rem;
  font-weight: 800;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-mute);
}

.host-stat-head strong {
  font-size: 0.74rem;
  font-weight: 800;
  color: var(--text-main);
  font-variant-numeric: tabular-nums;
}

.host-track {
  height: 4px;
  border-radius: 999px;
  background: var(--bg-subtle);
  overflow: hidden;
}

.host-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.6s cubic-bezier(0.23, 1, 0.32, 1);
}

.host-fill.accent {
  background: linear-gradient(90deg, var(--accent), rgba(var(--accent-rgb), 0.55));
}

.host-meta {
  font-size: 0.62rem;
  font-weight: 600;
  color: var(--text-mute);
}

/* Consumers */
.consumers-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0.75rem;
}
.consumer-card {
  padding: 1rem;
  min-height: auto;
}
.consumer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.75rem;
}
.consumer-name {
  font-size: 0.85rem;
  font-weight: 800;
  color: var(--text-main);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 70%;
}

/* Table panel */
.table-panel {
  border-radius: var(--radius-2xl);
  border: 1px solid var(--border);
  background: var(--bg-card);
  padding: 1.25rem 1.25rem 0.5rem;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
}

.panel-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 1.25rem;
  margin-bottom: 1.25rem;
  flex-wrap: wrap;
}

.toolbar-left h2 {
  font-size: 1.1rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  margin: 0 0 0.2rem;
  color: var(--text-main);
}

.toolbar-sub {
  font-size: 0.82rem;
  color: var(--text-mute);
  margin: 0;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.filter-pills {
  display: flex;
  gap: 0.4rem;
  padding: 0.25rem;
  border-radius: var(--radius-md);
  background: var(--bg-input);
  border: 1px solid var(--border);
}

.filter-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.45rem 0.75rem;
  border-radius: calc(var(--radius-md) - 2px);
  font-size: 0.75rem;
  font-weight: 700;
  color: var(--text-dim);
  transition: all 0.2s ease;
}

.filter-pill:hover {
  color: var(--text-main);
  background: var(--bg-subtle);
}

.filter-pill.active {
  background: var(--accent);
  color: #fff;
  box-shadow: 0 4px 12px rgba(var(--accent-rgb), 0.35);
}

.pill-count {
  font-size: 0.65rem;
  padding: 0.1rem 0.35rem;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.15);
  font-variant-numeric: tabular-nums;
}

.filter-pill:not(.active) .pill-count {
  background: var(--bg-subtle);
  color: var(--text-mute);
}

.search-box {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  padding: 0.5rem 0.85rem;
  border-radius: var(--radius-md);
  background: var(--bg-input);
  border: 1px solid var(--border);
  min-width: 200px;
}

.search-box input {
  background: transparent;
  border: none;
  color: var(--text-main);
  font-size: 0.8rem;
  font-weight: 600;
  width: 100%;
  outline: none;
}

.search-box svg {
  color: var(--text-mute);
  flex-shrink: 0;
}

@media (max-width: 1100px) {
  .metrics-bento {
    grid-template-columns: repeat(2, 1fr);
  }
  .consumers-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .metric-card {
    min-height: 120px;
  }
}

@media (max-width: 768px) {
  .dash-hero {
    padding: 1.25rem;
  }

  .hero-body {
    flex-direction: column;
    align-items: stretch;
  }

  .hero-actions {
    width: 100%;
  }

  .dash-action-btn {
    flex: 1;
    justify-content: center;
    min-width: calc(50% - 0.35rem);
  }

  .metrics-bento,
  .consumers-grid {
    grid-template-columns: 1fr;
  }

  .panel-toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-right {
    flex-direction: column;
    align-items: stretch;
  }

  .filter-pills {
    width: 100%;
    justify-content: space-between;
  }

  .filter-pill {
    flex: 1;
    justify-content: center;
  }

  .search-box {
    width: 100%;
    min-width: 0;
  }
}
</style>
