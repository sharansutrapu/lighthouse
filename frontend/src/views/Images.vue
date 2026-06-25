<template>
  <div class="page-container">
    <div class="page-header glass">
      <div class="header-left">
        <h1>Docker Images</h1>
        <p class="subtitle">Manage local images and clear disk space</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-warning" @click="pruneImages" :disabled="isLoading">
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
              <th>ID</th>
              <th>Tags</th>
              <th>Size</th>
              <th>Created</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody v-if="images.length > 0">
            <tr v-for="img in images" :key="img.Id">
              <td data-label="ID">{{ img.Id.split(':')[1]?.substring(0, 12) || img.Id }}</td>
              <td data-label="Tags">
                <div class="tags-container">
                  <span class="badge badge-dim mini" v-for="tag in img.RepoTags || []" :key="tag">
                    {{ tag }}
                  </span>
                  <span class="badge badge-warning mini" v-if="!img.RepoTags || img.RepoTags.length === 0">
                    &lt;none&gt;:&lt;none&gt;
                  </span>
                </div>
              </td>
              <td data-label="Size">{{ formatBytes(img.Size) }}</td>
              <td data-label="Created">{{ formatDate(img.Created) }}</td>
              <td data-label="Actions" class="text-right">
                <button class="action-btn danger" @click="requestRemoveImage(img.Id)" data-tooltip="Remove">
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
              <td colspan="5" class="empty-state">No images found.</td>
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
            <p class="text-mute" style="font-size: 0.8rem; margin-top: 0.2rem; margin-left: 1.5rem; margin-bottom: 0.5rem;">Prunes stopped containers to release held images before pruning.</p>
          </div>
          <div v-if="confirmModal.showAllUnused" class="modal-checkbox-wrapper" style="text-align: left;">
            <label class="checkbox-label" style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
              <input type="checkbox" v-model="confirmModal.allUnused" />
              Prune All Unused Images (not just dangling)
            </label>
            <p class="text-mute" style="font-size: 0.8rem; margin-top: 0.2rem; margin-left: 1.5rem;">Removes any image that is not referenced by any container.</p>
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

const images = ref([]);
const isLoading = ref(true);

const fetchImages = async () => {
  isLoading.value = true;
  try {
    const res = await apiFetch('/api/images');
    if (res.ok) {
      const data = await res.json();
      images.value = Array.isArray(data?.Items) ? data.Items : (Array.isArray(data) ? data : []);
    }
  } catch (err) {
    showToast('Error', 'Failed to fetch images', 'error');
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
  removeContainers: false,
  showAllUnused: false,
  allUnused: false
});

const openConfirm = (title, message, type, action, showRemoveContainers = false, showAllUnused = false) => {
  confirmModal.value = { 
    show: true, 
    title, 
    message, 
    type, 
    action,
    showRemoveContainers,
    removeContainers: false,
    showAllUnused,
    allUnused: false
  };
};

const closeConfirm = () => {
  confirmModal.value.show = false;
  confirmModal.value.action = null;
};

const executeConfirm = async () => {
  if (confirmModal.value.action) {
    await confirmModal.value.action({
      removeContainers: confirmModal.value.removeContainers,
      allUnused: confirmModal.value.allUnused
    });
  }
  closeConfirm();
};

const requestRemoveImage = (id) => {
  openConfirm('Remove Image', `Are you sure you want to remove the image ${id.substring(0, 12)}?`, 'error', async () => {
    try {
      const res = await apiFetch(`/api/images/${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Success', 'Image removed', 'success');
        fetchImages();
      } else {
        const err = await res.json();
        showToast('Error', err.error || 'Failed to remove', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  });
};

const pruneImages = () => {
  openConfirm('Prune Unused Images', 'Are you sure you want to prune unused images? This action cannot be undone.', 'warning', async (options) => {
    try {
      const res = await apiFetch('/api/images/prune', { 
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ 
          remove_containers: options.removeContainers,
          all_unused: options.allUnused
        })
      });
      if (res.ok) {
        const data = await res.json();
        const report = data.Report || data;
        showToast('Success', `Pruned images. Freed ${formatBytes(report.SpaceReclaimed || 0)}`, 'success');
        if (data.Warning) {
          showToast('Warning', data.Warning, 'warning');
        }
        fetchImages();
      } else {
        showToast('Error', 'Failed to prune images', 'error');
      }
    } catch (err) {
      showToast('Error', 'Connection error', 'error');
    }
  }, true, true);
};

const formatDate = (ts) => {
  if (!ts) return 'Unknown';
  return new Date(ts * 1000).toLocaleString();
};

onMounted(() => {
  fetchImages();
});
</script>

<style scoped>
.tags-container {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
</style>
