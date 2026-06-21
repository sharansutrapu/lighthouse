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

      <!-- Rules Table -->
      <div v-else class="premium-table-container">
        <table class="premium-table admin-table">
          <thead>
            <tr>
              <th>Rule Name</th>
              <th>Criteria Tags</th>
              <th>Cooldown</th>
              <th>Status</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in rules" :key="rule.id" :class="{ 'disabled-row': !rule.enabled }">
              <td data-label="Rule Name">
                <div class="user-cell">
                  <span class="user-name" style="font-weight: 800; font-size: 0.95rem;">{{ rule.name }}</span>
                </div>
              </td>
              <td data-label="Criteria Tags">
                <div class="rule-criteria" style="margin-top: 0;">
                  <div v-if="rule.container_pattern" class="crit-chip accent">
                    <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
                      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                    </svg>
                    <code>{{ rule.container_pattern }}</code>
                  </div>
                  <template v-if="rule.event_types">
                    <span v-for="ev in splitEvents(rule.event_types)" :key="ev" class="crit-chip event">
                      {{ formatEventName(ev) }}
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
              </td>
              <td data-label="Cooldown">
                <span class="cooldown-badge" style="background: transparent; border: none; padding: 0;">
                  <svg viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" stroke-width="2.5">
                    <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
                  </svg>
                  {{ formatCooldown(rule.cooldown_seconds) }}
                </span>
              </td>
              <td data-label="Status">
                <button
                  class="toggle-switch"
                  style="margin: 0;"
                  :class="{ on: rule.enabled }"
                  @click="toggleRule(rule)"
                  :data-tooltip="rule.enabled ? 'Disable rule' : 'Enable rule'"
                  :aria-label="rule.enabled ? 'Disable rule' : 'Enable rule'"
                >
                  <span class="toggle-thumb"/>
                </button>
              </td>
              <td class="text-right" data-label="Actions">
                <div class="action-group justify-end">
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
              </td>
            </tr>
          </tbody>
        </table>
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
        <div v-if="showModal" class="modal-overlay" @mousedown.self="showModal = false">
          <div class="modal-card alert-editor glass shadow-2xl">
            <!-- Header -->
            <div class="modal-card-header">
              <div class="header-content">
                <div class="header-icon" :class="editingRule ? 'warning' : 'accent'">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
                  </svg>
                </div>
                <div class="header-copy">
                  <div class="header-title-row">
                    <h3 class="modal-title">{{ editingRule ? 'Edit Alert Rule' : 'New Alert Rule' }}</h3>
                  </div>
                  <p class="modal-subtitle">Configure notification criteria and delivery channel</p>
                </div>
              </div>
              
              <div class="header-toggle" style="margin-left: 1rem; align-self: center;">
                <label class="premium-toggle" :class="{ active: form.enabled }">
                  <span class="status-label">{{ form.enabled ? 'ON' : 'OFF' }}</span>
                  <div class="toggle-rail">
                    <div class="toggle-handle"></div>
                  </div>
                  <input type="checkbox" v-model="form.enabled" style="display: none" />
                </label>
              </div>
              <button class="close-btn" @click="closeModal" style="margin-left: 1rem;">×</button>
            </div>

            <!-- Editor Shell -->
            <div class="editor-shell">
              <!-- Left Sidebar Nav -->
              <nav class="editor-nav">
                <button
                  v-for="sec in editorSections"
                  :key="sec.id"
                  class="editor-nav-btn"
                  :class="{ active: activeSection === sec.id }"
                  @click.prevent="scrollToSection(sec.id)"
                >
                  <span class="nav-step">{{ sec.step }}</span>
                  <div class="nav-copy">
                    <strong>{{ sec.label }}</strong>
                    <span>{{ sec.hint }}</span>
                  </div>
                </button>
              </nav>

              <!-- Right Body -->
              <div class="editor-body" ref="editorBodyRef" @scroll.passive="syncActiveSection">
                <div v-if="formError" class="form-error">{{ formError }}</div>

                <!-- Section 1: Basics -->
                <section id="section-basics" class="editor-section">
                  <div class="section-head compact">
                    <div>
                      <h4>Rule Basics</h4>
                      <p>Name and container scope</p>
                    </div>
                  </div>
                  
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
                </section>

                <!-- Section 2: Trigger -->
                <section id="section-trigger" class="editor-section">
                  <div class="section-head compact">
                    <div>
                      <h4>Trigger Criteria</h4>
                      <p>Mix and match events, logs, and metrics</p>
                    </div>
                  </div>

                  <!-- Events -->
                  <div class="input-group">
                    <label class="label-caps">Container Lifecycle Triggers</label>
                    <div class="checkbox-row" style="display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.5rem; margin-bottom: 1rem;">
                      <label class="choice-chip" :class="{ 'info active': form.events.die }">
                        <input type="checkbox" v-model="form.events.die" style="display:none"/>
                        <strong>💀 Container Die</strong>
                        <span>Container crashes or stops</span>
                      </label>
                      <label class="choice-chip" :class="{ 'warning active': form.events.oom }">
                        <input type="checkbox" v-model="form.events.oom" style="display:none"/>
                        <strong>🔥 OOM Kill</strong>
                        <span>Out of memory kills</span>
                      </label>
                      <label class="choice-chip" :class="{ 'critical active': form.events.health_status }">
                        <input type="checkbox" v-model="form.events.health_status" style="display:none"/>
                        <strong>💔 Health Status</strong>
                        <span>Docker healthcheck fails</span>
                      </label>
                      <label class="choice-chip" :class="{ 'info active': form.events.restart }">
                        <input type="checkbox" v-model="form.events.restart" style="display:none"/>
                        <strong>🔄 Container Restart</strong>
                        <span>Container enters restart loop</span>
                      </label>
                      <label class="choice-chip" :class="{ 'warning active': form.events.kill }">
                        <input type="checkbox" v-model="form.events.kill" style="display:none"/>
                        <strong>🔪 Container Kill</strong>
                        <span>Container forcefully killed</span>
                      </label>
                      <label class="choice-chip" :class="{ 'info active': form.events.stop }">
                        <input type="checkbox" v-model="form.events.stop" style="display:none"/>
                        <strong>🛑 Container Stop</strong>
                        <span>Container stopped normally</span>
                      </label>
                    </div>

                    <label class="label-caps">System, Security & Delivery Triggers</label>
                    <div class="checkbox-row" style="display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.5rem;">
                      <label class="choice-chip" :class="{ 'info active': form.events.audit }">
                        <input type="checkbox" v-model="form.events.audit" style="display:none"/>
                        <strong>🛡️ System Audit</strong>
                        <span>Security & audit events</span>
                      </label>
                      <label class="choice-chip" :class="{ 'critical active': form.events.auth_failed }">
                        <input type="checkbox" v-model="form.events.auth_failed" style="display:none"/>
                        <strong>🚨 Auth Failed</strong>
                        <span>Multiple failed logins</span>
                      </label>
                      <label class="choice-chip" :class="{ 'warning active': form.events.vulnerability_found }">
                        <input type="checkbox" v-model="form.events.vulnerability_found" style="display:none"/>
                        <strong>🦠 Vulnerability Found</strong>
                        <span>Critical/High CVEs discovered</span>
                      </label>
                      <label class="choice-chip" :class="{ 'warning active': form.events.image_pull_error }">
                        <input type="checkbox" v-model="form.events.image_pull_error" style="display:none"/>
                        <strong>📥 Image Pull Error</strong>
                        <span>Image pull backoff</span>
                      </label>
                      <label class="choice-chip" :class="{ 'info active': form.events.gitops_success }">
                        <input type="checkbox" v-model="form.events.gitops_success" style="display:none"/>
                        <strong>🚀 GitOps Sync Success</strong>
                        <span>Deployment successful</span>
                      </label>
                      <label class="choice-chip" :class="{ 'critical active': form.events.gitops_failed }">
                        <input type="checkbox" v-model="form.events.gitops_failed" style="display:none"/>
                        <strong>❌ GitOps Sync Failed</strong>
                        <span>Deployment failed</span>
                      </label>
                      <label class="choice-chip" :class="{ 'critical active': form.events.deployment_failed }">
                        <input type="checkbox" v-model="form.events.deployment_failed" style="display:none"/>
                        <strong>💔 Deployment Failed</strong>
                        <span>General deployment failure</span>
                      </label>
                      <label class="choice-chip" :class="{ 'info active': form.events.backup_success }">
                        <input type="checkbox" v-model="form.events.backup_success" style="display:none"/>
                        <strong>💾 Backup Success</strong>
                        <span>Automated backup completed</span>
                      </label>
                      <label class="choice-chip" :class="{ 'critical active': form.events.backup_failed }">
                        <input type="checkbox" v-model="form.events.backup_failed" style="display:none"/>
                        <strong>⚠️ Backup Failed</strong>
                        <span>Automated backup failed</span>
                      </label>
                    </div>
                  </div>

                  <!-- Logs -->
                  <div class="input-group">
                    <label class="label-caps">
                      Log Keyword Regex
                      <span class="label-hint">(optional)</span>
                    </label>
                    <input
                      v-model="form.log_pattern"
                      type="text"
                      class="premium-input mono"
                      placeholder="(?i)error|exception|fatal|panic"
                    />
                  </div>

                  <!-- Metrics -->
                  <div class="form-grid dual">
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
                </section>

                <!-- Section 3: Destinations -->
                <section id="section-destinations" class="editor-section">
                  <div class="section-head compact">
                    <div>
                      <h4>Destinations</h4>
                      <p>Where should we deliver this alert?</p>
                    </div>
                  </div>
                  
                  <div class="events-group">
                    <label class="check-pill" :class="{ active: form.enable_slack }">
                      <input type="checkbox" v-model="form.enable_slack" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_slack }"/>
                      Slack
                    </label>
                    <label class="check-pill" :class="{ active: form.enable_msteams }">
                      <input type="checkbox" v-model="form.enable_msteams" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_msteams }"/>
                      MS Teams
                    </label>
                    <label class="check-pill" :class="{ active: form.enable_gchat }">
                      <input type="checkbox" v-model="form.enable_gchat" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_gchat }"/>
                      GChat
                    </label>
                    <label class="check-pill" :class="{ active: form.enable_generic_webhook }">
                      <input type="checkbox" v-model="form.enable_generic_webhook" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_generic_webhook }"/>
                      Generic
                    </label>
                    <label class="check-pill" :class="{ active: form.enable_email }">
                      <input type="checkbox" v-model="form.enable_email" style="display:none"/>
                      <span class="check-dot" :class="{ on: form.enable_email }"/>
                      Email
                    </label>
                  </div>

                  <template v-if="form.enable_email">
                    <div class="input-group" style="margin-top: 0.5rem;">
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
                </section>

                <!-- Section 4: Throttling -->
                <section id="section-throttle" class="editor-section">
                  <div class="section-head compact">
                    <div>
                      <h4>Throttling</h4>
                      <p>Prevent alert storms</p>
                    </div>
                  </div>
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
                </section>

              </div>
            </div>

            <!-- Footer -->
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

    <!-- ── View Details Modal ────────────────────────────────────────────────── -->
    <Teleport to="body">
      <Transition name="modal-bounce">
        <div v-if="showDetailsModal" class="modal-overlay" @mousedown.self="showDetailsModal = false">
          <div class="modal-card glass shadow-2xl" style="max-width:500px">
            <div class="modal-card-header">
              <div class="header-content">
                <div class="header-icon accent">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                    <circle cx="12" cy="12" r="3"></circle>
                  </svg>
                </div>
                <div>
                  <h3 class="modal-title">Alert Details</h3>
                  <p class="modal-subtitle">Full summary of the triggered alert</p>
                </div>
              </div>
              <button class="close-btn" @click="showDetailsModal = false">×</button>
            </div>
            <div class="modal-card-body" v-if="viewDetailsEntry">
              <div class="input-group">
                <label class="label-caps">Rule Name</label>
                <div class="premium-input">{{ viewDetailsEntry.rule_name || '—' }}</div>
              </div>
              <div class="input-group" style="margin-top: 1rem;">
                <label class="label-caps">Container</label>
                <div class="premium-input">{{ viewDetailsEntry.container_name }}</div>
              </div>
              <div class="input-group" style="margin-top: 1rem;">
                <label class="label-caps">Time</label>
                <div class="premium-input">{{ formatDate(viewDetailsEntry.timestamp) }} {{ formatTime(viewDetailsEntry.timestamp) }}</div>
              </div>
              <div class="input-group" style="margin-top: 1rem;">
                <label class="label-caps">Details</label>
                <textarea class="premium-input" style="height: 100px; resize: none;" readonly>{{ viewDetailsEntry.details }}</textarea>
              </div>
            </div>
            <div class="modal-card-footer">
              <button @click="showDetailsModal = false" class="btn-primary" style="margin-left: auto;">Close</button>
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

const emit = defineEmits(["update-count"]);

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

const activeSection = ref('basics');
const editorBodyRef = ref(null);
const editorSections = computed(() => [
  { id: 'basics', step: '1', label: 'Basics', hint: 'Name and scope' },
  { id: 'trigger', step: '2', label: 'Trigger', hint: 'What fires the alert' },
  { id: 'destinations', step: '3', label: 'Destinations', hint: 'Where to send' },
  { id: 'throttle', step: '4', label: 'Throttling', hint: 'Noise control' },
]);

function scrollToSection(id) {
  activeSection.value = id;
  const el = document.getElementById(`section-${id}`);
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
}

function syncActiveSection() {
  const container = editorBodyRef.value;
  if (!container) return;
  const offset = container.scrollTop + 120;
  const sections = editorSections.value;
  for (let i = sections.length - 1; i >= 0; i -= 1) {
    const section = sections[i];
    const el = document.getElementById(`section-${section.id}`);
    if (el && el.offsetTop <= offset) {
      activeSection.value = section.id;
      break;
    }
  }
}

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
  events: { 
    die: false, 
    oom: false, 
    health_status: false, 
    restart: false,
    kill: false,
    stop: false,
    audit: false,
    auth_failed: false,
    vulnerability_found: false,
    gitops_success: false,
    gitops_failed: false,
    backup_success: false,
    backup_failed: false,
    deployment_failed: false,
    image_pull_error: false
  },
  log_pattern: '',
  cooldown_seconds: 300,
  enable_slack: false,
  enable_msteams: false,
  enable_gchat: false,
  enable_generic_webhook: false,
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
    if (res.ok) {
      rules.value = await res.json();
      emit("update-count", rules.value.length);
    } else {
      showToast('Error', 'Failed to load alert rules', 'error');
    }
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
      restart: evList.includes('restart'),
      kill: evList.includes('kill'),
      stop: evList.includes('stop'),
      audit: evList.includes('audit'),
      auth_failed: evList.includes('auth_failed'),
      vulnerability_found: evList.includes('vulnerability_found'),
      gitops_success: evList.includes('gitops_success'),
      gitops_failed: evList.includes('gitops_failed'),
      backup_success: evList.includes('backup_success'),
      backup_failed: evList.includes('backup_failed'),
      deployment_failed: evList.includes('deployment_failed'),
      image_pull_error: evList.includes('image_pull_error'),
    },
    log_pattern: rule.log_pattern || '',
    cooldown_seconds: rule.cooldown_seconds ?? 300,
    enable_slack: !!rule.enable_slack,
    enable_msteams: !!rule.enable_msteams,
    enable_gchat: !!rule.enable_gchat,
    enable_generic_webhook: !!rule.enable_generic_webhook,
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
  if (!form.value.enable_slack && !form.value.enable_msteams && !form.value.enable_gchat && !form.value.enable_generic_webhook && !form.value.enable_email) {
    return 'Select at least one delivery method.';
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
    body.append('enable_slack',      String(form.value.enable_slack));
    body.append('enable_msteams',    String(form.value.enable_msteams));
    body.append('enable_gchat',      String(form.value.enable_gchat));
    body.append('enable_generic_webhook', String(form.value.enable_generic_webhook));
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
const formatEventName = (ev) => {
  const map = {
    'die': 'Container Die',
    'oom': 'OOM Kill',
    'health_status': 'Health Status',
    'restart': 'Container Restart',
    'kill': 'Container Kill',
    'stop': 'Container Stop',
    'audit': 'System Audit',
    'auth_failed': 'Auth Failed',
    'vulnerability_found': 'Vulnerability Found',
    'image_pull_error': 'Image Pull Error',
    'gitops_success': 'GitOps Sync Success',
    'gitops_failed': 'GitOps Sync Failed',
    'deployment_failed': 'Deployment Failed',
    'backup_success': 'Backup Success',
    'backup_failed': 'Backup Failed'
  };
  return map[ev] || ev;
};

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

.disabled-row {
  opacity: 0.5;
}

.disabled-row .user-name {
  text-decoration: line-through;
  color: var(--text-dim);
}

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

/* Editor modal */
.editor-overlay {
  padding: 1rem;
}

.alert-editor {
  width: min(920px, 100%);
  max-height: min(92vh, 960px);
  display: flex;
  flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 24px;
  overflow: hidden;
}

.modal-card-header {
  padding: 1.35rem 1.5rem;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.header-content {
  display: flex;
  gap: 0.85rem;
  align-items: flex-start;
  min-width: 0;
  flex: 1;
}

.header-copy {
  min-width: 0;
  flex: 1;
}

.header-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}

.header-icon {
  width: 44px;
  height: 44px;
  background: var(--accent-soft);
  color: var(--accent);
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.modal-title {
  margin: 0;
  font-size: 1.15rem;
  font-weight: 800;
  color: var(--text-main);
}

.modal-subtitle {
  margin: 0.2rem 0 0;
  font-size: 0.82rem;
  color: var(--text-mute);
  line-height: 1.45;
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

.close-btn:hover {
  color: var(--text-main);
  border-color: var(--border-active);
  background: var(--bg-subtle);
}

.editor-shell {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  min-height: 0;
  flex: 1 1 auto;
}

.editor-nav {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  padding: 1rem;
  border-right: 1px solid var(--border);
  background: var(--bg-subtle);
}

.editor-nav-btn {
  display: flex;
  align-items: flex-start;
  gap: 0.65rem;
  width: 100%;
  padding: 0.7rem 0.75rem;
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}

.editor-nav-btn:hover {
  background: var(--bg-card);
  border-color: var(--border);
}

.editor-nav-btn.active {
  background: var(--bg-card);
  border-color: rgba(var(--accent-rgb), 0.35);
  box-shadow: 0 8px 20px -14px var(--shadow);
}

.nav-step {
  width: 22px;
  height: 22px;
  border-radius: 999px;
  display: grid;
  place-items: center;
  font-size: 0.68rem;
  font-weight: 800;
  color: var(--text-mute);
  background: var(--bg-input);
  border: 1px solid var(--border);
  flex-shrink: 0;
}

.editor-nav-btn.active .nav-step {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}

.nav-copy {
  display: flex;
  flex-direction: column;
  gap: 0.1rem;
  min-width: 0;
}

.nav-copy strong {
  font-size: 0.8rem;
  font-weight: 800;
  color: var(--text-main);
}

.nav-copy span {
  font-size: 0.72rem;
  color: var(--text-mute);
  line-height: 1.35;
}

.editor-body {
  padding: 1.25rem 1.5rem 1.5rem;
  overflow-y: auto;
  min-height: 0;
  display: grid;
  gap: 1rem;
  align-content: start;
  scroll-behavior: smooth;
}

.editor-section {
  display: grid;
  gap: 0.9rem;
  padding: 1rem 1.05rem;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border);
  background: var(--bg-input);
  scroll-margin-top: 0.75rem;
}

.section-head h4 {
  margin: 0;
  font-size: 0.95rem;
  font-weight: 800;
  color: var(--text-main);
}

.section-head p {
  margin: 0.2rem 0 0;
  font-size: 0.8rem;
  color: var(--text-mute);
  line-height: 1.45;
}

.form-grid {
  display: grid;
  gap: 0.75rem;
}

.form-grid.dual {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.premium-input {
  width: 100%;
  padding: 0.75rem 0.95rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  color: var(--text-main);
  font-size: 0.86rem;
  font-weight: 600;
  transition: border-color 0.2s ease, box-shadow 0.2s ease, background 0.2s ease;
}

.premium-input:focus {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.12);
  background: var(--bg-subtle);
}

.premium-input.mono {
  font-family: var(--font-mono);
  font-size: 0.82rem;
}

.choice-chip {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  padding: 0.75rem 0.85rem;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: var(--bg-card);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.2s ease, background 0.2s ease, transform 0.2s ease;
}

.choice-chip strong {
  font-size: 0.82rem;
  font-weight: 800;
  color: var(--text-main);
}

.choice-chip span {
  font-size: 0.72rem;
  color: var(--text-mute);
}

.choice-chip.info.active {
  border-color: rgba(8, 145, 178, 0.45);
  background: rgba(8, 145, 178, 0.1);
}

.choice-chip.warning.active {
  border-color: rgba(245, 158, 11, 0.45);
  background: rgba(245, 158, 11, 0.1);
}

.choice-chip.critical.active {
  border-color: rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.1);
}

.premium-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.6rem;
  cursor: pointer;
  user-select: none;
}

.toggle-rail {
  width: 36px;
  height: 20px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 20px;
  position: relative;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  flex-shrink: 0;
}

.premium-toggle.active .toggle-rail {
  background: var(--success);
  border-color: var(--success);
  box-shadow: 0 0 12px rgba(16, 185, 129, 0.2);
}

.toggle-handle {
  width: 14px;
  height: 14px;
  background: #fff;
  border-radius: 50%;
  position: absolute;
  top: 2px;
  left: 2px;
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.2);
}

.premium-toggle.active .toggle-handle {
  transform: translateX(16px);
}

.status-label {
  font-size: 0.72rem;
  font-weight: 800;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.02em;
  min-width: 1.5rem;
}

.premium-toggle.active .status-label {
  color: var(--success);
}

</style>
