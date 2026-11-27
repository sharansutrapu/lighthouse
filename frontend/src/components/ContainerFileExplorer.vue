<template>
  <div class="file-explorer-container glass-panel">
    <div class="explorer-header">
      <div class="explorer-title">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
        </svg>
        <h3>In-Browser File Explorer</h3>
        <span class="badge badge-dim mini">{{ currentPath }}</span>
      </div>

      <div class="explorer-actions">
        <button class="btn btn-secondary btn-sm" @click="fetchFiles" :disabled="loading" title="Refresh files">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" :class="{ spinning: loading }">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
          Refresh
        </button>
        <label class="btn btn-primary btn-sm upload-label" title="Upload file into current directory">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="17 8 12 3 7 8"></polyline>
            <line x1="12" y1="3" x2="12" y2="15"></line>
          </svg>
          Upload
          <input type="file" @change="handleFileUpload" style="display: none;" />
        </label>
      </div>
    </div>

    <!-- Breadcrumb navigation -->
    <div class="breadcrumbs-bar">
      <button class="breadcrumb-chip" @click="navigateTo('/')">/ (root)</button>
      <template v-for="(seg, idx) in pathSegments" :key="idx">
        <span class="breadcrumb-separator">/</span>
        <button class="breadcrumb-chip" @click="navigateToSegment(idx)">{{ seg }}</button>
      </template>
    </div>

    <!-- File listing table -->
    <div class="explorer-body">
      <div v-if="loading" class="loading-state mini">
        <div class="spinner"></div>
      </div>

      <div v-else-if="files.length > 0" class="files-table-wrapper">
        <table class="files-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Size</th>
              <th>Mode</th>
              <th>Modified</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="currentPath !== '/'" class="parent-dir-row" @click="navigateUp">
              <td colspan="5">
                <span class="file-icon">📁</span>
                <strong>.. (Parent Directory)</strong>
              </td>
            </tr>
            <tr
              v-for="item in files"
              :key="item.name"
              class="file-row"
              @click="handleItemClick(item)"
            >
              <td class="file-name-cell">
                <span class="file-icon">{{ item.is_dir ? '📁' : '📄' }}</span>
                <span :class="['name-text', { 'is-directory': item.is_dir }]">
                  {{ item.name }}
                </span>
              </td>
              <td class="file-size-cell">{{ item.is_dir ? '—' : formatBytes(item.size) }}</td>
              <td class="file-mode-cell mono">{{ item.mode }}</td>
              <td class="file-time-cell">{{ formatDate(item.mod_time) }}</td>
              <td class="file-actions-cell text-right" @click.stop>
                <template v-if="!item.is_dir">
                  <button
                    class="action-btn-sm"
                    @click="openFileEditor(item)"
                    title="View & Edit File"
                  >
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                      <path d="M12 20h9"></path>
                      <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"></path>
                    </svg>
                  </button>
                  <button
                    class="action-btn-sm"
                    @click="downloadFile(item)"
                    title="Download File"
                  >
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                      <polyline points="7 10 12 15 17 10"></polyline>
                      <line x1="12" y1="15" x2="12" y2="3"></line>
                    </svg>
                  </button>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="empty-files">
        <p>Directory is empty.</p>
      </div>
    </div>

    <!-- File Editor Modal -->
    <div v-if="editorModal.open" class="modal-overlay" @click.self="editorModal.open = false">
      <div class="modal-content shadow-2xl file-editor-modal">
        <div class="modal-header">
          <div class="file-title-info">
            <span class="file-icon">📄</span>
            <h3>{{ editorModal.filePath }}</h3>
          </div>
          <button class="close-btn" @click="editorModal.open = false">✕</button>
        </div>
        <div class="modal-body file-editor-body">
          <div v-if="editorModal.loading" class="loading-state">
            <div class="spinner"></div>
          </div>
          <textarea
            v-else
            v-model="editorModal.content"
            class="file-textarea"
            spellcheck="false"
          ></textarea>
        </div>
        <div class="modal-actions">
          <button class="modal-btn cancel" @click="editorModal.open = false" :disabled="editorModal.saving">
            Cancel
          </button>
          <button class="modal-btn confirm primary" @click="saveFileContent" :disabled="editorModal.saving">
            {{ editorModal.saving ? 'Saving...' : 'Save File' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { apiFetch } from "../utils/apiFetch";

const props = defineProps({
  containerId: {
    type: String,
    required: true
  }
});

const currentPath = ref("/");
const files = ref([]);
const loading = ref(false);

const editorModal = ref({
  open: false,
  filePath: "",
  content: "",
  loading: false,
  saving: false
});

const pathSegments = computed(() => {
  return currentPath.value.split("/").filter(Boolean);
});

const formatBytes = (bytes) => {
  if (!bytes || bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
};

const formatDate = (dateStr) => {
  if (!dateStr) return "—";
  return new Date(dateStr).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
};

const fetchFiles = async () => {
  loading.value = true;
  try {
    const res = await apiFetch(`/api/containers/${props.containerId}/files?path=${encodeURIComponent(currentPath.value)}`);
    if (res.ok) {
      files.value = await res.json();
    } else {
      files.value = [];
    }
  } catch (err) {
    console.error("Failed to list files:", err);
    files.value = [];
  } finally {
    loading.value = false;
  }
};

const navigateTo = (path) => {
  currentPath.value = path;
  fetchFiles();
};

const navigateToSegment = (idx) => {
  const parts = pathSegments.value.slice(0, idx + 1);
  currentPath.value = "/" + parts.join("/");
  fetchFiles();
};

const navigateUp = () => {
  const parts = pathSegments.value;
  if (parts.length <= 1) {
    currentPath.value = "/";
  } else {
    currentPath.value = "/" + parts.slice(0, -1).join("/");
  }
  fetchFiles();
};

const handleItemClick = (item) => {
  if (item.is_dir) {
    currentPath.value = item.path;
    fetchFiles();
  } else {
    openFileEditor(item);
  }
};

const openFileEditor = async (item) => {
  editorModal.value = {
    open: true,
    filePath: item.path,
    content: "",
    loading: true,
    saving: false
  };

  try {
    const res = await apiFetch(`/api/containers/${props.containerId}/files/content?path=${encodeURIComponent(item.path)}`);
    if (res.ok) {
      const data = await res.json();
      editorModal.value.content = data.content || "";
    } else {
      editorModal.value.content = "# Failed loading file or binary file cannot be edited in-browser.";
    }
  } catch (err) {
    editorModal.value.content = "Error loading: " + err.message;
  } finally {
    editorModal.value.loading = false;
  }
};

const saveFileContent = async () => {
  editorModal.value.saving = true;
  try {
    const res = await apiFetch(`/api/containers/${props.containerId}/files/content`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        path: editorModal.value.filePath,
        content: editorModal.value.content
      })
    });
    if (res.ok) {
      editorModal.value.open = false;
      await fetchFiles();
    } else {
      const err = await res.json();
      alert("Failed saving file: " + (err.error || res.statusText));
    }
  } catch (err) {
    alert("Request error: " + err.message);
  } finally {
    editorModal.value.saving = false;
  }
};

const downloadFile = (item) => {
  const url = `/api/containers/${props.containerId}/files/download?path=${encodeURIComponent(item.path)}`;
  window.open(url, "_blank");
};

const handleFileUpload = async (e) => {
  const file = e.target.files?.[0];
  if (!file) return;

  const formData = new FormData();
  formData.append("file", file);
  formData.append("destination", currentPath.value);

  try {
    const res = await apiFetch(`/api/containers/${props.containerId}/files/upload`, {
      method: "POST",
      body: formData
    });
    if (res.ok) {
      await fetchFiles();
    } else {
      const err = await res.json();
      alert("Upload failed: " + (err.error || res.statusText));
    }
  } catch (err) {
    alert("Upload failed: " + err.message);
  } finally {
    e.target.value = "";
  }
};

onMounted(() => {
  fetchFiles();
});
</script>

<style scoped>
.file-explorer-container {
  padding: 1.25rem;
  border-radius: 12px;
  background: var(--surface-bg, rgba(20, 24, 33, 0.7));
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  margin-top: 1.5rem;
}

.explorer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.75rem;
}

.explorer-title {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}

.explorer-title h3 {
  margin: 0;
  font-size: 1.05rem;
}

.explorer-actions {
  display: flex;
  gap: 0.5rem;
}

.upload-label {
  cursor: pointer;
}

.breadcrumbs-bar {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  background: rgba(0, 0, 0, 0.25);
  padding: 0.4rem 0.75rem;
  border-radius: 6px;
  margin-bottom: 1rem;
  overflow-x: auto;
}

.breadcrumb-chip {
  background: transparent;
  border: none;
  color: #38bdf8;
  font-size: 0.8rem;
  font-weight: 500;
  cursor: pointer;
  padding: 0.15rem 0.3rem;
  border-radius: 4px;
}

.breadcrumb-chip:hover {
  background: rgba(56, 189, 248, 0.15);
}

.breadcrumb-separator {
  color: #64748b;
  font-size: 0.8rem;
}

.files-table-wrapper {
  overflow-x: auto;
  max-height: 450px;
}

.files-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.85rem;
  text-align: left;
}

.files-table th {
  padding: 0.6rem 0.75rem;
  color: #94a3b8;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  font-weight: 500;
}

.files-table td {
  padding: 0.6rem 0.75rem;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}

.file-row, .parent-dir-row {
  cursor: pointer;
  transition: background 0.15s ease;
}

.file-row:hover, .parent-dir-row:hover {
  background: rgba(255, 255, 255, 0.04);
}

.file-name-cell {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}

.file-icon {
  font-size: 1rem;
}

.name-text.is-directory {
  color: #38bdf8;
  font-weight: 500;
}

.action-btn-sm {
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #e2e8f0;
  border-radius: 4px;
  padding: 0.25rem 0.4rem;
  cursor: pointer;
  margin-left: 0.3rem;
}

.action-btn-sm:hover {
  background: rgba(59, 130, 246, 0.2);
  border-color: #3b82f6;
}

.file-editor-modal {
  width: 750px;
  max-width: 95vw;
}

.file-title-info {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}

.file-title-info h3 {
  margin: 0;
  font-size: 1rem;
  font-family: ui-monospace, monospace;
}

.file-editor-body {
  height: 450px;
  padding: 0;
}

.file-textarea {
  width: 100%;
  height: 100%;
  background: #0d1117;
  color: #e6edf3;
  border: none;
  padding: 1rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  line-height: 1.5;
  resize: none;
  outline: none;
  tab-size: 2;
}

.empty-files {
  padding: 2rem;
  text-align: center;
  color: #94a3b8;
}
</style>
