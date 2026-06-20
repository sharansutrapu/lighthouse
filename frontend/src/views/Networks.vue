<template>
  <div class="page-container">
    <div class="page-header glass">
      <div class="header-left">
        <h1>Docker Networks</h1>
        <p class="subtitle">Manage network topologies and isolation</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-warning" @click="pruneNetworks" :disabled="isLoading">
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
              <th>Scope</th>
              <th>Subnet</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody v-if="networks.length > 0">
            <tr v-for="net in networks" :key="net.Id">
              <td data-label="Name"><strong>{{ net.Name }}</strong></td>
              <td data-label="Driver"><span class="badge badge-dim mini">{{ net.Driver }}</span></td>
              <td data-label="Scope">{{ net.Scope }}</td>
              <td data-label="Subnet">
                <span v-if="net.IPAM && net.IPAM.Config && net.IPAM.Config.length > 0" class="text-mute">
                  {{ net.IPAM.Config[0].Subnet }}
                </span>
                <span v-else class="text-mute">—</span>
              </td>
              <td data-label="Actions" class="text-right">
                <button class="action-btn danger" @click="requestRemoveNetwork(net.Id)" data-tooltip="Remove">
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
              <td colspan="5" class="empty-state">No networks found.</td>
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
import { showToast } from '../utils/sharedState';

const networks = ref([]);
const isLoading = ref(true);

const fetchNetworks = async () => {
  isLoading.value = true;
  try {
    const res = await apiFetch('/api/networks');
    if (res.ok) {
      const data = await res.json();
      networks.value = Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []);
    }
  } catch (err) {
    showToast('Error', 'Failed to fetch networks', 'error');
  } finally {
    isLoading.value = false;
  }
};

const confirmModal = ref({
  show: false,
  title: '',
  message: '',
  type: 'warning',
  action: null
});

const openConfirm = (title, message, type, action) => {
  confirmModal.value = { show: true, title, message, type, action };
};

const closeConfirm = () => {
  confirmModal.value.show = false;
  confirmModal.value.action = null;
};

const executeConfirm = async () => {
  if (confirmModal.value.action) {
    await confirmModal.value.action();
  }
  closeConfirm();
};

const requestRemoveNetwork = (id) => {
  openConfirm('Remove Network', `Are you sure you want to remove this network?`, 'error', async () => {
    try {
      const res = await apiFetch(`/api/networks/${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Success', 'Network removed', 'success');
        fetchNetworks();
      } else {
        const err = await res.json();
        showToast('Error', err.error || 'Failed to remove', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  });
};

const pruneNetworks = () => {
  openConfirm('Prune Unused Networks', 'Are you sure you want to prune all unused networks? This action cannot be undone.', 'warning', async () => {
    try {
      const res = await apiFetch('/api/networks/prune', { method: 'POST' });
      if (res.ok) {
        showToast('Success', `Pruned unused networks`, 'success');
        fetchNetworks();
      } else {
        showToast('Error', 'Failed to prune networks', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  });
};

onMounted(() => {
  fetchNetworks();
});
</script>
