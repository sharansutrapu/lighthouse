<template>
  <div class="page-container animate-fade-in">
    <div class="page-header glass">
      <div class="header-left">
        <h1>
          Private Registries
          <span class="badge badge-dim" style="font-size: 0.8rem; margin-left: 0.5rem; vertical-align: middle;">
            {{ registries.length }} Configured
          </span>
        </h1>
        <p class="subtitle">
          Secure credential vault for Docker Hub, GitHub Packages (ghcr.io), AWS ECR, and Harbor with authenticated image pulls.
        </p>
      </div>
      <div class="header-actions" style="display: flex; gap: 0.75rem; align-items: center;">
        <button class="btn btn-secondary" @click="fetchRegistries" :disabled="loading">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" :class="{ spinning: loading }">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
          Refresh
        </button>
        <button class="btn btn-secondary" @click="openPullModal">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
          Pull Image
        </button>
        <button class="btn btn-primary" @click="openAddModal">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          Add Registry
        </button>
      </div>
    </div>

    <div class="content-wrapper">
      <div v-if="loading && registries.length === 0" class="loading-state">
        <div class="spinner"></div>
      </div>

      <div v-else-if="registries.length > 0" class="registries-grid">
        <div
          v-for="reg in registries"
          :key="reg.id"
          class="registry-card glass-panel"
        >
          <div class="registry-card-header">
            <div class="reg-icon-wrapper">
              <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.5">
                <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
                <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
                <line x1="6" y1="6" x2="6.01" y2="6"></line>
                <line x1="6" y1="18" x2="6.01" y2="18"></line>
              </svg>
            </div>
            <div class="reg-title-info">
              <h3>{{ reg.name }}</h3>
              <span class="reg-url">{{ reg.url }}</span>
            </div>
            <span v-if="reg.is_default" class="badge badge-success mini">Default</span>
          </div>

          <div class="registry-card-details">
            <div class="detail-row">
              <span class="detail-lbl">Username:</span>
              <span class="detail-val">{{ reg.username || '(None / Public)' }}</span>
            </div>
            <div class="detail-row">
              <span class="detail-lbl">Auth Status:</span>
              <span class="detail-val auth-status" :class="{ secure: reg.has_password }">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                  <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
                </svg>
                {{ reg.has_password ? 'Credentials Encrypted' : 'No Token Saved' }}
              </span>
            </div>
          </div>

          <div class="registry-card-footer">
            <button class="btn btn-sm btn-secondary" @click="pullFromRegistry(reg)">
              Pull Image
            </button>
            <button class="action-btn danger" @click="deleteRegistry(reg)" title="Delete Registry">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </button>
          </div>
        </div>
      </div>

      <div v-else class="empty-state-card glass-panel text-center">
        <div class="empty-icon">
          <svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" stroke-width="1.5">
            <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
            <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
            <line x1="6" y1="6" x2="6.01" y2="6"></line>
            <line x1="6" y1="18" x2="6.01" y2="18"></line>
          </svg>
        </div>
        <h3>No Registries Configured</h3>
        <p>Add credentials for private Docker registries to pull private images seamlessly from LightHouse.</p>
        <button class="btn btn-primary" style="margin-top: 1rem;" @click="openAddModal">
          Add Private Registry
        </button>
      </div>
    </div>

    <!-- Add Registry Modal -->
    <div v-if="addModalOpen" class="modal-overlay" @click.self="addModalOpen = false">
      <div class="modal-content shadow-2xl form-modal">
        <div class="modal-header">
          <h3>Add Private Registry</h3>
          <button class="close-btn" @click="addModalOpen = false">✕</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label>Provider Preset</label>
            <select class="form-input" @change="applyPreset($event.target.value)">
              <option value="custom">Custom Registry</option>
              <option value="dockerhub">Docker Hub (registry-1.docker.io)</option>
              <option value="ghcr">GitHub Container Registry (ghcr.io)</option>
              <option value="ecr">AWS Elastic Container Registry (ECR)</option>
              <option value="harbor">Harbor Registry</option>
            </select>
          </div>
          <div class="form-group">
            <label>Registry Name *</label>
            <input type="text" v-model="form.name" placeholder="e.g. My GitHub Packages" class="form-input" />
          </div>
          <div class="form-group">
            <label>Registry URL / Host *</label>
            <input type="text" v-model="form.url" placeholder="e.g. ghcr.io or my-harbor.internal" class="form-input" />
          </div>
          <div class="form-group">
            <label>Username / Access Key</label>
            <input type="text" v-model="form.username" placeholder="e.g. octocat" class="form-input" />
          </div>
          <div class="form-group">
            <label>Password / Personal Access Token *</label>
            <input type="password" v-model="form.password" placeholder="••••••••••••" class="form-input" />
          </div>
          <div class="form-group">
            <label class="checkbox-label" style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
              <input type="checkbox" v-model="form.is_default" />
              Use as Default Registry
            </label>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="addModalOpen = false">Cancel</button>
          <button class="modal-btn confirm primary" @click="submitAddRegistry" :disabled="!form.name || !form.url">
            Save Registry
          </button>
        </div>
      </div>
    </div>

    <!-- Authenticated Pull Modal -->
    <div v-if="pullModalOpen" class="modal-overlay" @click.self="pullModalOpen = false">
      <div class="modal-content shadow-2xl form-modal">
        <div class="modal-header">
          <h3>Authenticated Image Pull</h3>
          <button class="close-btn" @click="pullModalOpen = false">✕</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label>Target Registry Vault</label>
            <select class="form-input" v-model="pullForm.registry_id">
              <option value="">Default Registry</option>
              <option v-for="r in registries" :key="r.id" :value="r.id">
                {{ r.name }} ({{ r.url }})
              </option>
            </select>
          </div>
          <div class="form-group">
            <label>Image Tag *</label>
            <input
              type="text"
              v-model="pullForm.image"
              placeholder="e.g. ghcr.io/org/repo:v1.2.0 or redis:alpine"
              class="form-input"
            />
          </div>
          <div v-if="pullOutput" class="pull-output-box">
            <pre><code>{{ pullOutput }}</code></pre>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="pullModalOpen = false" :disabled="pulling">Cancel</button>
          <button class="modal-btn confirm primary" @click="executePull" :disabled="pulling || !pullForm.image.trim()">
            {{ pulling ? 'Pulling from Registry...' : 'Pull Image' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from "vue";
import { apiFetch } from "../utils/apiFetch";

const registries = ref([]);
const loading = ref(false);
const addModalOpen = ref(false);
const pullModalOpen = ref(false);
const pulling = ref(false);
const pullOutput = ref("");

const form = ref({
  name: "",
  url: "",
  username: "",
  password: "",
  is_default: false
});

const pullForm = ref({
  image: "",
  registry_id: ""
});

const fetchRegistries = async () => {
  loading.value = true;
  try {
    const res = await apiFetch("/api/registries");
    if (res.ok) {
      registries.value = await res.json();
    }
  } catch (err) {
    console.error("Failed to load registries:", err);
  } finally {
    loading.value = false;
  }
};

const openAddModal = () => {
  form.value = {
    name: "",
    url: "",
    username: "",
    password: "",
    is_default: false
  };
  addModalOpen.value = true;
};

const applyPreset = (preset) => {
  if (preset === "dockerhub") {
    form.value.name = "Docker Hub";
    form.value.url = "https://index.docker.io/v1/";
  } else if (preset === "ghcr") {
    form.value.name = "GitHub Container Registry";
    form.value.url = "ghcr.io";
  } else if (preset === "ecr") {
    form.value.name = "AWS ECR";
    form.value.url = ".dkr.ecr.us-east-1.amazonaws.com";
  } else if (preset === "harbor") {
    form.value.name = "Harbor";
    form.value.url = "https://harbor.internal";
  }
};

const submitAddRegistry = async () => {
  try {
    const res = await apiFetch("/api/registries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(form.value)
    });
    if (res.ok) {
      addModalOpen.value = false;
      await fetchRegistries();
    } else {
      const err = await res.json();
      alert("Error: " + (err.error || res.statusText));
    }
  } catch (err) {
    alert("Request failed: " + err.message);
  }
};

const deleteRegistry = async (reg) => {
  if (!confirm(`Remove registry "${reg.name}"? Saved credentials will be deleted.`)) return;
  try {
    const res = await apiFetch(`/api/registries/${reg.id}`, { method: "DELETE" });
    if (res.ok) {
      await fetchRegistries();
    }
  } catch (err) {
    alert("Delete failed: " + err.message);
  }
};

const openPullModal = () => {
  pullForm.value = { image: "", registry_id: "" };
  pullOutput.value = "";
  pullModalOpen.value = true;
};

const pullFromRegistry = (reg) => {
  pullForm.value = { image: "", registry_id: reg.id };
  pullOutput.value = "";
  pullModalOpen.value = true;
};

const executePull = async () => {
  if (!pullForm.value.image.trim()) return;
  pulling.value = true;
  pullOutput.value = "Contacting registry and initiating image pull...\n";
  try {
    const res = await apiFetch("/api/images/pull", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        image: pullForm.value.image.trim(),
        registry_id: pullForm.value.registry_id ? Number(pullForm.value.registry_id) : 0
      })
    });
    const data = await res.json();
    if (res.ok) {
      pullOutput.value = (data.output || "Image pulled successfully.") + "\nStatus: Complete";
    } else {
      pullOutput.value = "Pull failed: " + (data.output || data.error || res.statusText);
    }
  } catch (err) {
    pullOutput.value = "Error: " + err.message;
  } finally {
    pulling.value = false;
  }
};

onMounted(() => {
  fetchRegistries();
});
</script>

<style scoped>
.registries-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 1.25rem;
}

.registry-card {
  padding: 1.25rem;
  border-radius: 12px;
  background: var(--surface-bg, rgba(255, 255, 255, 0.04));
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.registry-card-header {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  margin-bottom: 1rem;
}

.reg-icon-wrapper {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
  display: flex;
  align-items: center;
  justify-content: center;
}

.reg-title-info {
  flex: 1;
}

.reg-title-info h3 {
  font-size: 1.05rem;
  margin: 0;
}

.reg-url {
  font-size: 0.75rem;
  color: #94a3b8;
  word-break: break-all;
}

.registry-card-details {
  background: rgba(0, 0, 0, 0.2);
  padding: 0.75rem 1rem;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  margin-bottom: 1rem;
}

.detail-row {
  display: flex;
  justify-content: space-between;
  font-size: 0.8rem;
}

.detail-lbl {
  color: #94a3b8;
}

.detail-val {
  font-weight: 500;
}

.auth-status {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  color: #f59e0b;
}

.auth-status.secure {
  color: #10b981;
}

.registry-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 0.75rem;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.form-modal {
  width: 500px;
  max-width: 90vw;
}

.pull-output-box {
  margin-top: 1rem;
  background: #0d1117;
  padding: 0.75rem;
  border-radius: 6px;
  max-height: 200px;
  overflow-y: auto;
}

.pull-output-box pre {
  margin: 0;
  font-family: ui-monospace, monospace;
  font-size: 0.8rem;
  color: #38bdf8;
  white-space: pre-wrap;
}
</style>
