<template>
  <div class="scans-view animate-fade-in">
    <div class="view-header">
      <div class="title-group">
        <h1 class="page-title">Vulnerability Scans</h1>
        <p class="page-desc">
          Run on-demand security scans across your containers to identify CVEs.
        </p>
      </div>
      <div class="header-actions">
        <button 
          v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_run_scans"
          class="page-btn primary" 
          @click="scanAll" 
          :disabled="isScanningAll || containers.length === 0"
        >
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" :class="{ spinning: isScanningAll }">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
          </svg>
          {{ isScanningAll ? 'Scanning...' : 'Scan All' }}
        </button>
        <button class="page-btn primary" @click="fetchContainers" :disabled="loading">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" :class="{ spinning: loading }">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
          Refresh List
        </button>
      </div>
    </div>

    <div v-if="loading && containers.length === 0" class="empty-state">
      <span class="pulse-dot"></span> Loading containers...
    </div>

    <div v-else class="scans-grid">
      <article v-for="c in sortedContainers" :key="c.id" class="scan-card shadow-lg" :class="{ 'is-platform': c.is_platform }">
        <div class="card-header">
          <h3>
            {{ c.name.replace(/^\//, '') }}
            <span v-if="c.is_platform" class="platform-badge" style="font-size: 0.6rem; padding: 0.1rem 0.3rem; margin-left: 0.3rem;">⚡ PLATFORM</span>
          </h3>
          <span :class="['status-badge', c.state]">{{ c.state }}</span>
        </div>
        <div class="card-body">
          <div class="info-row">
            <span class="label">Image</span>
            <span class="value mono" :title="c.image">{{ c.image.length > 35 ? c.image.substring(0, 32) + '...' : c.image }}</span>
          </div>

          <div v-if="scanResults[c.id]" class="scan-result-summary">
            <div class="severity-badges">
               <span class="sev-badge critical" v-if="scanResults[c.id].counts.CRITICAL > 0">
                 {{ scanResults[c.id].counts.CRITICAL }} Critical
               </span>
               <span class="sev-badge high" v-if="scanResults[c.id].counts.HIGH > 0">
                 {{ scanResults[c.id].counts.HIGH }} High
               </span>
               <span class="sev-badge medium" v-if="scanResults[c.id].counts.MEDIUM > 0">
                 {{ scanResults[c.id].counts.MEDIUM }} Med
               </span>
               <span class="sev-badge low" v-if="scanResults[c.id].counts.LOW > 0">
                 {{ scanResults[c.id].counts.LOW }} Low
               </span>
               <span class="sev-badge success" v-if="scanResults[c.id].total === 0">
                 0 Vulnerabilities
               </span>
            </div>
          </div>
          <div v-else class="scan-result-summary pending">
            <span>No scan run yet</span>
          </div>
        </div>

        <div class="card-footer">
          <button 
            class="page-btn cancel sm" 
            v-if="scanResults[c.id]"
            @click="viewDetails(c)"
          >
            Details
          </button>
          <button 
            v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_run_scans"
            class="page-btn primary sm" 
            :disabled="scanning[c.id]"
            @click="triggerScan(c)"
          >
            {{ scanning[c.id] ? 'Scanning...' : (scanResults[c.id] ? 'Rescan' : 'Run Scan') }}
          </button>
        </div>
      </article>
    </div>

    <!-- Details Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showModal && activeScan" class="modal-overlay" @click.self="closeModal">
          <div class="modal-content shadow-2xl" style="max-width: 800px; max-height: 85vh; overflow-y: auto;">
            <div class="modal-header">
              <h3>Scan Results: {{ activeContainerName }}</h3>
            </div>
            
            <div class="vuln-list" v-if="activeScan.Results">
              <template v-for="target in activeScan.Results" :key="target.Target">
                <div v-if="target.Vulnerabilities" class="target-group">
                  <h4 class="target-name">{{ target.Target }}</h4>
                  <div class="vuln-item" v-for="v in target.Vulnerabilities" :key="v.VulnerabilityID">
                    <div class="vuln-header">
                      <span :class="['vuln-badge', v.Severity.toLowerCase()]">{{ v.Severity }}</span>
                      <a :href="v.PrimaryURL" target="_blank" class="vuln-id">{{ v.VulnerabilityID }}</a>
                      <span class="vuln-pkg">{{ v.PkgName }}</span>
                    </div>
                    <p class="vuln-desc">{{ v.Title || 'No description available' }}</p>
                    <div class="vuln-meta">
                      <span>Installed: <strong class="mono">{{ v.InstalledVersion }}</strong></span>
                      <span v-if="v.FixedVersion">Fixed: <strong class="mono">{{ v.FixedVersion }}</strong></span>
                    </div>
                  </div>
                </div>
              </template>
            </div>
            <div v-else class="empty-state">
              No vulnerabilities found!
            </div>

            <div class="modal-actions" style="margin-top: 1.5rem;">
              <button class="modal-btn cancel" @click="closeModal">Close</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue';
import { useContainers } from '../composables/useContainers';
import { apiFetch } from '../utils/apiFetch';
import { secureStorage } from '../utils/storage';
import { showToast, sharedState } from '../utils/sharedState';

const { containers, loading, fetchContainers } = useContainers();

const sortedContainers = computed(() => {
  return [...containers.value].sort((a, b) => {
    if (a.is_platform && !b.is_platform) return -1;
    if (!a.is_platform && b.is_platform) return 1;
    return a.name.localeCompare(b.name);
  });
});

const scanning = ref({});
const scanResults = ref({});
const isScanningAll = ref(false);

const showModal = ref(false);
const activeScan = ref(null);
const activeContainerName = ref('');

const scanAll = async () => {
  isScanningAll.value = true;
  showToast('Scan All Started', `Initiated vulnerability scans for ${containers.value.length} containers`, 'info');
  
  const promises = containers.value.map(c => {
    // Only trigger if not already scanning
    if (!scanning.value[c.id]) {
      return triggerScan(c);
    }
    return Promise.resolve();
  });
  
  await Promise.all(promises);
  isScanningAll.value = false;
  showToast('Scan All Complete', 'Finished launching vulnerability scans for all containers.', 'success');
};

const parseScanResults = (data) => {
  let counts = { CRITICAL: 0, HIGH: 0, MEDIUM: 0, LOW: 0, UNKNOWN: 0 };
  let total = 0;
  
  if (data.Results) {
    for (const target of data.Results) {
      if (target.Vulnerabilities) {
        for (const v of target.Vulnerabilities) {
          total++;
          if (counts[v.Severity] !== undefined) {
            counts[v.Severity]++;
          }
        }
      }
    }
  }
  return { data, counts, total };
};

const loadScanForContainer = async (c) => {
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch(`/api/images/scans?image=${encodeURIComponent(c.image)}`, {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      const text = await res.text();
      if (text && text.trim() !== '') {
        const data = JSON.parse(text);
        scanResults.value[c.id] = parseScanResults(data);
        return true;
      }
    }
  } catch (err) {
    console.warn(`No scan found for ${c.image}:`, err);
  }
  return false;
};

const loadAllScanHistories = async () => {
  const promises = containers.value.map(async (c) => {
    if (c.image) {
      await loadScanForContainer(c);
    }
  });
  await Promise.all(promises);
};

const triggerScan = async (c) => {
  scanning.value[c.id] = true;
  scanResults.value[c.id] = null; // Clear old result during new scan
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch(`/api/containers/${c.id}/scan?image=${encodeURIComponent(c.image)}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` }
    });
    
    if (!res.ok) {
      const errData = await res.json().catch(() => ({}));
      throw new Error(errData.error || 'Scan failed');
    }
    
    showToast('Scan Started', `Vulnerability scan initiated for ${c.name.replace(/^\//, '')}`, 'success');
    
    // Poll for background scan completion
    let attempts = 0;
    const timer = setInterval(async () => {
      const loaded = await loadScanForContainer(c);
      if (loaded) {
        clearInterval(timer);
        scanning.value[c.id] = false;
        showToast('Scan Complete', `Found ${scanResults.value[c.id].total} vulnerabilities in ${c.name.replace(/^\//, '')}`, 'success');
      } else {
        attempts++;
        if (attempts > 30) {
          clearInterval(timer);
          scanning.value[c.id] = false;
          showToast('Timeout', 'Scan is taking too long.', 'warning');
        }
      }
    }, 5000);
  } catch (err) {
    showToast('Scan Failed', err.message, 'error');
    scanning.value[c.id] = false;
  }
};

const viewDetails = (c) => {
  if (!scanResults.value[c.id]) return;
  activeScan.value = scanResults.value[c.id].data;
  activeContainerName.value = c.name.replace(/^\//, '');
  showModal.value = true;
};

const closeModal = () => {
  showModal.value = false;
  activeScan.value = null;
};

watch(containers, (newVal) => {
  if (newVal && newVal.length > 0) {
    loadAllScanHistories();
  }
}, { immediate: true });

onMounted(() => {
  fetchContainers();
});
</script>

<style scoped>
.scans-view { padding: 0; }
.view-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 2rem;
}
.scans-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 1.5rem;
}
.scan-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 1.25rem;
  display: flex;
  flex-direction: column;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
  gap: 1rem;
}
.card-header h3 { 
  margin: 0; 
  font-weight: 800; 
  font-size: 1.1rem; 
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}
.status-badge {
  padding: 0.2rem 0.6rem;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 800;
  text-transform: uppercase;
  flex-shrink: 0;
}
.status-badge.running { background: rgba(var(--success-rgb), 0.2); color: var(--success); }
.status-badge.exited, .status-badge.stopped { background: rgba(var(--error-rgb), 0.2); color: var(--error); }

.card-body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  margin-bottom: 1.5rem;
  flex-grow: 1;
}
.info-row {
  display: flex;
  flex-direction: column;
}
.info-row .label { font-size: 0.75rem; color: var(--text-mute); font-weight: 600; text-transform: uppercase; }
.info-row .value { font-size: 0.85rem; color: var(--text-main); }

.scan-result-summary {
  padding: 1rem;
  border-radius: var(--radius-md);
  background: var(--bg-input);
  border: 1px solid var(--border);
}
.scan-result-summary.pending {
  display: flex;
  justify-content: center;
  color: var(--text-mute);
  font-size: 0.85rem;
  font-style: italic;
}
.severity-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}
.sev-badge {
  font-size: 0.75rem;
  padding: 0.2rem 0.5rem;
  border-radius: 4px;
  font-weight: 700;
}
.sev-badge.critical { background: rgba(var(--error-rgb), 0.2); color: var(--error); }
.sev-badge.high { background: rgba(249, 115, 22, 0.2); color: #f97316; } /* Orange */
.sev-badge.medium { background: rgba(var(--warning-rgb), 0.2); color: var(--warning); }
.sev-badge.low { background: rgba(var(--text-mute-rgb), 0.2); color: var(--text-main); }
.sev-badge.success { background: rgba(var(--success-rgb), 0.2); color: var(--success); }

.card-footer {
  display: flex;
  gap: 0.75rem;
  justify-content: flex-end;
}

.spinning { animation: spin 0.9s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

/* Modal Styles */
.target-group { margin-bottom: 1.5rem; }
.target-name { 
  font-size: 1rem; 
  color: var(--text-main); 
  padding-bottom: 0.5rem; 
  border-bottom: 1px solid var(--border);
  margin-bottom: 1rem;
}
.vuln-item {
  background: var(--bg-input);
  border: 1px solid var(--border);
  padding: 1rem;
  border-radius: var(--radius-md);
  margin-bottom: 0.75rem;
}
.vuln-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}
.vuln-badge {
  font-size: 0.65rem;
  padding: 0.15rem 0.4rem;
  border-radius: 4px;
  font-weight: 800;
  text-transform: uppercase;
}
.vuln-badge.critical { background: var(--error); color: white; }
.vuln-badge.high { background: #f97316; color: white; }
.vuln-badge.medium { background: var(--warning); color: #1e293b; }
.vuln-badge.low { background: var(--bg-subtle); color: var(--text-main); }

.vuln-id { font-weight: 700; color: var(--accent); text-decoration: none; }
.vuln-id:hover { text-decoration: underline; }
.vuln-pkg { font-size: 0.85rem; color: var(--text-dim); }
.vuln-desc { font-size: 0.85rem; color: var(--text-main); margin-bottom: 0.75rem; line-height: 1.4; }
.vuln-meta {
  display: flex;
  gap: 1.5rem;
  font-size: 0.8rem;
  color: var(--text-mute);
}

@media (max-width: 640px) {
  .view-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 1rem;
  }
  .card-footer {
    flex-direction: column;
  }
  .card-footer .page-btn {
    width: 100%;
  }
  .vuln-meta {
    flex-direction: column;
    gap: 0.5rem;
  }
}
</style>
