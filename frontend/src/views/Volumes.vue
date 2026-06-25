<template>
  <div class="page-container">
    <div class="page-header glass">
      <div class="header-left">
        <h1>Docker Volumes</h1>
        <p class="subtitle">Manage persistent storage and mounts</p>
      </div>
      <div class="header-actions">
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
              <th>Driver</th>
              <th>Mountpoint</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody v-if="volumes.length > 0">
            <tr v-for="vol in volumes" :key="vol.Name">
              <td data-label="Name"><strong>{{ vol.Name.length > 30 ? vol.Name.substring(0,30) + '...' : vol.Name }}</strong></td>
              <td data-label="Driver"><span class="badge badge-dim mini">{{ vol.Driver }}</span></td>
              <td data-label="Mountpoint" class="text-mute"><small>{{ vol.Mountpoint }}</small></td>
              <td data-label="Actions" class="text-right">
                <button class="action-btn danger" @click="requestRemoveVolume(vol.Name)" data-tooltip="Remove">
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
              <td colspan="4" class="empty-state">No volumes found.</td>
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
import { ref, onMounted } from 'vue';
import { apiFetch } from '../utils/apiFetch';
import { formatBytes, showToast } from '../utils/sharedState';

const volumes = ref([]);
const isLoading = ref(true);

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

const confirmModal = ref({
  show: false,
  title: '',
  message: '',
  type: 'warning',
  action: null,
  showRemoveContainers: false,
  removeContainers: false
});

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

const closeConfirm = () => {
  confirmModal.value.show = false;
  confirmModal.value.action = null;
};

const executeConfirm = async () => {
  if (confirmModal.value.action) {
    await confirmModal.value.action({
      removeContainers: confirmModal.value.removeContainers
    });
  }
  closeConfirm();
};

const requestRemoveVolume = (name) => {
  openConfirm('Remove Volume', `Are you sure you want to remove this volume?`, 'error', async () => {
    try {
      const res = await apiFetch(`/api/volumes/${encodeURIComponent(name)}`, { method: 'DELETE' });
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

const pruneVolumes = () => {
  openConfirm('Prune Unused Volumes', 'Are you sure you want to prune all unused volumes? This action cannot be undone.', 'warning', async (options) => {
    try {
      const res = await apiFetch('/api/volumes/prune', { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ remove_containers: options.removeContainers })
      });
      if (res.ok) {
        const data = await res.json();
        const report = data.Report || data;
        showToast('Success', `Pruned volumes. Freed ${formatBytes(report.SpaceReclaimed || 0)}`, 'success');
        if (data.Warning) {
          showToast('Warning', data.Warning, 'warning');
        }
        fetchVolumes();
      } else {
        showToast('Error', 'Failed to prune volumes', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  }, true);
};

onMounted(() => {
  fetchVolumes();
});
</script>
