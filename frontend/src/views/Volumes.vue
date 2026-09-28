<template>
  <div class="page-container">
    <div class="page-header glass">
      <div class="header-left">
        <h1>Docker Volumes <span class="badge badge-dim" style="font-size: 0.8rem; margin-left: 0.5rem; vertical-align: middle;">{{ volumes.length }} Total</span></h1>
        <p class="subtitle">{{ isHubMode ? 'Manage persistent storage across every connected node' : 'Manage persistent storage and mounts' }}</p>
      </div>
      <div class="header-actions" style="display: flex; gap: 1rem; align-items: center;">
        <label v-if="isHubMode" class="node-filter">
          <span>Node</span>
          <select v-model="nodeFilter">
            <option value="all">All nodes</option>
            <option v-for="node in nodeOptions" :key="node" :value="node">{{ node }}</option>
          </select>
        </label>
        <div class="search-box glass" style="margin: 0; min-width: 250px;">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
            <circle cx="11" cy="11" r="8"></circle>
            <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
          </svg>
          <input
            type="text"
            v-model="searchQuery"
            placeholder="Search volumes..."
          />
        </div>
        <button class="btn btn-warning" @click="pruneVolumes" :disabled="isLoading">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="18" height="18">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          </svg>
          Prune Unused
        </button>
      </div>
    </div>

    <div class="content-wrapper">
      <div v-if="isLoading" class="loading-state">
        <div class="spinner"></div>
      </div>
      
      <div class="premium-table-container" v-else>
        <table class="premium-table">
          <thead>
            <tr>
              <th>Name</th>
              <th v-if="isHubMode">Node</th>
              <th>Used By</th>
              <th>Driver</th>
              <th>Mountpoint</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody v-if="filteredVolumes.length > 0">
            <tr v-for="vol in filteredVolumes" :key="`${vol.node_id || 'local'}:${vol.Name}`">
              <td data-label="Name"><strong>{{ vol.Name.length > 30 ? vol.Name.substring(0,30) + '...' : vol.Name }}</strong></td>
              <td v-if="isHubMode" data-label="Node"><span class="badge badge-dim mini">{{ resourceNode(vol) }}</span></td>
              <td data-label="Used By">
                <span v-if="getContainersUsingVolume(vol).length > 0" class="text-mute"><small>{{ getContainersUsingVolume(vol).join(', ') }}</small></span>
                <span v-else class="text-mute"><small>—</small></span>
              </td>
              <td data-label="Driver"><span class="badge badge-dim mini">{{ vol.Driver }}</span></td>
              <td data-label="Mountpoint" class="text-mute"><small>{{ vol.Mountpoint }}</small></td>
              <td data-label="Actions" class="text-right">
                <button class="action-btn danger" @click="requestRemoveVolume(vol)" data-tooltip="Remove">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="16" height="16">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                  </svg>
                </button>
              </td>
            </tr>
          </tbody>
          <tbody v-else>
            <tr>
              <td :colspan="isHubMode ? 6 : 5" class="empty-state">No volumes found.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Confirmation Modal -->
    <div v-if="confirmModal.show" class="modal-overlay" @click.self="closeConfirm">
      <div class="modal-content shadow-2xl">
        <div :class="['modal-icon', confirmModal.type]">
          <svg v-if="confirmModal.type === 'error'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="24" height="24">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="24" height="24">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
            <line x1="12" y1="9" x2="12" y2="13"></line>
            <line x1="12" y1="17" x2="12.01" y2="17"></line>
          </svg>
        </div>
        <div class="modal-text-center">
          <h3>{{ confirmModal.title }}</h3>
          <p>{{ confirmModal.message }}</p>
          <div v-if="confirmModal.showRemoveContainers" class="modal-checkbox-wrapper" style="margin-top: 1rem; text-align: left;">
            <label class="checkbox-label" style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
              <input type="checkbox" v-model="confirmModal.removeContainers" />
              Remove stopped containers first
            </label>
            <p class="text-mute" style="font-size: 0.8rem; margin-top: 0.2rem; margin-left: 1.5rem;">Prunes stopped containers to release held volumes before pruning.</p>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="closeConfirm">Cancel</button>
          <button :class="['modal-btn confirm', confirmModal.type]" @click="executeConfirm">Confirm</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// Docker volume management page: list/search volumes, remove one, or prune
// all unused ones. Mirrors Images.vue's confirm-modal pattern.
import { ref, computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { apiFetch } from '../utils/apiFetch';
import { formatBytes, sharedState, showToast } from '../utils/sharedState';
import { useContainers } from '../composables/useContainers';

const volumes = ref([]);
const isLoading = ref(true);
const searchQuery = ref('');
const route = useRoute();
const router = useRouter();
const nodeFilter = ref(typeof route.query.node === 'string' ? route.query.node : 'all');
const { containers } = useContainers();
const isHubMode = computed(() => sharedState.deploymentMode === 'hub');
const resourceNode = (resource) => resource.node_id || sharedState.localNodeId || 'local';
const nodeOptions = computed(() => [...new Set([
  sharedState.localNodeId,
  ...volumes.value.map(resourceNode),
  ...(nodeFilter.value !== 'all' ? [nodeFilter.value] : [])
].filter(Boolean))].sort());

const getContainersUsingVolume = (volume) => {
  if (!containers.value) return [];
  const using = [];
  for (const c of containers.value) {
    if (isHubMode.value && resourceNode(c) !== resourceNode(volume)) continue;
    if (c.mounts) {
      for (const m of c.mounts) {
        if ((m.Type === 'volume' || m.type === 'volume') && (m.Name === volume.Name || m.name === volume.Name)) {
          using.push(c.name ? c.name.replace(/^\//, '') : c.id.substring(0, 12));
          break;
        }
      }
    }
  }
  return using;
};

const filteredVolumes = computed(() => {
  const query = searchQuery.value.toLowerCase().trim();
  return volumes.value.filter(vol => {
    const name = (vol.Name || '').toLowerCase();
    const driver = (vol.Driver || '').toLowerCase();
    const matchesNode = nodeFilter.value === 'all' || resourceNode(vol) === nodeFilter.value;
    return matchesNode && (!query || name.includes(query) || driver.includes(query));
  });
});

const nodeURL = (path, nodeID) => {
  if (!isHubMode.value || !nodeID) return path;
  return `${path}${path.includes('?') ? '&' : '?'}node_id=${encodeURIComponent(nodeID)}`;
};

watch(nodeFilter, (node) => {
  const query = { ...route.query };
  if (node === 'all') delete query.node;
  else query.node = node;
  router.replace({ query });
});

watch(() => route.query.node, (node) => {
  const nextNode = typeof node === 'string' ? node : 'all';
  if (nodeFilter.value !== nextNode) nodeFilter.value = nextNode;
});

// fetchVolumes loads the full volume list from the backend.
const fetchVolumes = async () => {
  isLoading.value = true;
  try {
    const res = await apiFetch('/api/volumes');
    if (res.ok) {
      const data = await res.json();
      volumes.value = Array.isArray(data?.Volumes) ? data.Volumes : (Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []));
    }
  } catch (err) {
    showToast('Error', 'Failed to fetch volumes', 'error');
  } finally {
    isLoading.value = false;
  }
};

// confirmModal drives the single reusable confirmation dialog for both the
// per-volume remove and the bulk prune actions.
const confirmModal = ref({
  show: false,
  title: '',
  message: '',
  type: 'warning',
  action: null,
  showRemoveContainers: false,
  removeContainers: false
});

// openConfirm configures and shows the confirmation modal for a destructive action.
const openConfirm = (title, message, type, action, showRemoveContainers = false) => {
  confirmModal.value = { 
    show: true, 
    title, 
    message, 
    type, 
    action,
    showRemoveContainers,
    removeContainers: false
  };
};

// closeConfirm hides the modal without running its action.
const closeConfirm = () => {
  confirmModal.value.show = false;
  confirmModal.value.action = null;
};

// executeConfirm runs the modal's pending action then closes it.
const executeConfirm = async () => {
  if (confirmModal.value.action) {
    await confirmModal.value.action({
      removeContainers: confirmModal.value.removeContainers
    });
  }
  closeConfirm();
};

// requestRemoveVolume opens the confirmation modal for deleting one volume by name.
const requestRemoveVolume = (volume) => {
  const nodeID = resourceNode(volume);
  openConfirm('Remove Volume', `Remove ${volume.Name} from ${nodeID}?`, 'error', async () => {
    try {
      const res = await apiFetch(nodeURL(`/api/volumes/${encodeURIComponent(volume.Name)}`, nodeID), { method: 'DELETE' });
      if (res.ok) {
        showToast('Success', 'Volume removed', 'success');
        fetchVolumes();
      } else {
        const err = await res.json();
        showToast('Error', err.error || 'Failed to remove', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  });
};

// pruneVolumes opens the confirmation modal for removing all unused volumes.
const pruneVolumes = () => {
  const targets = isHubMode.value
    ? (nodeFilter.value === 'all' ? nodeOptions.value : [nodeFilter.value])
    : [null];
  const targetLabel = isHubMode.value
    ? (nodeFilter.value === 'all' ? `all ${targets.length} nodes` : nodeFilter.value)
    : 'this node';
  openConfirm('Prune Unused Volumes', `Prune unused volumes on ${targetLabel}? This action cannot be undone.`, 'warning', async (options) => {
    try {
      let reclaimed = 0;
      const warnings = [];
      const failures = [];
      for (const nodeID of targets) {
        const res = await apiFetch(nodeURL('/api/volumes/prune', nodeID), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ remove_containers: options.removeContainers })
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          failures.push(`${nodeID || 'local'}: ${data.error || 'prune failed'}`);
          continue;
        }
        const report = data.Report || data;
        reclaimed += report.SpaceReclaimed || 0;
        if (data.Warning) warnings.push(`${nodeID || 'local'}: ${data.Warning}`);
      }
      if (failures.length > 0) {
        showToast('Prune incomplete', failures.join(' · '), 'error');
      } else {
        showToast('Success', `Pruned volumes on ${targets.length} node${targets.length === 1 ? '' : 's'}. Freed ${formatBytes(reclaimed)}`, 'success');
      }
      if (warnings.length > 0) showToast('Warning', warnings.join(' · '), 'warning');
      fetchVolumes();
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  }, true);
};

onMounted(() => {
  fetchVolumes();
});
</script>

<style scoped>
.node-filter {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-mute);
  font-size: 0.68rem;
  font-weight: 800;
  text-transform: uppercase;
}

.node-filter select {
  min-height: 38px;
  padding: 0 2rem 0 0.7rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-input);
  color: var(--text-main);
  font: inherit;
  text-transform: none;
}
</style>
