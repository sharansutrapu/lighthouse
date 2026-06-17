<template>
  <div class="gitops-view animate-fade-in">
    <div class="view-header">
      <div class="title-group">
        <h1 class="page-title">Deployments & Stacks</h1>
        <p class="page-desc">
          Continuously deploy from Git repositories or deploy raw docker-compose stacks.
        </p>
      </div>
      <div class="header-actions">
        <button v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_create_deployments" class="page-btn primary" @click="openModal(null)">
          <svg viewBox="0 0 24 24" width="16" height="16" stroke="currentColor" stroke-width="2.5" fill="none">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          New Deployment
        </button>
      </div>
    </div>

    <div v-if="loading" class="empty-state">
      <span class="pulse-dot"></span> Loading deployments...
    </div>

    <div v-else-if="projects.length === 0" class="empty-state">
      <div class="empty-icon">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="64" height="64">
          <circle cx="12" cy="12" r="10"></circle>
          <line x1="12" y1="8" x2="12" y2="12"></line>
          <line x1="12" y1="16" x2="12.01" y2="16"></line>
        </svg>
      </div>
      <h3>No Deployments</h3>
      <p v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_create_deployments">Connect a Git repository or paste a compose file to deploy a stack.</p>
      <button v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_create_deployments" class="page-btn primary" @click="openModal(null)">Create Deployment</button>
    </div>

    <div v-else class="projects-grid">
      <article v-for="p in projects" :key="p.id" class="project-card shadow-lg">
        <div class="card-header">
          <h3>{{ p.name }}</h3>
          <span :class="['status-badge', p.status]">{{ p.status }}</span>
        </div>
        <div class="card-body">
          <div v-if="p.source_type === 'inline'" class="info-row">
            <span class="label">Source Type</span>
            <span class="value mono">Inline Compose Stack</span>
          </div>
          <template v-else>
            <div class="info-row">
              <span class="label">Repository</span>
              <span class="value mono">{{ p.repo_url }}</span>
            </div>
            <div class="info-row">
              <span class="label">Branch</span>
              <span class="value mono">{{ p.branch }}</span>
            </div>
            <div class="info-row">
              <span class="label">Compose File</span>
              <span class="value mono">{{ p.compose_path || 'docker-compose.yml' }}</span>
            </div>
          </template>
          
          <div class="info-row" v-if="p.last_commit">
            <span class="label">{{ p.source_type === 'inline' ? 'Hash Signature' : 'Last Commit' }}</span>
            <span class="value mono">{{ p.last_commit.substring(0, 7) }}</span>
          </div>
        </div>
        <div class="card-footer">
          <button v-if="sharedState.currentUser?.is_admin || sharedState.currentUser?.can_delete_deployments" class="page-btn cancel sm" @click="triggerDelete(p.id)">Remove</button>
          <button v-if="p.source_type === 'inline' && (sharedState.currentUser?.is_admin || sharedState.currentUser?.can_edit_deployments)" class="page-btn cancel sm" @click="openModal(p)">Edit</button>
          <button v-if="p.source_type !== 'inline' && (sharedState.currentUser?.is_admin || sharedState.currentUser?.can_edit_deployments)" class="page-btn cancel sm" @click="triggerSync(p.id)">Sync</button>
          <button class="page-btn primary sm" @click="viewLogs(p.id)">History</button>
        </div>
      </article>
    </div>

    <!-- Add/Edit Deployment Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
          <div class="modal-content shadow-2xl" style="max-width: 650px;">
            <div class="modal-header">
              <h3>{{ form.id ? 'Edit Inline Stack' : 'Create Deployment' }}</h3>
            </div>
            <form @submit.prevent="submitProject" class="settings-form">
              
              <div class="input-group">
                <label>Deployment Name</label>
                <input v-model="form.name" type="text" class="premium-input" placeholder="My App" required :disabled="form.id !== null" />
              </div>
              
              <div class="input-group" v-if="!form.id">
                <label>Source Type</label>
                <select v-model="form.source_type" class="premium-input">
                  <option value="git">Git Repository</option>
                  <option value="inline">Inline Compose File</option>
                </select>
              </div>

              <template v-if="form.source_type === 'inline'">
                <div class="input-group">
                  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
                    <label style="margin: 0;">Compose YAML</label>
                    <div>
                      <input type="file" ref="fileInput" accept=".yml,.yaml" style="display: none" @change="handleFileUpload" />
                      <button type="button" class="page-btn primary sm" @click="$refs.fileInput.click()">
                        Upload File
                      </button>
                    </div>
                  </div>
                  <textarea v-model="form.compose_content" class="premium-input mono" rows="12" placeholder="version: '3'\nservices:\n  web:\n    image: nginx\n..." required></textarea>
                </div>
              </template>

              <template v-else>
                <div class="input-group">
                  <label>Git Repository URL</label>
                  <input v-model="form.repo_url" type="text" class="premium-input" placeholder="https://github.com/user/repo.git" required />
                </div>
                <div class="input-group">
                  <label>Branch</label>
                  <input v-model="form.branch" type="text" class="premium-input" placeholder="main" required />
                </div>
                <div class="input-group">
                  <label>Auth Token (Optional)</label>
                  <input v-model="form.auth_token" type="password" class="premium-input" placeholder="For private repositories" />
                </div>
                <div class="input-group">
                  <label>Compose File Path</label>
                  <input v-model="form.compose_path" type="text" class="premium-input" placeholder="docker-compose.yml" />
                </div>
              </template>

              <div class="modal-actions">
                <button type="button" class="modal-btn cancel" @click="showModal = false">Cancel</button>
                <button type="submit" class="modal-btn confirm" :disabled="submitting">
                  {{ submitting ? 'Deploying...' : (form.id ? 'Save & Redeploy' : 'Deploy') }}
                </button>
              </div>
            </form>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- History Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showHistoryModal" class="modal-overlay" @click.self="showHistoryModal = false">
          <div class="modal-content shadow-2xl" style="max-width: 700px; max-height: 80vh; overflow-y: auto;">
            <div class="modal-header">
              <h3>Deployment History</h3>
            </div>
            <div v-if="historyLoading" class="empty-state">Loading...</div>
            <div v-else-if="deployments.length === 0" class="empty-state">No deployments yet.</div>
            <div v-else class="history-list">
              <div v-for="d in deployments" :key="d.id" class="history-item">
                <div class="history-head">
                  <span :class="['status-badge', d.status]">{{ d.status }}</span>
                  <span class="mono">{{ d.commit_sha.substring(0, 7) }}</span>
                  <span class="time">{{ new Date(d.created_at).toLocaleString() }}</span>
                </div>
                <pre class="history-logs mono">{{ d.logs }}</pre>
              </div>
            </div>
            <div class="modal-actions">
              <button type="button" class="modal-btn cancel" @click="showHistoryModal = false">Close</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
    <!-- Unified Action Confirmation Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showConfirm" class="modal-overlay">
          <div class="modal-content shadow-2xl">
            <div class="modal-icon delete">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </div>
            <div class="modal-text-center">
              <h3>Confirm Operation</h3>
              <p>Are you sure you want to <strong>remove</strong> this deployment? This action cannot be undone.</p>
            </div>
            <div class="modal-divider"></div>
            <div class="modal-actions">
              <button @click="showConfirm = false" class="modal-btn cancel">Cancel</button>
              <button @click="executeDelete" class="modal-btn confirm delete">Confirm remove</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Global Action Loader -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="isActionLoading" class="modal-overlay" style="z-index: 9999; background: rgba(0,0,0,0.6);">
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
import { ref, onMounted } from 'vue';
import { apiFetch } from '../utils/apiFetch';
import { secureStorage } from '../utils/storage';
import { showToast, sharedState } from '../utils/sharedState';

const projects = ref([]);
const loading = ref(true);
const showModal = ref(false);
const submitting = ref(false);
const showConfirm = ref(false);
const pendingProjectId = ref(null);
const isActionLoading = ref(false);

const fileInput = ref(null);

const form = ref({
  id: null,
  name: '',
  source_type: 'git',
  repo_url: '',
  branch: 'main',
  auth_token: '',
  compose_path: 'docker-compose.yml',
  compose_content: ''
});

const showHistoryModal = ref(false);
const historyLoading = ref(false);
const deployments = ref([]);

const loadProjects = async () => {
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch('/api/gitops/projects', {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      projects.value = await res.json();
    }
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const openModal = (project) => {
  if (project) {
    form.value = { 
      id: project.id, 
      name: project.name, 
      source_type: project.source_type || 'git',
      repo_url: project.repo_url, 
      branch: project.branch, 
      auth_token: '', 
      compose_path: project.compose_path || 'docker-compose.yml',
      compose_content: project.compose_content || ''
    };
  } else {
    form.value = { id: null, name: '', source_type: 'git', repo_url: '', branch: 'main', auth_token: '', compose_path: 'docker-compose.yml', compose_content: '' };
  }
  showModal.value = true;
};

const handleFileUpload = (event) => {
  const file = event.target.files[0];
  if (!file) return;

  const reader = new FileReader();
  reader.onload = (e) => {
    form.value.compose_content = e.target.result;
    showToast('Success', 'File loaded successfully', 'success');
  };
  reader.onerror = () => {
    showToast('Error', 'Failed to read file', 'error');
  };
  reader.readAsText(file);
};

const submitProject = async () => {
  submitting.value = true;
  try {
    const token = secureStorage.getItem('token');
    let res;
    
    if (form.value.id) {
      res = await apiFetch(`/api/gitops/projects/${form.value.id}`, {
        method: 'PUT',
        headers: { 
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(form.value)
      });
    } else {
      res = await apiFetch('/api/gitops/projects', {
        method: 'POST',
        headers: { 
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(form.value)
      });
    }

    if (res.ok) {
      showToast('Success', form.value.id ? 'Deployment updated' : 'Deployment created', 'success');
      showModal.value = false;
      await loadProjects();
    } else {
      showToast('Error', 'Failed to save deployment', 'error');
    }
  } catch (e) {
    showToast('Error', 'Failed to save deployment', 'error');
  } finally {
    submitting.value = false;
  }
};

const triggerDelete = (id) => {
  pendingProjectId.value = id;
  showConfirm.value = true;
};

const triggerSync = async (id) => {
  isActionLoading.value = true;
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch(`/api/gitops/projects/${id}/sync`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      showToast('Success', 'Sync triggered', 'success');
      await loadProjects();
    } else {
      showToast('Error', 'Failed to trigger sync', 'error');
    }
  } catch (e) {
    showToast('Error', 'Failed to trigger sync', 'error');
  } finally {
    isActionLoading.value = false;
  }
};

const executeDelete = async () => {
  const id = pendingProjectId.value;
  showConfirm.value = false;
  isActionLoading.value = true;
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch(`/api/gitops/projects/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      showToast('Success', 'Deployment removed', 'success');
      await loadProjects();
    } else {
      showToast('Error', 'Failed to delete deployment', 'error');
    }
  } catch (e) {
    showToast('Error', 'Failed to delete deployment', 'error');
  } finally {
    isActionLoading.value = false;
    pendingProjectId.value = null;
  }
};

const viewLogs = async (id) => {
  showHistoryModal.value = true;
  historyLoading.value = true;
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch(`/api/gitops/projects/${id}/deployments`, {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      deployments.value = await res.json();
    }
  } catch (e) {
    console.error(e);
  } finally {
    historyLoading.value = false;
  }
};

onMounted(() => {
  loadProjects();
  // Poll for updates every 10s
  setInterval(loadProjects, 10000);
});
</script>

<style scoped>
.gitops-view { padding: 0; }
.view-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 2rem;
}
.projects-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 1.5rem;
}
.project-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 1.25rem;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
}
.card-header h3 { margin: 0; font-weight: 800; font-size: 1.1rem; }
.status-badge {
  padding: 0.2rem 0.6rem;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 800;
  text-transform: uppercase;
}
.status-badge.synced { background: rgba(var(--success-rgb), 0.2); color: var(--success); }
.status-badge.pending { background: rgba(var(--warning-rgb), 0.2); color: var(--warning); }
.status-badge.failed { background: rgba(var(--error-rgb), 0.2); color: var(--error); }
.status-badge.success { background: rgba(var(--success-rgb), 0.2); color: var(--success); }

.card-body {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin-bottom: 1.5rem;
}
.info-row {
  display: flex;
  flex-direction: column;
}
.info-row .label { font-size: 0.75rem; color: var(--text-mute); font-weight: 600; text-transform: uppercase; }
.info-row .value { font-size: 0.85rem; color: var(--text-main); }
.card-footer {
  display: flex;
  gap: 0.75rem;
  justify-content: flex-end;
}
.history-item {
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem;
  margin-bottom: 1rem;
}
.history-head {
  display: flex;
  gap: 1rem;
  align-items: center;
  margin-bottom: 0.75rem;
}
.history-head .time { color: var(--text-mute); font-size: 0.8rem; }
.history-logs {
  background: var(--bg-main);
  padding: 0.75rem;
  border-radius: 4px;
  font-size: 0.75rem;
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
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
  .history-head {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.5rem;
  }
}
</style>
