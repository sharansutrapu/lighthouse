<template>
  <div class="team-manager">
    <div class="premium-table-container">
      <table class="premium-table admin-table">
        <thead>
          <tr>
            <th>Team Name</th>
            <th>Description</th>
            <th>Allowed Containers</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody v-if="teams.length > 0">
          <tr v-for="t in teams" :key="t.id">
            <td data-label="Team Name">
              <div class="user-cell">
                <div class="user-info">
                  <span class="user-name">{{ t.name }}</span>
                </div>
              </div>
            </td>
            <td data-label="Description">
              {{ t.description || 'N/A' }}
            </td>
            <td data-label="Allowed Containers">
              <code class="tag">{{ t.allowed_containers }}</code>
            </td>
            <td class="text-right" data-label="Actions">
              <div class="action-group justify-end">
                <button @click="openEditModal(t)" class="icon-btn" data-tooltip="Edit Team">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
                  </svg>
                </button>
                <button @click="openDeleteConfirm(t)" class="icon-btn stop" data-tooltip="Delete Team">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                  </svg>
                </button>
              </div>
            </td>
          </tr>
        </tbody>
        <tbody v-else>
          <tr>
            <td colspan="4">
              <div class="empty-state-wrapper">
                <div class="empty-state-content">
                  <h4 class="empty-title">No Teams</h4>
                  <p class="empty-text">Create a team to manage container access for groups of users.</p>
                </div>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- CREATE/EDIT MODAL -->
    <Teleport to="body">
      <Transition name="modal-bounce">
        <div v-if="showModal" class="modal-overlay">
          <div class="modal-card wide-modal glass shadow-2xl">
            <div class="modal-card-header">
              <div class="header-content">
                <div>
                  <h3 class="modal-title">{{ isEditing ? "Edit Team" : "New Team" }}</h3>
                  <p class="modal-subtitle">Configure team details and container access patterns</p>
                </div>
              </div>
              <button class="close-btn" @click="closeModal" aria-label="Close">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <line x1="18" y1="6" x2="6" y2="18"></line>
                  <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
              </button>
            </div>
            <div class="modal-card-body">
              <div class="form-grid">
                <div class="input-group">
                  <label class="label-caps">Team Name</label>
                  <input type="text" v-model="activeTeam.name" class="premium-input" placeholder="e.g. Developers" />
                </div>
                <div class="input-group">
                  <label class="label-caps">Description</label>
                  <input type="text" v-model="activeTeam.description" class="premium-input" placeholder="Optional description" />
                </div>
              </div>

                <div class="input-group mt-4">
                  <label class="label-caps">Assign Role Template</label>
                  <select v-model="activeTeam.role_template_id" class="premium-input">
                    <option value="">No Role Template</option>
                    <option v-for="role in roleTemplates" :key="role.id" :value="role.id">
                      {{ role.name }}
                    </option>
                  </select>
                </div>

                <div class="perm-section mt-4">
                  <label class="label-caps">Container Visibility</label>
                  <div class="pattern-input-wrap mt-2">
                    <input
                      type="text"
                      v-model="activeTeam.allowed_containers"
                      class="premium-input"
                      placeholder="e.g. api-*, prod-web, ^front.*"
                      autocomplete="off"
                    />
                  </div>
                  <p class="hint-text mt-2 mb-2">Use comma-separated names, wildcards (*), or regex. Use .* for full access.</p>
                  
                  <div v-if="runningContainers.length > 0" class="container-suggestions">
                    <span class="suggestion-label">Active Containers:</span>
                    <button 
                      v-for="c in runningContainers" 
                      :key="c.id"
                      class="suggestion-pill"
                      @click="appendContainer(c.name)"
                      type="button"
                    >
                      + {{ c.name }}
                    </button>
                  </div>
                </div>

                <div class="perm-section perm-section-compact">
                  <label class="label-caps">Action Rights</label>
                  
                  <div v-for="mod in permissionModules" :key="mod.name" class="perm-module mt-3">
                    <h4 class="module-title">{{ mod.name }}</h4>
                    <div class="modern-rights-grid">
                      <label
                        v-for="action in mod.actions"
                        :key="action.field"
                        class="right-card"
                        :class="{ checked: activeTeam[action.field] }"
                      >
                        <input
                          type="checkbox"
                          v-model="activeTeam[action.field]"
                        />
                        <div class="right-card-content">
                          <div class="custom-check">
                            <svg v-if="activeTeam[action.field]" viewBox="0 0 24 24" width="12" height="12" stroke="currentColor" stroke-width="3" fill="none"><polyline points="20 6 9 17 4 12"></polyline></svg>
                          </div>
                          <span class="right-label">{{ action.label }}</span>
                        </div>
                      </label>
                    </div>
                  </div>
                </div>

                <div class="perm-section mt-4">
                  <label class="label-caps">Alert Destinations (Team specific)</label>
                  <p class="hint-text mb-3">Alerts for containers matching the above visibility will be sent to these destinations in addition to the global admin alerts.</p>
                  <div class="form-grid">
                    <div class="input-group">
                      <label class="label-caps">Email Address</label>
                      <input type="email" v-model="activeTeam.alerts_email_address" class="premium-input" placeholder="team@example.com" />
                    </div>
                    <div class="input-group">
                      <label class="label-caps">Slack Webhook URL</label>
                      <input type="url" v-model="activeTeam.slack_webhook_url" class="premium-input" placeholder="https://hooks.slack.com/services/..." />
                    </div>
                    <div class="input-group">
                      <label class="label-caps">MS Teams Webhook URL</label>
                      <input type="url" v-model="activeTeam.msteams_webhook_url" class="premium-input" placeholder="https://outlook.office.com/webhook/..." />
                    </div>
                    <div class="input-group">
                      <label class="label-caps">Google Chat Webhook URL</label>
                      <input type="url" v-model="activeTeam.gchat_webhook_url" class="premium-input" placeholder="https://chat.googleapis.com/v1/spaces/..." />
                    </div>
                    <div class="input-group">
                      <label class="label-caps">Generic Webhook URL</label>
                      <input type="url" v-model="activeTeam.generic_webhook_url" class="premium-input" placeholder="https://api.example.com/webhook" />
                    </div>
                  </div>
                </div>
            </div>

            <div class="modal-card-footer">
              <button @click="closeModal" class="btn-secondary">Cancel</button>
              <button @click="saveTeam" class="btn-primary">
                {{ isEditing ? "Save Changes" : "Create Team" }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- DELETE CONFIRMATION -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showDeleteModal" class="modal-overlay" @click.self="closeModal">
          <div class="modal-content shadow-2xl">
            <div class="modal-icon error">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="24" height="24">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </div>
            <h3>Delete Team?</h3>
            <p>Permanently remove <strong>{{ teamToDelete?.name }}</strong>? Users in this team will lose the team's inherited permissions.</p>
            <div class="modal-actions" style="margin-top: 1.5rem">
              <button @click="closeModal" class="modal-btn cancel">Cancel</button>
              <button @click="confirmDelete" class="modal-btn confirm error">Delete Team</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { showToast } from "../utils/sharedState";
import { apiFetch } from "../utils/apiFetch";

const props = defineProps({
  token: String,
});

const emit = defineEmits(["update-count"]);

const teams = ref([]);
const showModal = ref(false);
const showDeleteModal = ref(false);
const isEditing = ref(false);

const defaultTeam = { 
  name: "", 
  description: "", 
  allowed_containers: ".*",
  role_template_id: "",
  can_start: false,
  can_stop: false,
  can_restart: false,
  can_delete: false,
  can_shell: false,
  can_view_system_health: false,
  can_run_scans: false,
  can_create_deployments: false,
  can_edit_deployments: false,
  can_delete_deployments: false,
  alerts_email_address: "",
  slack_webhook_url: "",
  msteams_webhook_url: "",
  gchat_webhook_url: "",
  generic_webhook_url: ""
};
const activeTeam = ref({ ...defaultTeam });
const teamToDelete = ref(null);

const fetchTeams = async () => {
  try {
    const res = await apiFetch("/api/admin/teams", {
      headers: { Authorization: `Bearer ${props.token}` },
    });
    if (res.ok) {
      teams.value = await res.json() || [];
      emit("update-count", teams.value.length);
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
};

const roleTemplates = ref([]);
const fetchRoleTemplates = async () => {
  try {
    const res = await apiFetch("/api/admin/role_templates", {
      headers: { Authorization: `Bearer ${props.token}` },
    });
    if (res.ok) {
      roleTemplates.value = await res.json();
    }
  } catch (err) {
    console.error("Failed to fetch role templates", err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
};

const runningContainers = ref([]);
const fetchContainers = async () => {
  try {
    const res = await apiFetch("/api/containers", {
      headers: { Authorization: `Bearer ${props.token}` },
    });
    if (res.ok) {
      const data = await res.json();
      runningContainers.value = data.filter(c => c.state === 'running') || [];
    }
  } catch (err) {
    console.error("Failed to fetch containers", err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
};

const appendContainer = (name) => {
  let current = activeTeam.value.allowed_containers || "";
  if (current === ".*" || current === "") {
    activeTeam.value.allowed_containers = name;
  } else {
    // avoid duplicates
    const parts = current.split(",").map(p => p.trim());
    if (!parts.includes(name)) {
      activeTeam.value.allowed_containers = current + ", " + name;
    }
  }
};

const permissionModules = [
  {
    name: "Containers",
    actions: [
      { label: "Start Container", field: "can_start" },
      { label: "Stop Container", field: "can_stop" },
      { label: "Restart Container", field: "can_restart" },
      { label: "Delete Container", field: "can_delete" },
      { label: "Terminal Access", field: "can_shell" }
    ]
  },
  {
    name: "System Health",
    actions: [
      { label: "View System Health", field: "can_view_system_health" }
    ]
  },
  {
    name: "Vulnerability Scans",
    actions: [
      { label: "Run Scans", field: "can_run_scans" }
    ]
  },
  {
    name: "Deployments & Stacks",
    actions: [
      { label: "Create Deployment", field: "can_create_deployments" },
      { label: "Edit Inline", field: "can_edit_deployments" },
      { label: "Delete Deployment", field: "can_delete_deployments" }
    ]
  }
];

const openCreateModal = () => {
  isEditing.value = false;
  activeTeam.value = { ...defaultTeam };
  showModal.value = true;
};

const openEditModal = (team) => {
  isEditing.value = true;
  activeTeam.value = { ...team };
  showModal.value = true;
};

const openDeleteConfirm = (team) => {
  teamToDelete.value = team;
  showDeleteModal.value = true;
};

const closeModal = () => {
  showModal.value = false;
  showDeleteModal.value = false;
  activeTeam.value = { ...defaultTeam };
  teamToDelete.value = null;
};

const saveTeam = async () => {
  const isUpdate = isEditing.value;
  const url = isUpdate ? `/api/admin/teams/${activeTeam.value.id}` : "/api/admin/teams";
  const method = isUpdate ? "PUT" : "POST";

  const form = new URLSearchParams();
  form.append("name", activeTeam.value.name);
  form.append("description", activeTeam.value.description);
  form.append("allowed_containers", activeTeam.value.allowed_containers);
  if (activeTeam.value.role_template_id) {
    form.append("role_template_id", activeTeam.value.role_template_id);
  } else {
    form.append("role_template_id", "null");
  }
  
  form.append("alerts_email_address", activeTeam.value.alerts_email_address || "");
  form.append("slack_webhook_url", activeTeam.value.slack_webhook_url || "");
  form.append("msteams_webhook_url", activeTeam.value.msteams_webhook_url || "");
  form.append("gchat_webhook_url", activeTeam.value.gchat_webhook_url || "");
  form.append("generic_webhook_url", activeTeam.value.generic_webhook_url || "");
  
  // Append all permission fields
  permissionModules.forEach(mod => {
    mod.actions.forEach(action => {
      form.append(action.field, activeTeam.value[action.field] ? "true" : "false");
    });
  });

  try {
    const res = await apiFetch(url, {
      method,
      headers: { Authorization: `Bearer ${props.token}`, "Content-Type": "application/x-www-form-urlencoded" },
      body: form.toString(),
    });
    if (res.ok) {
      showToast("Success", isEditing.value ? "Team updated" : "Team created", "success");
      closeModal();
      fetchTeams();
    } else {
      const errorData = await res.json().catch(() => ({}));
      showToast("Error", errorData.error || "Failed to save team", "error");
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
    showToast("Error", "Network error", "error");
  }
};

const confirmDelete = async () => {
  if (!teamToDelete.value) return;
  try {
    const res = await apiFetch(`/api/admin/teams/${teamToDelete.value.id}`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${props.token}` },
    });
    if (res.ok) {
      showToast("Success", "Team deleted", "success");
      closeModal();
      fetchTeams();
    }
  } catch (err) {
    console.error(err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  }
};

onMounted(() => {
  fetchTeams();
  fetchRoleTemplates();
  fetchContainers();
});

defineExpose({ openCreateModal });
</script>

<style scoped>
.action-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.action-group.justify-end {
  justify-content: flex-end;
}

.modal-card-body {
  max-height: 65vh;
  overflow-y: auto;
}
/* Enhance form styling to match the rest of the application */
.perm-section {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid rgba(255, 255, 255, 0.05);
  border-radius: 8px;
  padding: 1rem;
}

/* Footer & Buttons */
.modal-card-footer {
  padding: 1.5rem 2rem;
  border-top: 1px solid var(--border);
  display: flex;
  gap: 1rem;
  justify-content: flex-end;
}
.btn-primary {
  background: var(--accent);
  color: white;
  font-family: var(--font-main);
  font-size: 0.95rem;
  font-weight: 700;
  padding: 0.8rem 1.5rem;
  border-radius: 12px;
  border: none;
  cursor: pointer;
  box-shadow: 0 4px 15px rgba(59, 130, 246, 0.3);
  transition: all 0.2s;
}
.btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 20px rgba(59, 130, 246, 0.4);
}
.btn-secondary {
  background: transparent;
  color: var(--text-main);
  font-family: var(--font-main);
  font-size: 0.95rem;
  font-weight: 600;
  padding: 0.8rem 1.5rem;
  border-radius: 12px;
  border: 1px solid var(--border);
  cursor: pointer;
  transition: all 0.2s;
}
.btn-secondary:hover {
  background: var(--bg-subtle);
}
.modal-text-center {
  text-align: center;
}
.modal-actions {
  display: flex;
  gap: 1rem;
  margin-top: 1.5rem;
}
.modal-btn {
  padding: 0.8rem 1.5rem;
  border-radius: 12px;
  font-weight: 700;
  border: none;
  cursor: pointer;
  flex: 1;
}
.modal-btn.cancel {
  background: var(--bg-subtle);
  color: var(--text-main);
}
.modal-btn.confirm.error {
  background: var(--stop);
  color: white;
}
.flex-1 {
  flex: 1;
}

/* Modal General */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  backdrop-filter: blur(10px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 1rem;
}

.modal-card {
  width: 100%;
  max-width: 650px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 24px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  max-height: 90vh;
}

.modal-card-header {
  padding: 1.5rem 2rem;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
  border-bottom: 1px solid var(--border);
}

.close-btn {
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-input);
  color: var(--text-dim);
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease, border-color 0.2s ease;
}

.close-btn svg {
  width: 18px;
  height: 18px;
}

.close-btn:hover {
  background: var(--bg-subtle);
  color: var(--text-main);
  border-color: var(--border-active);
}

.header-content {
  display: flex;
  gap: 1.25rem;
  align-items: center;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1.5rem;
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
.input-group {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.label-caps {
  display: block;
  font-family: var(--font-main);
  text-transform: uppercase;
  font-size: 0.68rem;
  font-weight: 800;
  color: var(--text-dim);
  letter-spacing: 0.05em;
}
.hint-text {
  font-family: var(--font-main);
  font-size: 0.72rem;
  font-weight: 500;
  color: var(--text-mute);
  margin: 0;
}
.modal-card-body {
  padding: 2rem;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}
.modal-title {
  font-size: 1.25rem;
  font-weight: 800;
  color: var(--text-main);
  margin: 0;
}
.modal-subtitle {
  font-size: 0.85rem;
  color: var(--text-mute);
  margin-top: 0.25rem;
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 0.65rem;
}

.mini-avatar {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent);
  border: 1px solid rgba(var(--accent-rgb), 0.15);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.8rem;
  font-weight: 800;
  flex-shrink: 0;
}

.user-name {
  font-weight: 800;
  color: var(--text-main);
}
</style>
