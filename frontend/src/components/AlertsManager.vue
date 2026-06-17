<template>
  <div class="alerts-manager">

    <!-- ── Toolbar ──────────────────────────────────────────────────────────── -->
    <div class="page-toolbar alerts-toolbar">
      <div class="tab-switcher">
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'rules' }"
          @click="activeTab = 'rules'"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="3">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          </svg>
          Active Rules
          <span class="tab-count" v-if="rules.length">{{ rules.length }}</span>
        </button>
        <button
          class="tab-btn"
          :class="{ active: activeTab === 'history' }"
          @click="activeTab = 'history'; fetchHistory()"
        >
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="3">
            <circle cx="12" cy="12" r="10"/>
            <polyline points="12 6 12 12 16 14"/>
          </svg>
          Alert History
          <span class="tab-count" v-if="history.length">{{ history.length }}</span>
        </button>
      </div>

      <div class="toolbar-right">
        <button @click="loadRules" class="page-btn" :disabled="loading" data-tooltip="Refresh">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3"
            :class="{ rotating: loading }">
            <polyline points="23 4 23 10 17 10"/>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
          </svg>
          Refresh
        </button>
        <button @click="openCreateModal" class="page-btn primary">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3">
            <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
          New Rule
        </button>
      </div>
    </div>

    <!-- ── Rules Grid ───────────────────────────────────────────────────────── -->
    <div v-if="activeTab === 'rules'">
      <!-- Loading skeleton -->
      <div v-if="loading" class="rules-grid">
        <div v-for="i in 3" :key="i" class="rule-card skeleton">
          <div class="shimmer" style="height:130px; border-radius:16px;"/>
        </div>
      </div>

      <!-- Empty state -->
      <div v-else-if="!rules.length" class="empty-state-wrapper">
        <div class="empty-state-content">
          <div class="empty-icon-box">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
            </svg>
          </div>
          <h4 class="empty-title">No Alert Rules</h4>
          <p class="empty-text">Create your first rule to start receiving notifications when containers crash or log patterns match.</p>
          <button @click="openCreateModal" class="btn-primary mt-4">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Create First Rule
          </button>
        </div>
      </div>

      <!-- Rules grid -->
      <div v-else class="rules-grid">
        <div
          v-for="rule in rules"
          :key="rule.id"
          class="rule-card premium-card"
          :class="{ disabled: !rule.enabled }"
        >
          <!-- Card top bar: name + toggle -->
          <div class="rule-card-header">
            <div class="rule-name-group">
              <div class="rule-channel-icon" :class="rule.channel_type">
                <svg v-if="rule.channel_type === 'slack'" viewBox="0 0 24 24" width="14" height="14" fill="currentColor">
                  <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z"/>
                </svg>
                <svg v-else viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                  <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
                  <polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
                </svg>
              </div>
              <div>
                <span class="rule-name">{{ rule.name }}</span>
                <span class="rule-channel-label">{{ rule.channel_type === 'slack' ? 'Slack / Discord' : 'Webhook' }}</span>
              </div>
            </div>
            <!-- Toggle switch -->
            <button
              class="toggle-switch"
              :class="{ on: rule.enabled }"
              @click="toggleRule(rule)"
              :data-tooltip="rule.enabled ? 'Disable rule' : 'Enable rule'"
              :aria-label="rule.enabled ? 'Disable rule' : 'Enable rule'"
            >
              <span class="toggle-thumb"/>
            </button>
          </div>

          <!-- Criteria chips -->
          <div class="rule-criteria">
            <div v-if="rule.container_pattern" class="crit-chip accent">
              <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
                <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
              </svg>
              <code>{{ rule.container_pattern }}</code>
            </div>
            <template v-if="rule.event_types">
              <span v-for="ev in splitEvents(rule.event_types)" :key="ev" class="crit-chip event">
                {{ ev }}
              </span>
            </template>
            <div v-if="rule.log_pattern" class="crit-chip log">
              <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
                <line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/>
                <line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/>
                <line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/>
              </svg>
              <code>{{ rule.log_pattern }}</code>
            </div>
            <div v-if="rule.metric_cpu_threshold > 0" class="crit-chip warning">
              <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
                <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
              </svg>
              <code>CPU &gt; {{ rule.metric_cpu_threshold }}%</code>
            </div>
            <div v-if="rule.metric_mem_threshold > 0" class="crit-chip warning">
              <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
                <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect><rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
              </svg>
              <code>Mem &gt; {{ rule.metric_mem_threshold }}MB</code>
            </div>
          </div>

          <!-- Footer: cooldown + actions -->
          <div class="rule-card-footer">
            <span class="cooldown-badge">
              <svg viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" stroke-width="2.5">
                <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
              </svg>
              {{ formatCooldown(rule.cooldown_seconds) }} cooldown
            </span>
            <div class="card-actions">
              <button @click="openEditModal(rule)" class="icon-btn" data-tooltip="Edit rule">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                  <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                  <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                </svg>
              </button>
              <button @click="confirmDelete(rule)" class="icon-btn stop" data-tooltip="Delete rule">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
                  <path d="M10 11v6"/><path d="M14 11v6"/>
                </svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ── History Table ─────────────────────────────────────────────────────── -->
    <div v-if="activeTab === 'history'">
      <div class="page-toolbar" style="margin-bottom:1rem">
        <div class="search-box">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3">
            <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
          </svg>
          <input v-model="historySearch" type="text" placeholder="Filter by container or details…"/>
        </div>
        <button @click="fetchHistory" class="page-btn" :disabled="historyLoading">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3"
            :class="{ rotating: historyLoading }">
            <polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
          </svg>
          Refresh
        </button>
      </div>

      <div class="premium-table-container">
        <table class="premium-table audit-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Rule</th>
              <th>Container</th>
              <th>Type</th>
              <th>Delivery</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody v-if="filteredHistory.length">
            <tr v-for="entry in filteredHistory" :key="entry.id" class="audit-row">
              <td>
                <span class="date-part">{{ formatDate(entry.timestamp) }}</span>
                <span class="time-part">{{ formatTime(entry.timestamp) }}</span>
              </td>
              <td>
                <span class="rule-ref">{{ entry.rule_name || '—' }}</span>
              </td>
              <td>
                <code class="resource-code">{{ entry.container_name }}</code>
              </td>
              <td>
                <span :class="['alert-type-badge', entry.alert_type]">
                  <span class="type-dot"/>
                  {{ entry.alert_type === 'event' ? '⚡ Crash Event' : '📄 Log Match' }}
                </span>
              </td>
              <td>
                <div class="delivery-status">
                  <span class="delivery-channel" v-if="entry.delivery_channel">{{ entry.delivery_channel }}</span>
                  <span class="delivery-msg" :title="entry.delivery_status">{{ entry.delivery_status || 'Pending' }}</span>
                </div>
              </td>
              <td class="message-cell">
                <div style="display: flex; align-items: center; justify-content: space-between;">
                  <p class="truncate-msg" :title="entry.details" style="margin: 0;">{{ entry.details }}</p>
                  <button @click="openDetailsModal(entry)" class="icon-btn" data-tooltip="View full details" style="margin-left: 0.5rem; flex-shrink: 0;">
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
          <tbody v-else>
            <tr>
              <td colspan="6">
                <div class="empty-state-wrapper">
                  <div class="empty-state-content">
                    <div class="empty-icon-box">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                        <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
                      </svg>
                    </div>
                    <h4 class="empty-title">No Alert History</h4>
                    <p class="empty-text">No notifications have fired yet. History will appear here once rules trigger.</p>
                  </div>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- ────────────────────────────────────────────────────────────────────── -->
    <!-- View Details Modal                                                     -->
    <!-- ────────────────────────────────────────────────────────────────────── -->
    <Teleport to="body">
      <Transition name="modal-bounce">
        <div v-if="showDetailsModal" class="modal-overlay" @mousedown.self="showDetailsModal = false">
          <div class="modal-card wide-modal glass shadow-2xl">
            <div class="modal-card-header">
              <div class="header-content">
                <div class="header-icon accent">
                  <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle>
                  </svg>
                </div>
                <div>
                  <h3 class="modal-title">Alert Details</h3>
                  <p class="modal-subtitle">Full payload of the triggered alert</p>
                </div>
              </div>
              <button class="close-btn" @click="showDetailsModal = false">×</button>
            </div>
            <div class="modal-card-body" style="padding: 1.5rem;" v-if="viewDetailsEntry">
              <div style="margin-bottom: 1.5rem; padding: 1rem; background: var(--bg-subtle); border-radius: 8px; border: 1px solid var(--border);">
                <div style="display: flex; justify-content: space-between; margin-bottom: 0.75rem;">
                  <span class="label-caps" style="margin: 0;">Delivery Channels</span>
                  <strong>{{ viewDetailsEntry.delivery_channel || 'None' }}</strong>
                </div>
                <div style="display: flex; justify-content: space-between;">
                  <span class="label-caps" style="margin: 0;">Delivery Status</span>
                  <span :class="{'text-accent': viewDetailsEntry.delivery_status?.toLowerCase().includes('success'), 'text-error': viewDetailsEntry.delivery_status?.toLowerCase().includes('fail')}">
                    {{ viewDetailsEntry.delivery_status || 'Pending' }}
                  </span>
                </div>
              </div>
              <label class="label-caps" style="margin-bottom: 0.5rem; display: block;">Alert Payload</label>
              <pre class="premium-code-block" style="margin: 0; white-space: pre-wrap; font-size: 0.85rem; color: var(--text-main); background: var(--bg-subtle); padding: 1rem; border-radius: 8px; border: 1px solid var(--border);">{{ viewDetailsEntry.details }}</pre>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- ────────────────────────────────────────────────────────────────────── -->
    <!-- Rule Create / Edit Modal                                               -->
    <!-- ────────────────────────────────────────────────────────────────────── -->
    <Teleport to="body">
      <Transition name="modal-bounce">
        <div v-if="showModal" class="modal-overlay" @mousedown.self="closeModal">
          <div class="modal-card wide-modal glass shadow-2xl">

            <!-- Modal header -->
            <div class="modal-card-header">
              <div class="header-content">
                <div class="header-icon" :class="editingRule ? 'warning' : 'accent'">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
                  </svg>
                </div>
                <div>
                  <h3 class="modal-title">{{ editingRule ? 'Edit Alert Rule' : 'New Alert Rule' }}</h3>
                  <p class="modal-subtitle">Configure notification criteria and delivery channel</p>
                </div>
              </div>
              <button class="close-btn" @click="closeModal">×</button>
            </div>

            <!-- Modal body -->
            <div class="modal-card-body">
              <div v-if="formError" class="form-error">{{ formError }}</div>

              <!-- Name -->
              <div class="input-group">
                <label class="label-caps">Rule Name <span class="req">*</span></label>
                <input
                  v-model="form.name"
                  type="text"
                  class="premium-input"
                  placeholder="e.g. Nginx crash alert"
                  autofocus
                />
              </div>

              <!-- Container Pattern -->
              <div class="input-group">
                <label class="label-caps">
                  Container Name Pattern <span class="req">*</span>
                  <span class="label-hint">(Go regex)</span>
                </label>
                <input
                  v-model="form.container_pattern"
                  type="text"
                  class="premium-input mono"
                  placeholder="^nginx-.*$  or  .*  for all containers"
                />
              </div>

              <!-- Event Triggers -->
              <div class="input-group">
                <label class="label-caps">Docker Event Triggers</label>
                <div class="checkbox-row">
                  <label class="check-pill" :class="{ active: form.events.die }">
                    <input type="checkbox" v-model="form.events.die" style="display:none"/>
                    <span class="check-dot" :class="{ on: form.events.die }"/>
                    💀 Container Die
                  </label>
                  <label class="check-pill" :class="{ active: form.events.oom }">
                    <input type="checkbox" v-model="form.events.oom" style="display:none"/>
                    <span class="check-dot" :class="{ on: form.events.oom }"/>
                    🔥 OOM Kill
                  </label>
                  <label class="check-pill" :class="{ active: form.events.health_status }">
                    <input type="checkbox" v-model="form.events.health_status" style="display:none"/>
                    <span class="check-dot" :class="{ on: form.events.health_status }"/>
                    💔 Health Status
                  </label>
                </div>
              </div>

              <!-- Log Pattern -->
              <div class="input-group">
                <label class="label-caps">
                  Log Keyword Regex
                  <span class="label-hint">(optional — scans stdout/stderr)</span>
                </label>
                <input
                  v-model="form.log_pattern"
                  type="text"
                  class="premium-input mono"
                  placeholder="(?i)error|exception|fatal|panic"
                />
              </div>

              <div class="form-grid dual">
                <!-- CPU Threshold -->
                <div class="input-group">
                  <label class="label-caps">
                    CPU Threshold (%)
                    <span class="label-hint">(0 to disable)</span>
                  </label>
                  <input
                    v-model.number="form.metric_cpu_threshold"
                    type="number"
                    step="0.01"
                    min="0"
                    class="premium-input"
                    placeholder="e.g. 80.00"
                  />
                </div>

                <!-- Memory Threshold -->
                <div class="input-group">
                  <label class="label-caps">
                    Memory Threshold (MB)
                    <span class="label-hint">(0 to disable)</span>
                  </label>
                  <input
                    v-model.number="form.metric_mem_threshold"
                    type="number"
                    min="0"
                    class="premium-input"
                    placeholder="e.g. 512"
                  />
                </div>
              </div>

              <div class="form-grid dual">
                <!-- Cooldown -->
                <div class="input-group">
                  <label class="label-caps">Cooldown Window</label>
                  <select v-model="form.cooldown_seconds" class="premium-input">
                    <option :value="30">30 seconds</option>
                    <option :value="60">1 minute</option>
                    <option :value="300">5 minutes (default)</option>
                    <option :value="900">15 minutes</option>
                    <option :value="1800">30 minutes</option>
                    <option :value="3600">1 hour</option>
                  </select>
                </div>

                <!-- Delivery Toggles -->
                <div class="input-group">
                  <label class="label-caps">Delivery Methods</label>
                  <div class="events-group">
                    <label class="check-pill" :class="{ active: form.enable_webhook }">
                      <input type="checkbox" v-model="form.enable_webhook" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_webhook }"/>
                      Webhook
                    </label>
                    <label class="check-pill" :class="{ active: form.enable_email }">
                      <input type="checkbox" v-model="form.enable_email" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_email }"/>
                      Email
                    </label>
                  </div>
                </div>
              </div>


              <!-- Email Config -->
              <template v-if="form.enable_email">
                <div class="input-group">
                  <label class="label-caps">
                    Email Address <span class="req">*</span>
                  </label>
                  <input
                    v-model="form.email_address"
                    type="email"
                    class="premium-input"
                    placeholder="alerts@example.com"
                  />
                </div>
              </template>

              <!-- Enabled toggle -->
              <div class="input-group enabled-row">
                <label class="label-caps" style="margin:0">Rule Active</label>
                <button
                  class="toggle-switch"
                  :class="{ on: form.enabled }"
                  @click="form.enabled = !form.enabled"
                  :aria-label="form.enabled ? 'Disable' : 'Enable'"
                  type="button"
                >
                  <span class="toggle-thumb"/>
                </button>
              </div>
            </div>

            <!-- Modal footer -->
            <div class="modal-card-footer">
              <button @click="closeModal" class="btn-secondary">Cancel</button>
              <button @click="saveRule" class="btn-primary" :disabled="saving">
                <svg v-if="saving" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="3"
                  class="rotating">
                  <polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
                </svg>
                {{ editingRule ? 'Save Changes' : 'Create Rule' }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- ── Delete Confirmation Modal ────────────────────────────────────────── -->
    <Teleport to="body">
      <Transition name="modal-bounce">
        <div v-if="showDeleteModal" class="modal-overlay" @mousedown.self="showDeleteModal = false">
          <div class="modal-card glass shadow-2xl" style="max-width:420px">
            <div class="modal-card-header">
              <div class="header-content">
                <div class="header-icon error">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
                    <path d="M10 11v6"/><path d="M14 11v6"/>
                  </svg>
                </div>
                <div>
                  <h3 class="modal-title">Delete Rule</h3>
                  <p class="modal-subtitle">This action is permanent</p>
                </div>
              </div>
              <button class="close-btn" @click="showDeleteModal = false">×</button>
            </div>
            <div class="modal-card-body">
              <p style="color:var(--text-dim); font-size:0.9rem; line-height:1.6;">
                Are you sure you want to delete <strong style="color:var(--text-main)">{{ deletingRule?.name }}</strong>?
                All alert history for this rule will be preserved but the rule will no longer fire.
              </p>
            </div>
            <div class="modal-card-footer">
              <button @click="showDeleteModal = false" class="btn-secondary">Cancel</button>
              <button @click="deleteRule" class="btn-danger" :disabled="saving">
                {{ saving ? 'Deleting…' : 'Delete Rule' }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { apiFetch } from '../utils/apiFetch';
import { secureStorage } from '../utils/storage';
import { showToast } from '../utils/sharedState';

// ── Auth ──────────────────────────────────────────────────────────────────────

const authHeaders = () => ({
  Authorization: `Bearer ${secureStorage.getItem('token')}`,
});

// ── State ─────────────────────────────────────────────────────────────────────

const activeTab   = ref('rules');
const loading     = ref(false);
const saving      = ref(false);
const rules       = ref([]);
const history     = ref([]);
const historyLoading = ref(false);
const historySearch  = ref('');

const showModal       = ref(false);
const showDeleteModal = ref(false);
const showDetailsModal = ref(false);
const viewDetailsEntry = ref(null);
const editingRule     = ref(null);
const deletingRule    = ref(null);
const formError       = ref('');

const emptyForm = () => ({
  name: '',
  container_pattern: '.*',
  events: { die: false, oom: false, health_status: false },
  log_pattern: '',
  cooldown_seconds: 300,
  channel_type: 'slack',
  webhook_url: '',
  enable_webhook: true,
  enable_email: false,
  email_address: '',
  metric_cpu_threshold: 0,
  metric_mem_threshold: 0,
  enabled: true,
});

const form = ref(emptyForm());

// ── Computed ──────────────────────────────────────────────────────────────────

const filteredHistory = computed(() => {
  const q = historySearch.value.toLowerCase();
  if (!q) return history.value;
  return history.value.filter(h =>
    h.container_name?.toLowerCase().includes(q) ||
    h.rule_name?.toLowerCase().includes(q) ||
    h.details?.toLowerCase().includes(q)
  );
});

// ── API helpers ───────────────────────────────────────────────────────────────

const apiBase = '/api/admin/alerts';

const loadRules = async () => {
  loading.value = true;
  try {
    const res = await apiFetch(`${apiBase}/rules`, { headers: authHeaders() });
    if (res.ok) rules.value = await res.json();
    else showToast('Error', 'Failed to load alert rules', 'error');
  } catch (e) {
    showToast('Error', 'Network error loading rules', 'error');
  } finally {
    loading.value = false;
  }
};

const fetchHistory = async () => {
  historyLoading.value = true;
  try {
    const res = await apiFetch(`${apiBase}/history?limit=200`, { headers: authHeaders() });
    if (res.ok) history.value = await res.json();
    else showToast('Error', 'Failed to load alert history', 'error');
  } catch (e) {
    showToast('Error', 'Network error loading history', 'error');
  } finally {
    historyLoading.value = false;
  }
};

// ── Modal helpers ─────────────────────────────────────────────────────────────

const openCreateModal = () => {
  editingRule.value = null;
  form.value = emptyForm();
  formError.value = '';
  showModal.value = true;
};

const openDetailsModal = (entry) => {
  viewDetailsEntry.value = entry;
  showDetailsModal.value = true;
};

const openEditModal = (rule) => {
  editingRule.value = rule;
  const evList = (rule.event_types || '').split(',').map(s => s.trim());
  form.value = {
    name: rule.name,
    container_pattern: rule.container_pattern,
    events: {
      die: evList.includes('die'),
      oom: evList.includes('oom'),
      health_status: evList.includes('health_status'),
    },
    log_pattern: rule.log_pattern || '',
    cooldown_seconds: rule.cooldown_seconds ?? 300,
    channel_type: rule.channel_type || 'slack',
    webhook_url: (() => {
      try { return JSON.parse(rule.channel_config || '{}').url || ''; } catch { return ''; }
    })(),
    enable_webhook: rule.enable_webhook !== false,
    enable_email: !!rule.enable_email,
    email_address: rule.email_address || '',
    metric_cpu_threshold: rule.metric_cpu_threshold || 0,
    metric_mem_threshold: rule.metric_mem_threshold || 0,
    enabled: rule.enabled !== false,
  };
  formError.value = '';
  showModal.value = true;
};

const closeModal = () => { showModal.value = false; };

// Build event_types string from checkboxes
const buildEventTypes = () =>
  Object.entries(form.value.events)
    .filter(([, v]) => v)
    .map(([k]) => k)
    .join(',');

const validate = () => {
  if (!form.value.name.trim()) return 'Rule name is required.';
  if (!form.value.container_pattern.trim()) return 'Container pattern is required.';

  if (form.value.enable_email) {
    if (!form.value.email_address.trim()) return 'Email address is required.';
    if (!form.value.email_address.includes('@')) return 'Please enter a valid email address.';
  }
  if (!form.value.enable_webhook && !form.value.enable_email) {
    return 'Select at least one delivery method (Webhook or Email).';
  }
  const hasEvent = Object.values(form.value.events).some(Boolean);
  const hasLog   = !!form.value.log_pattern.trim();
  if (!hasEvent && !hasLog) return 'Select at least one Docker event or provide a log pattern.';
  return '';
};

const saveRule = async () => {
  formError.value = validate();
  if (formError.value) return;

  saving.value = true;
  try {
    const body = new FormData();
    body.append('name',              form.value.name.trim());
    body.append('container_pattern', form.value.container_pattern.trim());
    body.append('event_types',       buildEventTypes());
    body.append('log_pattern',       form.value.log_pattern.trim());
    body.append('cooldown_seconds',  String(form.value.cooldown_seconds));
    body.append('channel_type',      'generic_webhook');
    body.append('channel_config',    '{}');
    body.append('enable_webhook',    String(form.value.enable_webhook));
    body.append('enable_email',      String(form.value.enable_email));
    body.append('email_address',     form.value.email_address.trim());
    body.append('metric_cpu_threshold', String(form.value.metric_cpu_threshold));
    body.append('metric_mem_threshold', String(form.value.metric_mem_threshold));
    body.append('enabled',           String(form.value.enabled));

    const isEdit = !!editingRule.value;
    const url    = isEdit ? `${apiBase}/rules/${editingRule.value.id}` : `${apiBase}/rules`;
    const method = isEdit ? 'PUT' : 'POST';

    const res = await apiFetch(url, { method, headers: authHeaders(), body });
    if (res.ok || res.status === 201) {
      showToast('Success', isEdit ? 'Rule updated' : 'Rule created', 'success');
      closeModal();
      await loadRules();
    } else {
      const data = await res.json().catch(() => ({}));
      formError.value = data.error || `Server returned ${res.status}`;
    }
  } catch (e) {
    formError.value = 'Network error — please try again.';
  } finally {
    saving.value = false;
  }
};

// ── Toggle ────────────────────────────────────────────────────────────────────

const toggleRule = async (rule) => {
  const next = !rule.enabled;
  const body = new FormData();
  body.append('enabled', String(next));
  try {
    const res = await apiFetch(`${apiBase}/rules/${rule.id}/toggle`, {
      method: 'PUT', headers: authHeaders(), body,
    });
    if (res.ok) {
      rule.enabled = next;
      showToast('Updated', `Rule "${rule.name}" ${next ? 'enabled' : 'disabled'}`, 'success');
    }
  } catch { showToast('Error', 'Failed to toggle rule', 'error'); }
};

// ── Delete ────────────────────────────────────────────────────────────────────

const confirmDelete = (rule) => { deletingRule.value = rule; showDeleteModal.value = true; };

const deleteRule = async () => {
  saving.value = true;
  try {
    const res = await apiFetch(`${apiBase}/rules/${deletingRule.value.id}`, {
      method: 'DELETE', headers: authHeaders(),
    });
    if (res.ok) {
      showToast('Deleted', `Rule "${deletingRule.value.name}" removed`, 'success');
      showDeleteModal.value = false;
      await loadRules();
    }
  } catch { showToast('Error', 'Failed to delete rule', 'error'); }
  finally { saving.value = false; }
};

// ── Formatting helpers ────────────────────────────────────────────────────────

const splitEvents  = (s) => (s || '').split(',').map(e => e.trim()).filter(Boolean);

const formatCooldown = (s) => {
  if (!s) return '5m';
  if (s < 60)   return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  return `${Math.round(s / 3600)}h`;
};

const formatDate = (ts) => new Date(ts).toLocaleDateString([], { month: 'short', day: 'numeric' });
const formatTime = (ts) => new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(loadRules);

// ── Exposed API for parent ref ────────────────────────────────────────────────
defineExpose({ openCreateModal });
</script>

<style scoped>
/* ── Toolbar ── */
.alerts-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-bottom: 1rem;
}
.toolbar-right {
  display: flex;
  gap: 0.65rem;
  align-items: center;
}

/* ── Tab switcher ── */
.tab-switcher {
  display: flex;
  padding: 0.3rem;
  background: var(--bg-subtle);
  border-radius: 14px;
  border: 1px solid var(--border);
  gap: 0.1rem;
}
.tab-btn {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  padding: 0.6rem 1.1rem;
  border-radius: 10px;
  font-weight: 800;
  font-size: 0.82rem;
  color: var(--text-mute);
  transition: all 0.2s;
}
.tab-btn.active {
  background: var(--accent);
  color: #fff;
  box-shadow: 0 4px 12px rgba(var(--accent-rgb), 0.28);
}
.tab-count {
  background: rgba(255,255,255,0.2);
  padding: 0.05rem 0.4rem;
  border-radius: 99px;
  font-size: 0.7rem;
}
.tab-btn:not(.active) .tab-count {
  background: var(--bg-subtle);
  color: var(--text-mute);
}

/* ── Delivery Status Column ── */
.delivery-status {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}
.delivery-channel {
  font-size: 0.7rem;
  text-transform: uppercase;
  color: var(--text-mute);
  font-weight: 800;
}
.delivery-msg {
  font-size: 0.8rem;
  color: var(--text-main);
  max-width: 150px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ── Rules grid ── */
.rules-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 1rem;
}

/* ── Rule card ── */
.rule-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  padding: 1.1rem 1.15rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  transition: all 0.3s cubic-bezier(0.23, 1, 0.32, 1);
  position: relative;
  overflow: hidden;
}
.rule-card::before {
  content: '';
  position: absolute;
  top: 0; left: 0; right: 0;
  height: 2px;
  background: linear-gradient(90deg, var(--accent), transparent);
  opacity: 0;
  transition: opacity 0.25s;
}
.rule-card:hover { transform: translateY(-3px); border-color: var(--border-active); box-shadow: 0 16px 32px -12px var(--shadow); }
.rule-card:hover::before { opacity: 1; }
.rule-card.disabled { opacity: 0.5; filter: grayscale(0.4); }

/* card header */
.rule-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.rule-name-group {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  min-width: 0;
}
.rule-channel-icon {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: var(--accent-soft);
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.rule-channel-icon.generic_webhook { background: rgba(var(--warning-rgb), 0.1); color: var(--warning); }
.rule-name { display: block; font-size: 0.9rem; font-weight: 800; color: var(--text-main); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 160px; }
.rule-channel-label { display: block; font-size: 0.68rem; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: 0.05em; margin-top: 1px; }

/* criteria chips */
.rule-criteria {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}
.crit-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.68rem;
  font-weight: 700;
  padding: 0.25rem 0.55rem;
  border-radius: 7px;
  max-width: 100%;
}
.crit-chip code {
  font-family: var(--font-mono);
  font-size: 0.65rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 140px;
}
.crit-chip.accent  { background: var(--accent-soft); color: var(--accent); }
.crit-chip.event   { background: rgba(var(--warning-rgb), 0.1); color: var(--warning); }
.crit-chip.log     { background: rgba(var(--success-rgb), 0.1); color: var(--success); }

/* card footer */
.rule-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: auto;
  padding-top: 0.5rem;
  border-top: 1px solid var(--border-subtle);
}
.cooldown-badge {
  display: flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.7rem;
  font-weight: 700;
  color: var(--text-mute);
}
.card-actions { display: flex; gap: 0.4rem; }

/* ── Toggle switch ── */
.toggle-switch {
  width: 40px;
  height: 22px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 99px;
  padding: 2px;
  cursor: pointer;
  transition: background 0.25s, border-color 0.25s;
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
.toggle-switch.on { background: var(--accent); border-color: var(--accent); }
.toggle-thumb {
  width: 16px;
  height: 16px;
  background: var(--text-mute);
  border-radius: 50%;
  transition: transform 0.25s cubic-bezier(0.23, 1, 0.32, 1), background 0.25s;
}
.toggle-switch.on .toggle-thumb { transform: translateX(18px); background: #fff; }

/* ── History table ── */
.search-box {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.7rem 1.25rem;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 14px;
}
.search-box input {
  background: none;
  border: none;
  outline: none;
  color: var(--text-main);
  font-size: 0.9rem;
  font-weight: 600;
  width: 100%;
}
.premium-table-container { min-height: 400px; }
.audit-row:hover { background: rgba(var(--accent-rgb), 0.02); }
.date-part { display: block; font-size: 0.7rem; font-weight: 800; color: var(--text-mute); text-transform: uppercase; }
.time-part { display: block; font-size: 0.82rem; font-weight: 700; font-family: var(--font-mono); color: var(--text-main); }
.rule-ref { font-size: 0.8rem; font-weight: 700; color: var(--text-dim); }
.resource-code { font-family: var(--font-mono); font-size: 0.75rem; background: var(--bg-input); padding: 0.2rem 0.4rem; border-radius: 4px; color: var(--accent); }
.message-cell { max-width: 240px; }
.truncate-msg { font-size: 0.75rem; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin: 0; }

.alert-type-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.7rem;
  font-weight: 800;
  padding: 0.25rem 0.6rem;
  border-radius: 7px;
  white-space: nowrap;
}
.alert-type-badge.event { background: rgba(var(--error-rgb), 0.1); color: var(--error); }
.alert-type-badge.log   { background: rgba(var(--warning-rgb), 0.1); color: var(--warning); }
.type-dot { width: 5px; height: 5px; border-radius: 50%; background: currentColor; }

/* ── Modal ── */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.75);
  backdrop-filter: blur(10px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 5000;
  padding: 1.5rem;
}
.modal-card {
  width: 100%;
  max-width: 540px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 24px;
  overflow: hidden;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}
.wide-modal { max-width: 600px; }
.modal-card-header {
  padding: 1.4rem 1.75rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.header-content { display: flex; gap: 1rem; align-items: center; }
.header-icon {
  width: 42px;
  height: 42px;
  background: var(--accent-soft);
  color: var(--accent);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.header-icon.warning { background: rgba(var(--warning-rgb), 0.12); color: var(--warning); }
.header-icon.error   { background: rgba(var(--error-rgb), 0.12);   color: var(--error);   }
.header-icon.accent  { background: var(--accent-soft); color: var(--accent); }
.header-icon svg { width: 22px; height: 22px; }
.modal-title   { font-size: 1.05rem; font-weight: 800; color: var(--text-main); margin: 0; }
.modal-subtitle { font-size: 0.78rem; color: var(--text-mute); margin: 0; }
.close-btn { background: none; border: none; color: var(--text-mute); font-size: 1.5rem; cursor: pointer; line-height: 1; }

.modal-card-body {
  padding: 1.5rem 1.75rem;
  display: flex;
  flex-direction: column;
  gap: 1.1rem;
  overflow-y: auto;
}
.modal-card-footer {
  padding: 1.25rem 1.75rem;
  border-top: 1px solid var(--border);
  display: flex;
  gap: 0.75rem;
  justify-content: flex-end;
  flex-shrink: 0;
}

/* Form elements */
.input-group { display: flex; flex-direction: column; gap: 0.45rem; }
.label-caps {
  font-size: 0.7rem;
  font-weight: 800;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.label-hint { text-transform: none; font-weight: 500; color: var(--text-mute); margin-left: 0.3em; }
.req { color: var(--error); }
.premium-input {
  width: 100%;
  background: var(--bg-input);
  border: 1.5px solid var(--border);
  border-radius: 12px;
  padding: 0.75rem 1rem;
  color: var(--text-main);
  font-family: inherit;
  font-size: 0.88rem;
  font-weight: 600;
  transition: border-color 0.2s, box-shadow 0.2s;
  outline: none;
}
.premium-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.12); }
.premium-input.mono { font-family: var(--font-mono); font-size: 0.82rem; }
select.premium-input { cursor: pointer; }

.form-grid.dual { display: grid; grid-template-columns: 1fr 1fr; gap: 0.85rem; }

.checkbox-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.check-pill {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.45rem 0.85rem;
  border-radius: 10px;
  background: var(--bg-input);
  border: 1.5px solid var(--border);
  font-size: 0.8rem;
  font-weight: 700;
  color: var(--text-dim);
  cursor: pointer;
  transition: all 0.2s;
  user-select: none;
}
.check-pill.active { background: var(--accent-soft); color: var(--accent); border-color: rgba(var(--accent-rgb), 0.35); }
.check-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  border: 2px solid var(--border);
  transition: background 0.2s, border-color 0.2s;
}
.check-dot.on { background: var(--accent); border-color: var(--accent); }

.enabled-row { flex-direction: row; align-items: center; justify-content: space-between; padding: 0.65rem 0; border-top: 1px solid var(--border-subtle); }

.form-error {
  background: rgba(var(--error-rgb), 0.08);
  color: var(--error);
  border: 1px solid rgba(var(--error-rgb), 0.2);
  border-radius: var(--radius-md);
  padding: 0.7rem 1rem;
  font-size: 0.82rem;
  font-weight: 600;
}

/* Buttons */
.btn-primary {
  display: inline-flex; align-items: center; gap: 0.45rem;
  background: var(--accent); color: white; border: none;
  padding: 0.75rem 1.5rem; border-radius: 12px; font-weight: 700; font-size: 0.88rem;
  cursor: pointer; transition: all 0.2s;
}
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); transform: translateY(-1px); }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-secondary {
  background: var(--bg-subtle); color: var(--text-main); border: 1px solid var(--border);
  padding: 0.75rem 1.5rem; border-radius: 12px; font-weight: 700; font-size: 0.88rem;
  cursor: pointer; transition: all 0.2s;
}
.btn-secondary:hover { border-color: var(--text-mute); }

.btn-danger {
  background: rgba(var(--error-rgb), 0.12); color: var(--error); border: 1px solid rgba(var(--error-rgb), 0.25);
  padding: 0.75rem 1.5rem; border-radius: 12px; font-weight: 700; font-size: 0.88rem;
  cursor: pointer; transition: all 0.2s;
}
.btn-danger:hover:not(:disabled) { background: var(--error); color: #fff; }
.btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }

/* ── Empty state ── */
.empty-state-wrapper { display: flex; align-items: center; justify-content: center; min-height: 320px; }
.empty-state-content { text-align: center; display: flex; flex-direction: column; align-items: center; }
.empty-icon-box {
  width: 68px; height: 68px;
  background: var(--accent-soft);
  border: 1px solid rgba(var(--accent-rgb), 0.15);
  border-radius: 18px;
  display: flex; align-items: center; justify-content: center;
  color: var(--accent); margin-bottom: 1rem;
}
.empty-icon-box svg { width: 28px; height: 28px; }
.empty-title { font-size: 1.15rem; font-weight: 800; color: var(--text-main); margin-bottom: 0.4rem; }
.empty-text  { font-size: 0.85rem; color: var(--text-mute); max-width: 260px; line-height: 1.6; }
.mt-4 { margin-top: 1rem; }

/* ── Misc ── */
.rotating { animation: spin 1s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.modal-bounce-enter-active { animation: modal-in 0.35s cubic-bezier(0.34, 1.56, 0.64, 1); }
.modal-bounce-leave-active { animation: modal-in 0.2s ease reverse; }
@keyframes modal-in { from { opacity: 0; transform: scale(0.92); } to { opacity: 1; transform: scale(1); } }

@media (max-width: 640px) {
  .rules-grid { grid-template-columns: 1fr; }
  .form-grid.dual { grid-template-columns: 1fr; }
  .alerts-toolbar { flex-direction: column; align-items: stretch; }
  .toolbar-right { justify-content: flex-end; }
  .modal-card { max-height: 95vh; }
  .modal-card-header, .modal-card-body, .modal-card-footer { padding-left: 1.25rem; padding-right: 1.25rem; }
}
</style>
