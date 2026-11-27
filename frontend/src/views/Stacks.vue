<template>
  <div class="page-container animate-fade-in">
    <div class="page-header glass">
      <div class="header-left">
        <h1>
          Stack Studio
          <span class="badge badge-dim" style="font-size: 0.8rem; margin-left: 0.5rem; vertical-align: middle;">
            {{ stacks.length }} Stacks
          </span>
        </h1>
        <p class="subtitle">
          Interactive Docker Compose Studio: author, validate, deploy, and manage multi-container application stacks.
        </p>
      </div>
      <div class="header-actions" style="display: flex; gap: 0.75rem; align-items: center;">
        <div class="search-box glass" style="margin: 0; min-width: 220px;">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
            <circle cx="11" cy="11" r="8"></circle>
            <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
          </svg>
          <input type="text" v-model="searchQuery" placeholder="Search stacks..." />
        </div>
        <button class="btn btn-secondary" @click="fetchStacks" :disabled="loading">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" :class="{ spinning: loading }">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
          Refresh
        </button>
        <button class="btn btn-primary" @click="openCreateModal">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          New Stack
        </button>
      </div>
    </div>

    <!-- Active Stack Editor or List -->
    <div class="content-wrapper">
      <!-- Stacks Overview Cards / Table -->
      <div v-if="!selectedStack" class="stacks-list-container">
        <div v-if="loading && stacks.length === 0" class="loading-state">
          <div class="spinner"></div>
        </div>

        <div v-else-if="filteredStacks.length > 0" class="stacks-grid">
          <div
            v-for="stk in filteredStacks"
            :key="stk.id"
            class="stack-card glass-panel"
            @click="selectStack(stk)"
          >
            <div class="stack-card-header">
              <div class="stack-card-title">
                <div class="stack-icon">
                  <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.5">
                    <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
                    <polyline points="2 17 12 22 22 17"></polyline>
                    <polyline points="2 12 12 17 22 12"></polyline>
                  </svg>
                </div>
                <div>
                  <h3 class="stack-name">{{ stk.name }}</h3>
                  <span class="stack-updated">Updated {{ formatDate(stk.updated_at) }}</span>
                </div>
              </div>
              <div :class="['status-pill', stk.status]">
                <span class="pulse-dot"></span>
                {{ stk.status || 'draft' }}
              </div>
            </div>

            <div class="stack-card-preview">
              <pre class="yaml-preview"><code>{{ stk.compose_content.slice(0, 180) }}...</code></pre>
            </div>

            <div class="stack-card-footer" @click.stop>
              <div class="footer-left">
                <span class="env-count-badge" v-if="stk.env_content">
                  <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                  </svg>
                  .env configured
                </span>
              </div>
              <div class="footer-actions">
                <button
                  class="action-btn success"
                  @click.stop="deployStack(stk)"
                  title="Deploy / Up"
                  :disabled="actionInProgress === stk.id"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                    <polygon points="5 3 19 12 5 21 5 3"></polygon>
                  </svg>
                </button>
                <button
                  class="action-btn warning"
                  @click.stop="stopStack(stk)"
                  title="Stop"
                  :disabled="actionInProgress === stk.id"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                    <rect x="6" y="6" width="12" height="12"></rect>
                  </svg>
                </button>
                <button
                  class="action-btn primary"
                  @click.stop="selectStack(stk)"
                  title="Open Editor"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M12 20h9"></path>
                    <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"></path>
                  </svg>
                </button>
                <button
                  class="action-btn danger"
                  @click.stop="confirmDeleteStack(stk)"
                  title="Delete Stack"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div v-else class="empty-state-card glass-panel text-center">
          <div class="empty-icon">
            <svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" stroke-width="1.5">
              <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
              <polyline points="2 17 12 22 22 17"></polyline>
              <polyline points="2 12 12 17 22 12"></polyline>
            </svg>
          </div>
          <h3>No Stacks Created Yet</h3>
          <p>Compose multi-container applications visually or paste an existing docker-compose.yml file.</p>
          <button class="btn btn-primary" style="margin-top: 1rem;" @click="openCreateModal">
            Create Your First Stack
          </button>
        </div>
      </div>

      <!-- In-Browser Stack Studio Editor Pane -->
      <div v-else class="stack-studio-editor animate-fade-in">
        <div class="editor-top-bar glass">
          <div class="bar-left">
            <button class="btn-icon" @click="selectedStack = null" title="Back to Stacks">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.5">
                <line x1="19" y1="12" x2="5" y2="12"></line>
                <polyline points="12 19 5 12 12 5"></polyline>
              </svg>
            </button>
            <div class="stack-header-info">
              <h2>{{ selectedStack.name }}</h2>
              <div :class="['status-pill mini', selectedStack.status]">
                <span class="pulse-dot"></span>
                {{ selectedStack.status || 'draft' }}
              </div>
            </div>
          </div>

          <div class="bar-tabs">
            <button
              :class="['editor-tab', { active: activeEditorTab === 'compose' }]"
              @click="activeEditorTab = 'compose'"
            >
              docker-compose.yml
            </button>
            <button
              :class="['editor-tab', { active: activeEditorTab === 'env' }]"
              @click="activeEditorTab = 'env'"
            >
              .env (Environment)
            </button>
          </div>

          <div class="bar-actions">
            <button
              class="btn btn-secondary"
              @click="saveStack"
              :disabled="saving"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"></path>
                <polyline points="17 21 17 13 7 13 7 21"></polyline>
                <polyline points="7 3 7 8 15 8"></polyline>
              </svg>
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
            <button
              class="btn btn-success"
              @click="deployStack(selectedStack)"
              :disabled="actionInProgress === selectedStack.id"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <polygon points="5 3 19 12 5 21 5 3"></polygon>
              </svg>
              Deploy Stack
            </button>
            <button
              class="btn btn-warning"
              @click="stopStack(selectedStack)"
              :disabled="actionInProgress === selectedStack.id"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <rect x="6" y="6" width="12" height="12"></rect>
              </svg>
              Stop
            </button>
            <button
              class="btn btn-secondary"
              @click="restartStack(selectedStack)"
              :disabled="actionInProgress === selectedStack.id"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <polyline points="23 4 23 10 17 10"></polyline>
                <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
              </svg>
              Restart
            </button>
            <button
              class="btn btn-danger"
              @click="downStack(selectedStack)"
              :disabled="actionInProgress === selectedStack.id"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
              Down
            </button>
          </div>
        </div>

        <div class="editor-body">
          <div v-show="activeEditorTab === 'compose'" class="code-editor-wrapper">
            <textarea
              v-model="selectedStack.compose_content"
              class="code-textarea"
              placeholder="# Enter docker-compose.yml here...&#10;version: '3.8'&#10;services:&#10;  app:&#10;    image: nginx:alpine&#10;    ports:&#10;      - '8080:80'"
              spellcheck="false"
            ></textarea>
          </div>
          <div v-show="activeEditorTab === 'env'" class="code-editor-wrapper">
            <textarea
              v-model="selectedStack.env_content"
              class="code-textarea env-theme"
              placeholder="# KEY=VALUE environment pairs&#10;PORT=3000&#10;NODE_ENV=production"
              spellcheck="false"
            ></textarea>
          </div>
        </div>
      </div>
    </div>

    <!-- Create Stack Modal -->
    <div v-if="createModalOpen" class="modal-overlay" @click.self="createModalOpen = false">
      <div class="modal-content shadow-2xl stack-modal">
        <div class="modal-header">
          <h3>Create New Stack</h3>
          <button class="close-btn" @click="createModalOpen = false">✕</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label>Stack Name *</label>
            <input
              type="text"
              v-model="newStackName"
              placeholder="e.g. web-app, monitoring, redis-cluster"
              class="form-input"
            />
          </div>
          <div class="form-group">
            <label>Template or Initial Compose File</label>
            <textarea
              v-model="newStackCompose"
              rows="10"
              class="form-input code-font"
              placeholder="services:&#10;  web:&#10;    image: nginx:alpine&#10;    ports:&#10;      - '80:80'"
            ></textarea>
          </div>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="createModalOpen = false">Cancel</button>
          <button class="modal-btn confirm primary" @click="submitCreateStack" :disabled="!newStackName.trim()">
            Create Stack
          </button>
        </div>
      </div>
    </div>

    <!-- Live Execution / Deploy Console Modal -->
    <div v-if="executionModal.open" class="modal-overlay" @click.self="executionModal.open = false">
      <div class="modal-content shadow-2xl console-modal">
        <div class="modal-header">
          <div class="console-title">
            <span class="status-indicator" :class="executionModal.status"></span>
            <h3>{{ executionModal.title }}</h3>
          </div>
          <button class="close-btn" @click="executionModal.open = false">✕</button>
        </div>
        <div class="modal-body console-body">
          <pre class="terminal-output"><code>{{ executionModal.output }}</code></pre>
        </div>
        <div class="modal-actions">
          <button class="modal-btn confirm primary" @click="executionModal.open = false">Close</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { apiFetch } from "../utils/apiFetch";

const stacks = ref([]);
const loading = ref(false);
const saving = ref(false);
const searchQuery = ref("");
const selectedStack = ref(null);
const activeEditorTab = ref("compose");
const actionInProgress = ref(null);

const createModalOpen = ref(false);
const newStackName = ref("");
const newStackCompose = ref(`version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    restart: unless-stopped
`);

const executionModal = ref({
  open: false,
  title: "",
  output: "",
  status: "idle"
});

const filteredStacks = computed(() => {
  if (!searchQuery.value) return stacks.value;
  const q = searchQuery.value.toLowerCase();
  return stacks.value.filter((s) => s.name.toLowerCase().includes(q));
});

const formatDate = (d) => {
  if (!d) return "Just now";
  return new Date(d).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
};

const fetchStacks = async () => {
  loading.value = true;
  try {
    const res = await apiFetch("/api/stacks");
    if (res.ok) {
      stacks.value = await res.json();
    }
  } catch (err) {
    console.error("Failed to load stacks:", err);
  } finally {
    loading.value = false;
  }
};

const openCreateModal = () => {
  newStackName.value = "";
  createModalOpen.value = true;
};

const submitCreateStack = async () => {
  if (!newStackName.value.trim()) return;
  try {
    const res = await apiFetch("/api/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: newStackName.value.trim(),
        compose_content: newStackCompose.value,
        env_content: ""
      })
    });
    if (res.ok) {
      const created = await res.json();
      createModalOpen.value = false;
      await fetchStacks();
      selectedStack.value = created;
    } else {
      const err = await res.json();
      alert("Error creating stack: " + (err.error || res.statusText));
    }
  } catch (err) {
    alert("Request failed: " + err.message);
  }
};

const selectStack = (stk) => {
  selectedStack.value = { ...stk };
  activeEditorTab.value = "compose";
};

const saveStack = async () => {
  if (!selectedStack.value) return;
  saving.value = true;
  try {
    const res = await apiFetch(`/api/stacks/${selectedStack.value.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        compose_content: selectedStack.value.compose_content,
        env_content: selectedStack.value.env_content
      })
    });
    if (res.ok) {
      const updated = await res.json();
      selectedStack.value = updated;
      await fetchStacks();
    }
  } catch (err) {
    alert("Failed saving stack: " + err.message);
  } finally {
    saving.value = false;
  }
};

const runAction = async (stk, action) => {
  actionInProgress.value = stk.id;
  executionModal.value = {
    open: true,
    title: `Running docker compose ${action} on ${stk.name}...`,
    output: "Executing compose command on host...\n",
    status: "running"
  };

  try {
    const res = await apiFetch(`/api/stacks/${stk.id}/${action}`, { method: "POST" });
    const data = await res.json();
    if (res.ok) {
      executionModal.value.status = "success";
      executionModal.value.title = `Stack ${action.toUpperCase()} Succeeded`;
      executionModal.value.output = data.output || `docker compose ${action} completed successfully.`;
      if (selectedStack.value && selectedStack.value.id === stk.id) {
        selectedStack.value.status = data.status || stk.status;
      }
      await fetchStacks();
    } else {
      executionModal.value.status = "error";
      executionModal.value.title = `Stack ${action.toUpperCase()} Failed`;
      executionModal.value.output = data.output || data.error || "Execution failed";
    }
  } catch (err) {
    executionModal.value.status = "error";
    executionModal.value.title = `Action Failed`;
    executionModal.value.output = err.message;
  } finally {
    actionInProgress.value = null;
  }
};

const deployStack = (stk) => runAction(stk, "deploy");
const stopStack = (stk) => runAction(stk, "stop");
const restartStack = (stk) => runAction(stk, "restart");
const downStack = (stk) => runAction(stk, "down");

const confirmDeleteStack = async (stk) => {
  if (!confirm(`Are you sure you want to delete stack "${stk.name}"? This removes the stack definition.`)) return;
  try {
    const res = await apiFetch(`/api/stacks/${stk.id}`, { method: "DELETE" });
    if (res.ok) {
      if (selectedStack.value?.id === stk.id) selectedStack.value = null;
      await fetchStacks();
    }
  } catch (err) {
    alert("Delete failed: " + err.message);
  }
};

onMounted(() => {
  fetchStacks();
});
</script>

<style scoped>
.stacks-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 1.25rem;
}

.stack-card {
  padding: 1.25rem;
  border-radius: 12px;
  background: var(--surface-bg, rgba(255, 255, 255, 0.04));
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  cursor: pointer;
  transition: transform 0.15s ease, border-color 0.15s ease;
}

.stack-card:hover {
  transform: translateY(-2px);
  border-color: var(--primary, #3b82f6);
}

.stack-card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 0.75rem;
}

.stack-card-title {
  display: flex;
  gap: 0.75rem;
  align-items: center;
}

.stack-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
  display: flex;
  align-items: center;
  justify-content: center;
}

.stack-name {
  font-size: 1.05rem;
  font-weight: 600;
  margin: 0;
}

.stack-updated {
  font-size: 0.75rem;
  color: var(--text-muted, #94a3b8);
}

.stack-card-preview {
  margin: 0.75rem 0;
  background: rgba(0, 0, 0, 0.25);
  border-radius: 6px;
  padding: 0.5rem 0.75rem;
  overflow: hidden;
}

.yaml-preview {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  color: #cbd5e1;
  white-space: pre-wrap;
  word-break: break-all;
}

.stack-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 0.5rem;
  padding-top: 0.75rem;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.env-count-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.75rem;
  color: #10b981;
}

.footer-actions {
  display: flex;
  gap: 0.4rem;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.2rem 0.6rem;
  border-radius: 9999px;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  background: rgba(148, 163, 184, 0.15);
  color: #94a3b8;
}

.status-pill.deployed, .status-pill.running {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
}

.status-pill.stopped {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}

.pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

/* Studio Editor View */
.stack-studio-editor {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 180px);
  background: var(--surface-bg, rgba(20, 24, 33, 0.85));
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  border-radius: 12px;
  overflow: hidden;
}

.editor-top-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1.25rem;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.bar-left {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.btn-icon {
  background: transparent;
  border: none;
  color: var(--text-color, #e2e8f0);
  cursor: pointer;
  padding: 0.35rem;
  border-radius: 6px;
  display: flex;
  align-items: center;
}

.btn-icon:hover {
  background: rgba(255, 255, 255, 0.1);
}

.stack-header-info {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.stack-header-info h2 {
  font-size: 1.15rem;
  margin: 0;
}

.bar-tabs {
  display: flex;
  gap: 0.5rem;
  background: rgba(0, 0, 0, 0.3);
  padding: 0.25rem;
  border-radius: 8px;
}

.editor-tab {
  background: transparent;
  border: none;
  color: #94a3b8;
  padding: 0.4rem 0.9rem;
  font-size: 0.85rem;
  font-weight: 500;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.editor-tab.active {
  background: var(--primary, #3b82f6);
  color: white;
}

.bar-actions {
  display: flex;
  gap: 0.5rem;
}

.editor-body {
  flex: 1;
  position: relative;
  display: flex;
}

.code-editor-wrapper {
  width: 100%;
  height: 100%;
}

.code-textarea {
  width: 100%;
  height: 100%;
  background: #0d1117;
  color: #e6edf3;
  border: none;
  padding: 1.25rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.9rem;
  line-height: 1.6;
  resize: none;
  outline: none;
  tab-size: 2;
}

.code-textarea.env-theme {
  color: #58a6ff;
}

/* Modals */
.stack-modal {
  width: 550px;
  max-width: 90vw;
}

.console-modal {
  width: 750px;
  max-width: 95vw;
}

.console-body {
  background: #0d1117;
  padding: 1rem;
  border-radius: 8px;
  max-height: 400px;
  overflow-y: auto;
}

.terminal-output {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  color: #4ade80;
  white-space: pre-wrap;
}

.form-group {
  margin-bottom: 1rem;
}

.form-group label {
  display: block;
  font-size: 0.85rem;
  font-weight: 500;
  margin-bottom: 0.4rem;
  color: #cbd5e1;
}

.form-input {
  width: 100%;
  padding: 0.6rem 0.8rem;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(255, 255, 255, 0.15);
  border-radius: 6px;
  color: white;
  font-size: 0.9rem;
}

.form-input.code-font {
  font-family: ui-monospace, monospace;
  font-size: 0.8rem;
}
</style>
