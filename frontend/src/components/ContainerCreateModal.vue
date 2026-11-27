<template>
  <div class="modal-overlay" @click.self="$emit('close')">
    <div class="modal-content shadow-2xl wizard-modal animate-fade-in">
      <div class="modal-header">
        <div class="wizard-header-title">
          <div class="wizard-icon">
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.5">
              <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
              <polyline points="2 17 12 22 22 17"></polyline>
              <polyline points="2 12 12 17 22 12"></polyline>
            </svg>
          </div>
          <div>
            <h3>Create Container</h3>
            <span class="wizard-sub">Deploy a workload directly without writing Compose files</span>
          </div>
        </div>
        <button class="close-btn" @click="$emit('close')">✕</button>
      </div>

      <!-- Navigation Tabs -->
      <div class="wizard-tabs">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          :class="['wizard-tab', { active: activeTab === tab.id }]"
          @click="activeTab = tab.id"
        >
          {{ tab.label }}
        </button>
      </div>

      <div class="modal-body wizard-body">
        <!-- 1. Basics Tab -->
        <div v-show="activeTab === 'basics'" class="tab-pane">
          <div class="form-group">
            <label>Container Name *</label>
            <input
              type="text"
              v-model="form.name"
              placeholder="e.g. my-app-service"
              class="form-input"
            />
          </div>

          <div class="form-group">
            <label>Image *</label>
            <input
              type="text"
              v-model="form.image"
              placeholder="e.g. nginx:alpine, redis:7-alpine, postgres:16"
              class="form-input"
            />
            <div class="quick-chips">
              <span class="chip-label">Quick select:</span>
              <button
                type="button"
                v-for="img in quickImages"
                :key="img"
                class="quick-chip"
                @click="form.image = img"
              >
                {{ img }}
              </button>
            </div>
          </div>

          <div class="form-group">
            <label>Entrypoint Command (Optional)</label>
            <input
              type="text"
              v-model="form.command"
              placeholder="e.g. npm start or python app.py"
              class="form-input"
            />
          </div>
        </div>

        <!-- 2. Ports Tab -->
        <div v-show="activeTab === 'ports'" class="tab-pane">
          <p class="tab-help">Map host ports to internal container ports.</p>
          <div
            v-for="(port, idx) in form.ports"
            :key="idx"
            class="dynamic-row"
          >
            <input
              type="text"
              v-model="port.host"
              placeholder="Host (e.g. 8080)"
              class="form-input compact"
            />
            <span class="row-arrow">➔</span>
            <input
              type="text"
              v-model="port.container"
              placeholder="Container (e.g. 80)"
              class="form-input compact"
            />
            <select v-model="port.proto" class="form-input proto-select">
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </select>
            <button
              type="button"
              class="action-btn danger mini"
              @click="removePort(idx)"
              title="Remove port"
            >
              ✕
            </button>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="addPort">
            + Add Port Mapping
          </button>
        </div>

        <!-- 3. Environment Tab -->
        <div v-show="activeTab === 'env'" class="tab-pane">
          <p class="tab-help">Provide environment variable key-value pairs for the container process.</p>
          <div
            v-for="(envItem, idx) in form.env"
            :key="idx"
            class="dynamic-row"
          >
            <input
              type="text"
              v-model="envItem.key"
              placeholder="KEY (e.g. NODE_ENV)"
              class="form-input compact"
            />
            <span class="row-equal">=</span>
            <input
              type="text"
              v-model="envItem.val"
              placeholder="VALUE (e.g. production)"
              class="form-input compact"
            />
            <button
              type="button"
              class="action-btn danger mini"
              @click="removeEnv(idx)"
              title="Remove variable"
            >
              ✕
            </button>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="addEnv">
            + Add Environment Variable
          </button>
        </div>

        <!-- 4. Volumes Tab -->
        <div v-show="activeTab === 'volumes'" class="tab-pane">
          <p class="tab-help">Mount host paths or named Docker volumes inside the container.</p>
          <div
            v-for="(vol, idx) in form.volumes"
            :key="idx"
            class="dynamic-row"
          >
            <input
              type="text"
              v-model="vol.host"
              placeholder="Host path or named volume"
              class="form-input compact"
            />
            <span class="row-arrow">➔</span>
            <input
              type="text"
              v-model="vol.container"
              placeholder="Container mount point (e.g. /app/data)"
              class="form-input compact"
            />
            <select v-model="vol.mode" class="form-input proto-select">
              <option value="rw">Read/Write (rw)</option>
              <option value="ro">Read-Only (ro)</option>
            </select>
            <button
              type="button"
              class="action-btn danger mini"
              @click="removeVolume(idx)"
              title="Remove volume"
            >
              ✕
            </button>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="addVolume">
            + Add Volume Mount
          </button>
        </div>

        <!-- 5. Advanced / Resources Tab -->
        <div v-show="activeTab === 'advanced'" class="tab-pane">
          <div class="form-grid">
            <div class="form-group">
              <label>Network</label>
              <select v-model="form.network" class="form-input">
                <option value="bridge">bridge (default)</option>
                <option value="host">host</option>
                <option value="none">none</option>
              </select>
            </div>
            <div class="form-group">
              <label>Restart Policy</label>
              <select v-model="form.restart_policy" class="form-input">
                <option value="unless-stopped">Unless Stopped (Recommended)</option>
                <option value="always">Always</option>
                <option value="on-failure">On Failure</option>
                <option value="no">Do not restart (no)</option>
              </select>
            </div>
          </div>

          <div class="form-grid" style="margin-top: 1rem;">
            <div class="form-group">
              <label>Memory Limit (MB)</label>
              <input
                type="number"
                v-model.number="form.memory_mb"
                placeholder="0 = Unlimited"
                class="form-input"
              />
            </div>
            <div class="form-group">
              <label>CPU Shares</label>
              <input
                type="number"
                v-model.number="form.cpu_shares"
                placeholder="1024 = Default"
                class="form-input"
              />
            </div>
          </div>

          <div class="form-group" style="margin-top: 1.25rem;">
            <label class="checkbox-label" style="display: flex; align-items: center; gap: 0.6rem; cursor: pointer;">
              <input type="checkbox" v-model="form.start" />
              <span>Start container immediately upon creation</span>
            </label>
          </div>
        </div>
      </div>

      <div class="modal-actions">
        <button class="modal-btn cancel" @click="$emit('close')" :disabled="submitting">
          Cancel
        </button>
        <button
          class="modal-btn confirm primary"
          @click="submitCreate"
          :disabled="submitting || !form.name.trim() || !form.image.trim()"
        >
          {{ submitting ? 'Launching...' : 'Launch Container' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from "vue";
import { apiFetch } from "../utils/apiFetch";

const emit = defineEmits(["close", "created"]);

const activeTab = ref("basics");
const submitting = ref(false);

const tabs = [
  { id: "basics", label: "1. Basics" },
  { id: "ports", label: "2. Ports" },
  { id: "env", label: "3. Environment" },
  { id: "volumes", label: "4. Volumes" },
  { id: "advanced", label: "5. Limits & Network" }
];

const quickImages = [
  "nginx:alpine",
  "redis:7-alpine",
  "postgres:16-alpine",
  "node:20-alpine",
  "python:3.11-alpine"
];

const form = ref({
  name: "",
  image: "",
  command: "",
  ports: [],
  env: [],
  volumes: [],
  network: "bridge",
  restart_policy: "unless-stopped",
  memory_mb: 0,
  cpu_shares: 0,
  start: true
});

const addPort = () => {
  form.value.ports.push({ host: "", container: "", proto: "tcp" });
};
const removePort = (idx) => {
  form.value.ports.splice(idx, 1);
};

const addEnv = () => {
  form.value.env.push({ key: "", val: "" });
};
const removeEnv = (idx) => {
  form.value.env.splice(idx, 1);
};

const addVolume = () => {
  form.value.volumes.push({ host: "", container: "", mode: "rw" });
};
const removeVolume = (idx) => {
  form.value.volumes.splice(idx, 1);
};

const submitCreate = async () => {
  if (!form.value.name.trim() || !form.value.image.trim()) return;
  submitting.value = true;

  // Format ports: ["8080:80/tcp"]
  const formattedPorts = form.value.ports
    .filter((p) => p.container)
    .map((p) => (p.host ? `${p.host}:${p.container}/${p.proto}` : `${p.container}/${p.proto}`));

  // Format env: ["KEY=VAL"]
  const formattedEnv = form.value.env
    .filter((e) => e.key)
    .map((e) => `${e.key}=${e.val}`);

  // Format volumes: ["host:container:rw"]
  const formattedVolumes = form.value.volumes
    .filter((v) => v.host && v.container)
    .map((v) => `${v.host}:${v.container}:${v.mode}`);

  const payload = {
    name: form.value.name.trim(),
    image: form.value.image.trim(),
    command: form.value.command.trim(),
    ports: formattedPorts,
    env: formattedEnv,
    volumes: formattedVolumes,
    network: form.value.network,
    restart_policy: form.value.restart_policy,
    memory_mb: form.value.memory_mb || 0,
    cpu_shares: form.value.cpu_shares || 0,
    start: form.value.start
  };

  try {
    const res = await apiFetch("/api/containers/create", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (res.ok) {
      emit("created", data);
      emit("close");
    } else {
      alert("Creation failed: " + (data.error || res.statusText));
    }
  } catch (err) {
    alert("Request error: " + err.message);
  } finally {
    submitting.value = false;
  }
};
</script>

<style scoped>
.wizard-modal {
  width: 640px;
  max-width: 95vw;
}

.wizard-header-title {
  display: flex;
  gap: 0.75rem;
  align-items: center;
}

.wizard-icon {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: rgba(59, 130, 246, 0.15);
  color: #3b82f6;
  display: flex;
  align-items: center;
  justify-content: center;
}

.wizard-header-title h3 {
  margin: 0;
  font-size: 1.15rem;
}

.wizard-sub {
  font-size: 0.75rem;
  color: #94a3b8;
}

.wizard-tabs {
  display: flex;
  background: rgba(0, 0, 0, 0.25);
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  overflow-x: auto;
}

.wizard-tab {
  background: transparent;
  border: none;
  padding: 0.75rem 1rem;
  color: #94a3b8;
  font-size: 0.82rem;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  border-bottom: 2px solid transparent;
  transition: all 0.15s ease;
}

.wizard-tab.active {
  color: #38bdf8;
  border-bottom-color: #38bdf8;
  background: rgba(56, 189, 248, 0.05);
}

.wizard-body {
  padding: 1.25rem;
  min-height: 280px;
}

.tab-help {
  font-size: 0.8rem;
  color: #94a3b8;
  margin-bottom: 1rem;
}

.quick-chips {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.5rem;
  flex-wrap: wrap;
}

.chip-label {
  font-size: 0.75rem;
  color: #94a3b8;
}

.quick-chip {
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #e2e8f0;
  padding: 0.2rem 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
  cursor: pointer;
}

.quick-chip:hover {
  background: rgba(59, 130, 246, 0.2);
  border-color: #3b82f6;
}

.dynamic-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.6rem;
}

.compact {
  flex: 1;
}

.proto-select {
  width: 90px;
}

.row-arrow, .row-equal {
  color: #64748b;
  font-weight: bold;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}
</style>
