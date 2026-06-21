<template>
  <div class="page-view admin-view animate-fade-in">

    <!-- ── Page Hero ─────────────────────────────────────────────────────────── -->
    <section class="page-hero">
      <div class="page-hero-body">
        <div class="page-hero-copy">
          <span class="page-hero-eyebrow">Administration</span>
          <h1>Control Center</h1>
          <p class="page-hero-sub">
            Manage user accounts, roles, notification rules, and audit events.
          </p>
        </div>

        <div class="page-hero-actions">
          <!-- Context-sensitive primary action -->
          <button
            v-if="activeSection === 'users'"
            @click="userManagerRef?.openCreateModal()"
            class="page-btn primary"
          >
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Add user
          </button>
          <button
            v-if="activeSection === 'alerts' && isAdmin"
            @click="openNewAlertModal"
            class="page-btn primary"
          >
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            New Alert Rule
          </button>
          <button
            v-if="activeSection === 'teams'"
            @click="teamManagerRef?.openCreateModal()"
            class="page-btn primary"
          >
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            Add Team
          </button>
        </div>
      </div>
      <div class="page-hero-mesh" aria-hidden="true"/>
    </section>

    <!-- ── Metrics row ────────────────────────────────────────────────────────── -->
    <section class="page-metrics animate-slide-up" style="display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 0.5rem;">
      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon success"><AppIcon name="checkCircle"/></div>
          <span class="badge badge-success">Active</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Status</span>
          <span class="stat-value">Online</span>
        </div>
      </div>

      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon"><AppIcon name="users"/></div>
          <span class="badge badge-dim">Staff</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Accounts</span>
          <span class="stat-value">{{ staffUsersCount }}</span>
        </div>
      </div>

      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon"><AppIcon name="users"/></div>
          <span class="badge badge-dim">Groups</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Teams</span>
          <span class="stat-value">{{ teamsCount }}</span>
        </div>
      </div>

      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon"><AppIcon name="shield"/></div>
          <span class="badge badge-dim">Audit</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Audit events</span>
          <span class="stat-value">{{ auditLogsCount || '—' }}</span>
        </div>
      </div>

      <div class="page-metric-card" :class="{ active: activeSection === 'alerts' }" style="cursor:pointer; padding: 1rem;" @click="activeSection = 'alerts'">
        <div class="stat-header">
          <div class="stat-icon warning">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
            </svg>
          </div>
          <span class="badge badge-warning">Rules</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Alerts</span>
          <span class="stat-value">{{ alertRulesCount ?? '—' }}</span>
        </div>
      </div>

      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon critical">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
              <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
            </svg>
          </div>
          <span class="badge badge-critical">History</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Triggered</span>
          <span class="stat-value">{{ alertsTriggeredCount ?? '—' }}</span>
        </div>
      </div>

      <div class="page-metric-card" style="padding: 1rem;">
        <div class="stat-header">
          <div class="stat-icon">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5">
              <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
            </svg>
          </div>
          <span class="badge badge-dim">Policy</span>
        </div>
        <div class="stat-content">
          <span class="stat-label">Retention</span>
          <span class="stat-value">{{ settings.metrics_retention_days || 30 }} Days</span>
        </div>
      </div>
    </section>

    <!-- ── Section tab bar ───────────────────────────────────────────────────── -->
    <div class="admin-tab-bar">
      <button
        class="admin-tab"
        :class="{ active: activeSection === 'audit' }"
        @click="activeSection = 'audit'"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
          <polyline points="14 2 14 8 20 8"/>
          <line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
        </svg>
        Audit Log
      </button>

      <!-- Teams tab -->
      <button
        class="admin-tab"
        :class="{ active: activeSection === 'teams' }"
        @click="activeSection = 'teams'"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
          <circle cx="9" cy="7" r="4"/>
          <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
          <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
        </svg>
        Teams
      </button>

      <button
        class="admin-tab"
        :class="{ active: activeSection === 'users' }"
        @click="activeSection = 'users'"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
          <circle cx="9" cy="7" r="4"/>
          <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
          <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
        </svg>
        Users
      </button>

      <!-- Alerts tab — admin-gated -->
      <button
        v-if="isAdmin"
        class="admin-tab alerts-tab"
        :class="{ active: activeSection === 'alerts' }"
        @click="activeSection = 'alerts'"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
        </svg>
        Alerts
      </button>

      <!-- Settings tab -->
      <button
        v-if="isAdmin"
        class="admin-tab"
        :class="{ active: activeSection === 'settings' }"
        @click="activeSection = 'settings'"
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <circle cx="12" cy="12" r="3"></circle>
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
        </svg>
        Settings
      </button>
    </div>

    <!-- ── Panels ─────────────────────────────────────────────────────────────── -->
    <section class="page-panel">

      <!-- Users panel -->
      <div v-show="activeSection === 'users'">
        <UserManager
          ref="userManagerRef"
          :token="token"
          embedded
          @update-count="handleStaffCountUpdate"
        />
      </div>

      <!-- Audit panel -->
      <div v-show="activeSection === 'audit'">
        <AuditManager
          :token="token"
          embedded
          @update-count="(n) => (auditLogsCount = n)"
        />
      </div>

      <!-- Teams panel -->
      <div v-show="activeSection === 'teams'">
        <TeamManager ref="teamManagerRef" :token="token" @update-count="(n) => (teamsCount = n)" />
      </div>

      <!-- Alerts panel — admin-gated render guard -->
      <div v-show="activeSection === 'alerts'">
        <div v-if="!isAdmin" class="access-denied">
          <svg viewBox="0 0 24 24" width="36" height="36" fill="none" stroke="currentColor" stroke-width="1.5">
            <circle cx="12" cy="12" r="10"/>
            <line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/>
          </svg>
          <h3>Admin access required</h3>
          <p>Only administrators can view or modify alert rules and webhook configurations.</p>
        </div>
        <AlertsManager
          v-else
          ref="alertsManagerRef"
          @update-count="(n) => (alertRulesCount = n)"
        />
      </div>

      <!-- Settings panel -->
      <div v-if="activeSection === 'settings'">
        <div v-if="!isAdmin" class="access-denied">
          <svg viewBox="0 0 24 24" width="36" height="36" fill="none" stroke="currentColor" stroke-width="1.5">
            <circle cx="12" cy="12" r="10"/>
            <line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/>
          </svg>
          <h3>Admin access required</h3>
          <p>Only administrators can modify system settings.</p>
        </div>
        <div v-else class="settings-panel">
          <div class="settings-grid">
            
            <!-- Data Retention Policy -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'retention'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <circle cx="12" cy="12" r="10"></circle>
                    <polyline points="12 6 12 12 16 14"></polyline>
                  </svg>
                </div>
                <div>
                  <h3>Data Retention Policy</h3>
                  <p class="card-desc">Configure metrics retention.</p>
                </div>
              </div>
            </div>

            <!-- SMTP Settings -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'smtp'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"></path>
                    <polyline points="22,6 12,13 2,6"></polyline>
                  </svg>
                </div>
                <div>
                  <h3>Email Delivery (SMTP)</h3>
                  <p class="card-desc">Outgoing email settings.</p>
                </div>
              </div>
            </div>

            <!-- Webhook Settings -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'webhook'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
                    <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
                  </svg>
                </div>
                <div>
                  <h3>Alert Destinations</h3>
                  <p class="card-desc">Configure destinations for alerts (Email, Slack, MS Teams, etc).</p>
                </div>
              </div>
            </div>

            <!-- Automated Cloud Backups -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'backup'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                    <polyline points="17 8 12 3 7 8"></polyline>
                    <line x1="12" y1="3" x2="12" y2="15"></line>
                  </svg>
                </div>
                <div>
                  <h3>Automated Backups</h3>
                  <p class="card-desc">Backups to S3, GCS, or Azure.</p>
                </div>
              </div>
            </div>

            <!-- Long-Term Storage Archival -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'archival'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon warning">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                    <polyline points="17 8 12 3 7 8"></polyline>
                    <line x1="12" y1="3" x2="12" y2="15"></line>
                  </svg>
                </div>
                <div>
                  <h3>Long-Term Archival</h3>
                  <p class="card-desc">Archive logs to cloud storage.</p>
                </div>
              </div>
            </div>

            <!-- Google OAuth Settings -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'oauth'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
                  </svg>
                </div>
                <div>
                  <h3>Single Sign-On (OAuth)</h3>
                  <p class="card-desc">Google OAuth login settings.</p>
                </div>
              </div>
            </div>

            <!-- Profile -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'profile'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
                    <circle cx="12" cy="7" r="4"></circle>
                  </svg>
                </div>
                <div>
                  <h3>Profile</h3>
                  <p class="card-desc">Your account identity.</p>
                </div>
              </div>
            </div>

            <!-- Security -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'security'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                    <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
                  </svg>
                </div>
                <div>
                  <h3>Security</h3>
                  <p class="card-desc">Update your password.</p>
                </div>
              </div>
            </div>

            <!-- Appearance -->
            <div class="settings-card shadow-lg interactive" @click="activeSettingsModal = 'appearance'">
              <div class="settings-card-header card-header borderless">
                <div class="card-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <circle cx="12" cy="12" r="3"></circle>
                    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
                  </svg>
                </div>
                <div>
                  <h3>Appearance</h3>
                  <p class="card-desc">{{ themeDescription }}</p>
                </div>
              </div>
            </div>

          </div>
        </div>
      </div>

    </section>

    <!-- ── Settings Modals ───────────────────────────────────────────────────────── -->
    <div v-if="activeSettingsModal" class="modal-overlay" @click.self="closeModal">
      <div class="modal-content animate-slide-up" style="max-width: 500px; width: 90%; max-height: 90vh; overflow-y: auto;">
        
        <!-- Data Retention -->
        <template v-if="activeSettingsModal === 'retention'">
          <div class="modal-header">
            <h3 class="modal-title">Data Retention Policy</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem;">
            <label style="margin-bottom: 0.5rem; display: block; font-weight: 600; color: var(--text-main);">Metrics Retention (Days)</label>
            <select v-model="settings.metrics_retention_days" class="premium-input">
              <option :value="7">7 Days</option>
              <option :value="14">14 Days</option>
              <option :value="30">30 Days</option>
              <option :value="60">60 Days</option>
              <option :value="90">90 Days</option>
            </select>
            <div class="settings-warning" style="margin-top: 1rem;">
              <AppIcon name="alert" :size="16"/>
              <p><strong>Warning:</strong> Higher retention periods will significantly increase the size of the <code>lighthouse.db</code> SQLite file and may impact dashboard performance.</p>
            </div>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- SMTP Settings -->
        <template v-else-if="activeSettingsModal === 'smtp'">
          <div class="modal-header">
            <h3 class="modal-title">Email Delivery (SMTP)</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div class="input-group">
              <label>SMTP Host</label>
              <input v-model="settings.smtp_host" type="text" class="premium-input" placeholder="smtp.gmail.com" />
            </div>
            <div class="input-group">
              <label>SMTP Port</label>
              <input v-model="settings.smtp_port" type="number" class="premium-input" placeholder="587" />
            </div>
            <div class="input-group">
              <label>SMTP Username</label>
              <input v-model="settings.smtp_user" type="text" class="premium-input" placeholder="hello@example.com" />
            </div>
            <div class="input-group">
              <label>SMTP Password</label>
              <input v-model="settings.smtp_pass" type="password" class="premium-input" placeholder="••••••••" />
            </div>
            
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- Webhook Settings -->
        <template v-else-if="activeSettingsModal === 'webhook'">
          <div class="modal-header">
            <h3 class="modal-title">Alert Destinations</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div class="input-group">
              <label>Default Alert Destination Email</label>
              <input v-model="settings.alerts_email_address" type="email" class="premium-input" placeholder="alerts@yourteam.com" />
            </div>
            <div class="input-group">
              <label>Slack / Discord Webhook URL</label>
              <input v-model="settings.slack_webhook_url" type="text" class="premium-input" placeholder="https://hooks.slack.com/services/..." />
            </div>
            <div class="input-group">
              <label>Microsoft Teams Webhook URL</label>
              <input v-model="settings.msteams_webhook_url" type="text" class="premium-input" placeholder="https://<tenant>.webhook.office.com/..." />
            </div>
            <div class="input-group">
              <label>Google Chat Webhook URL</label>
              <input v-model="settings.gchat_webhook_url" type="text" class="premium-input" placeholder="https://chat.googleapis.com/v1/spaces/..." />
            </div>
            <div class="input-group">
              <label>Generic Webhook URL (JSON POST)</label>
              <input v-model="settings.generic_webhook_url" type="text" class="premium-input" placeholder="https://api.yourdomain.com/webhooks" />
            </div>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- Automated Backups -->
        <template v-else-if="activeSettingsModal === 'backup'">
          <div class="modal-header">
            <h3 class="modal-title">Automated Cloud Backups</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div style="background: var(--bg-subtle); border: 1px solid var(--border); border-radius: var(--radius-md); padding: 1.25rem; display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.5rem;">
              <div style="display: flex; flex-direction: column; gap: 0.25rem;">
                <span style="font-size: 0.95rem; font-weight: 800; color: var(--text-main);">Enable Automated Backups</span>
                <span style="font-size: 0.8rem; color: var(--text-dim);">Automatically backup your Lighthouse configuration and data to cloud storage.</span>
              </div>
              <label style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer; margin: 0;">
                <input type="checkbox" v-model="settings.backup_enabled" style="transform: scale(1.3); accent-color: var(--accent); cursor: pointer;" />
              </label>
            </div>
            
            <template v-if="settings.backup_enabled">
              <div class="input-group">
                <label>Storage Provider</label>
                <select v-model="settings.backup_provider" class="premium-input">
                  <option value="s3">AWS S3 / MinIO</option>
                  <option value="gcs">Google Cloud Storage</option>
                  <option value="azure">Azure Blob Storage</option>
                </select>
              </div>
              
              <div class="input-group">
                <label>Backup Cron Schedule</label>
                <input v-model="settings.backup_cron" type="text" class="premium-input" placeholder="0 0 * * * (Daily at midnight)" />
              </div>

              <template v-if="settings.backup_provider === 's3'">
                <div class="input-group">
                  <label>Endpoint</label>
                  <input v-model="settings.backup_endpoint" type="text" class="premium-input" placeholder="s3.amazonaws.com" />
                </div>
                <div class="input-group">
                  <label>Region</label>
                  <input v-model="settings.backup_region" type="text" class="premium-input" placeholder="us-east-1" />
                </div>
                <div class="input-group">
                  <label>Bucket Name</label>
                  <input v-model="settings.backup_bucket" type="text" class="premium-input" placeholder="my-backups" />
                </div>
                <div class="input-group">
                  <label>Access Key</label>
                  <input v-model="settings.backup_auth1" type="text" class="premium-input" placeholder="AKIA..." />
                </div>
                <div class="input-group">
                  <label>Secret Key</label>
                  <input v-model="settings.backup_auth2" type="password" class="premium-input" placeholder="••••••••" />
                </div>
              </template>

              <template v-else-if="settings.backup_provider === 'gcs'">
                <div class="input-group">
                  <label>Bucket Name</label>
                  <input v-model="settings.backup_bucket" type="text" class="premium-input" placeholder="my-gcs-bucket" />
                </div>
                <div class="input-group">
                  <label>Service Account Key (JSON)</label>
                  <textarea v-model="settings.backup_auth1" class="premium-input mono" rows="5" placeholder='{"type": "service_account", ...}'></textarea>
                </div>
              </template>

              <template v-else-if="settings.backup_provider === 'azure'">
                <div class="input-group">
                  <label>Container Name</label>
                  <input v-model="settings.backup_bucket" type="text" class="premium-input" placeholder="my-container" />
                </div>
                <div class="input-group">
                  <label>Account Name</label>
                  <input v-model="settings.backup_auth1" type="text" class="premium-input" placeholder="my-storage-account" />
                </div>
                <div class="input-group">
                  <label>Account Key</label>
                  <input v-model="settings.backup_auth2" type="password" class="premium-input" placeholder="••••••••" />
                </div>
              </template>
            </template>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button v-if="settings.backup_enabled" @click="testBackup" class="modal-btn cancel" :disabled="backupTesting || settingsSaving">
              {{ backupTesting ? 'Testing...' : 'Test' }}
            </button>
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving || backupTesting">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- Archival -->
        <template v-else-if="activeSettingsModal === 'archival'">
          <div class="modal-header">
            <h3 class="modal-title">Long-Term Archival</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div style="background: var(--bg-subtle); border: 1px solid var(--border); border-radius: var(--radius-md); padding: 1.25rem; display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.5rem;">
              <div style="display: flex; flex-direction: column; gap: 0.25rem;">
                <span style="font-size: 0.95rem; font-weight: 800; color: var(--text-main);">Enable Long-Term Archival</span>
                <span style="font-size: 0.8rem; color: var(--text-dim);">Archive historical metrics and logs to cold storage to save space.</span>
              </div>
              <label style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer; margin: 0;">
                <input type="checkbox" v-model="settings.archival_enabled" style="transform: scale(1.3); accent-color: var(--accent); cursor: pointer;" />
              </label>
            </div>

            <template v-if="settings.archival_enabled">
              <div class="form-check-group" style="display: flex; gap: 1rem;">
                <label class="form-check-label">
                  <input type="checkbox" v-model="settings.archive_metrics" class="premium-checkbox" />
                  <span>Archive Metrics</span>
                </label>
                <label class="form-check-label">
                  <input type="checkbox" v-model="settings.archive_logs" class="premium-checkbox" />
                  <span>Archive Logs</span>
                </label>
              </div>

              <div class="input-group">
                <label>Storage Provider</label>
                <select v-model="settings.archival_provider" class="premium-input">
                  <option value="s3">Amazon S3 (or compatible)</option>
                  <option value="gcs">Google Cloud Storage</option>
                  <option value="azure">Azure Blob Storage</option>
                </select>
              </div>

              <div class="input-group">
                <label>Archival Cron Schedule</label>
                <input v-model="settings.archival_cron" type="text" class="premium-input mono" placeholder="0 * * * *" />
              </div>

              <template v-if="settings.archival_provider === 's3'">
                <div class="input-group">
                  <label>Bucket Name</label>
                  <input v-model="settings.archival_bucket" type="text" class="premium-input" placeholder="my-archival-bucket" />
                </div>
                <div class="input-group">
                  <label>Region</label>
                  <input v-model="settings.archival_region" type="text" class="premium-input" placeholder="us-east-1" />
                </div>
                <div class="input-group">
                  <label>Endpoint (Optional)</label>
                  <input v-model="settings.archival_endpoint" type="text" class="premium-input" placeholder="https://..." />
                </div>
                <div class="input-group">
                  <label>Access Key</label>
                  <input v-model="settings.archival_auth1" type="text" class="premium-input" placeholder="AKIA..." />
                </div>
                <div class="input-group">
                  <label>Secret Key</label>
                  <input v-model="settings.archival_auth2" type="password" class="premium-input" placeholder="••••••••" />
                </div>
              </template>

              <template v-else-if="settings.archival_provider === 'gcs'">
                <div class="input-group">
                  <label>Bucket Name</label>
                  <input v-model="settings.archival_bucket" type="text" class="premium-input" placeholder="my-archival-bucket" />
                </div>
                <div class="input-group">
                  <label>Service Account Key (JSON)</label>
                  <textarea v-model="settings.archival_auth1" class="premium-input mono" rows="5" placeholder='{"type": "service_account", ...}'></textarea>
                </div>
              </template>

              <template v-else-if="settings.archival_provider === 'azure'">
                <div class="input-group">
                  <label>Container Name</label>
                  <input v-model="settings.archival_bucket" type="text" class="premium-input" placeholder="my-archival-container" />
                </div>
                <div class="input-group">
                  <label>Account Name</label>
                  <input v-model="settings.archival_auth1" type="text" class="premium-input" placeholder="my-storage-account" />
                </div>
                <div class="input-group">
                  <label>Account Key</label>
                  <input v-model="settings.archival_auth2" type="password" class="premium-input" placeholder="••••••••" />
                </div>
              </template>
            </template>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button v-if="settings.archival_enabled" @click="testArchival" class="modal-btn cancel" :disabled="archivalTesting || settingsSaving">
              {{ archivalTesting ? 'Testing...' : 'Test' }}
            </button>
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving || archivalTesting">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- Google OAuth -->
        <template v-else-if="activeSettingsModal === 'oauth'">
          <div class="modal-header">
            <h3 class="modal-title">Single Sign-On (Google OAuth)</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div class="input-group">
              <label>Client ID</label>
              <input v-model="settings.google_client_id" type="text" class="premium-input" placeholder="xxxx.apps.googleusercontent.com" />
            </div>
            <div class="input-group">
              <label>Client Secret</label>
              <input v-model="settings.google_client_secret" type="password" class="premium-input" placeholder="••••••••" />
            </div>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn cancel">Cancel</button>
            <button @click="saveSettingsAndClose" class="modal-btn confirm" :disabled="settingsSaving">
              {{ settingsSaving ? 'Saving...' : 'Save Settings' }}
            </button>
          </div>
        </template>

        <!-- Profile -->
        <template v-else-if="activeSettingsModal === 'profile'">
          <div class="modal-header">
            <h3 class="modal-title">Profile Info</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem; display: flex; flex-direction: column; gap: 1rem;">
            <div class="info-row">
              <label>Username</label>
              <div class="value-box">{{ sharedState.currentUser?.username }}</div>
            </div>
            <div class="info-row">
              <label>Role</label>
              <div class="value-box">
                <span :class="['badge', sharedState.currentUser?.is_admin ? 'badge-warning' : 'badge-dim']">
                  {{ sharedState.currentUser?.is_admin ? "Administrator" : "Staff member" }}
                </span>
              </div>
            </div>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn confirm">Done</button>
          </div>
        </template>

        <!-- Security -->
        <template v-else-if="activeSettingsModal === 'security'">
          <div class="modal-header">
            <h3 class="modal-title">Update Password</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem;">
            <form @submit.prevent="handlePasswordUpdate" class="settings-form" style="display: flex; flex-direction: column; gap: 1rem;">
              <div class="input-group">
                <label>Current password</label>
                <input type="password" v-model="currentPassword" placeholder="Enter current password" class="premium-input" required />
              </div>
              <div class="input-group">
                <label>New password</label>
                <input type="password" v-model="newPassword" placeholder="At least 8 characters" class="premium-input" required />
              </div>
              <div class="input-group">
                <label>Confirm password</label>
                <input type="password" v-model="confirmPassword" placeholder="Confirm new password" class="premium-input" required />
              </div>
              <p v-if="error" class="error-msg">{{ error }}</p>
              
              <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding-top: 1rem; border-top: none;">
                <button type="button" @click="closeModal" class="modal-btn cancel">Cancel</button>
                <button type="submit" class="modal-btn confirm" :disabled="loading">
                  {{ loading ? 'Updating...' : 'Update Password' }}
                </button>
              </div>
            </form>
          </div>
        </template>

        <!-- Appearance -->
        <template v-else-if="activeSettingsModal === 'appearance'">
          <div class="modal-header">
            <h3 class="modal-title">Appearance Settings</h3>
            <button class="modal-close" @click="closeModal"><AppIcon name="close" :size="20"/></button>
          </div>
          <div class="modal-body" style="padding-top: 1rem;">
            <p class="card-desc" style="margin-bottom: 1rem;">{{ themeDescription }}</p>
            <div class="theme-options">
              <button
                v-for="option in themeOptions"
                :key="option.value"
                type="button"
                :class="['page-filter-pill', { active: sharedState.themePreference === option.value }]"
                @click="applyTheme(option.value)"
              >
                {{ option.label }}
              </button>
            </div>
          </div>
          <div class="modal-footer" style="display: flex; gap: 0.5rem; justify-content: flex-end; padding: 1rem;">
            <button @click="closeModal" class="modal-btn confirm">Done</button>
          </div>
        </template>
        
      </div>
    </div>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { apiFetch } from '../utils/apiFetch';
import AppIcon        from '../components/AppIcon.vue';
import UserManager from '../components/UserManager.vue';
import TeamManager from '../components/TeamManager.vue';
import AuditManager from '../components/AuditManager.vue';
import AlertsManager  from '../components/AlertsManager.vue';
import { secureStorage } from '../utils/storage';
import { sharedState, showToast, applyTheme }   from '../utils/sharedState';

// ── Refs ──────────────────────────────────────────────────────────────────────
const userManagerRef   = ref(null);
const auditManagerRef  = ref(null);
const alertsManagerRef = ref(null);
const teamManagerRef   = ref(null);
const loading          = ref(false);
const settingsSaving   = ref(false);
const archivalTesting  = ref(false);

const activeSettingsModal = ref(null);
const closeModal = () => {
  activeSettingsModal.value = null;
};
const saveSettingsAndClose = async () => {
  await saveSettings();
  closeModal();
};

const activeSection   = ref('audit');
const staffUsersCount = ref(0);
const auditLogsCount  = ref(0);
const alertRulesCount = ref(null);
const teamsCount = ref(0);
const alertsTriggeredCount = ref(0);
const currentUser = ref({});
const settings = ref({
  metrics_retention_days: 30,
  smtp_host: "",
  smtp_port: 587,
  smtp_user: "",
  smtp_pass: "",
  alerts_email_address: "",
  google_client_id: "",
  google_client_secret: "",
  webhook_type: "generic_webhook",
  webhook_url: "",
  backup_enabled: false,
  backup_provider: "s3",
  backup_cron: "0 0 * * *",
  backup_bucket: "",
  backup_region: "",
  backup_endpoint: "",
  backup_auth1: "",
  backup_auth2: "",
});

const backupTesting = ref(false);
const token = secureStorage.getItem('token');

// ── Permission guard ──────────────────────────────────────────────────────────
const isAdmin = computed(() => sharedState.currentUser?.is_admin === true);

onMounted(async () => {
  const fetchAlertRulesCount = async () => {
    try {
      const currentToken = secureStorage.getItem('token');
      if (!currentToken) return;
      const res = await apiFetch('/api/admin/alerts/rules', {
        headers: { Authorization: `Bearer ${currentToken}` }
      });
      if (res.ok) {
        const rules = await res.json();
        alertRulesCount.value = rules ? rules.length : 0;
      } else {
        alertRulesCount.value = 0;
      }
    } catch (e) {
      console.error('Failed to fetch alert rules count:', e);
    }
  };
  fetchAlertRulesCount();

  const fetchAlertsTriggeredCount = async () => {
    try {
      const currentToken = secureStorage.getItem('token');
      if (!currentToken) return;
      const res = await apiFetch('/api/admin/alerts/history?limit=1000', {
        headers: { Authorization: `Bearer ${currentToken}` }
      });
      if (res.ok) {
        const history = await res.json();
        alertsTriggeredCount.value = history ? history.length : 0;
      } else {
        alertsTriggeredCount.value = 0;
      }
    } catch (e) {
      console.error('Failed to fetch alerts triggered count:', e);
    }
  };
  fetchAlertsTriggeredCount();

  const fetchSettings = async () => {
    try {
      const token = secureStorage.getItem('token');
      if (!token) return;
      const res = await apiFetch('/api/admin/settings', {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        settings.value = data;
      } else {
        console.error('Failed to fetch settings');
      }
    } catch (e) {
      console.error('Failed to fetch settings:', e);
    }
  };
  fetchSettings();
});

// ── Handlers ──────────────────────────────────────────────────────────────────
const handleStaffCountUpdate = (count) => { staffUsersCount.value = count; };

const saveSettings = async () => {
  if (settingsSaving.value) return;
  settingsSaving.value = true;
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch('/api/admin/settings', {
      method: 'PUT',
      headers: { 
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(settings.value)
    });
    if (res.ok) {
      showToast('Settings saved', 'Configuration updated successfully', 'success');
    } else {
      showToast('Error', 'Failed to update settings', 'error');
    }
  } catch (e) {
    console.error(e);
    showToast('Error', 'Failed to update settings', 'error');
  } finally {
    settingsSaving.value = false;
  }
};

const testBackup = async () => {
  if (backupTesting.value) return;
  
  // Save settings first before testing
  await saveSettings();
  
  backupTesting.value = true;
  try {
    const token = secureStorage.getItem('token');
    const res = await apiFetch('/api/admin/settings/backup/test', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${token}` }
    });
    if (res.ok) {
      showToast('Success', 'Backup uploaded successfully!', 'success');
    } else {
      const data = await res.json();
      showToast('Error', data.error || 'Failed to upload backup', 'error');
    }
  } catch (e) {
    showToast('Error', 'Failed to trigger backup', 'error');
  } finally {
    backupTesting.value = false;
  }
};

const themeOptions = [
  { value: "auto", label: "Auto" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

const themeDescription = computed(() => {
  if (sharedState.themePreference === "auto") {
    return `Following system — currently ${sharedState.theme}`;
  }
  return `Using ${sharedState.themePreference} theme`;
});

const newPassword = ref("");
const confirmPassword = ref("");
const currentPassword = ref("");
const error = ref("");

const handlePasswordUpdate = async () => {
  if (newPassword.value !== confirmPassword.value) {
    error.value = "Passwords do not match";
    return;
  }
  if (newPassword.value.length < 8) {
    error.value = "Password must be at least 8 characters";
    return;
  }
  if (!currentPassword.value) {
    error.value = "Current password is required";
    return;
  }

  loading.value = true;
  error.value = "";

  try {
    const tokenStr = secureStorage.getItem("token");
    const formData = new FormData();
    formData.append("password", newPassword.value);
    formData.append("current_password", currentPassword.value);

    const res = await apiFetch("/api/user/change-password", {
      method: "POST",
      headers: { Authorization: `Bearer ${tokenStr}` },
      body: formData,
    });

    if (res.ok) {
      showToast("Success", "Password updated successfully", "success");
      newPassword.value = "";
      confirmPassword.value = "";
      currentPassword.value = "";
    } else {
      const data = await res.json();
      error.value = data.error || "Failed to update password";
    }
  } catch (err) {
    error.value = "System connection failed";
  } finally {
    loading.value = false;
  }
};
const testArchival = async () => {
  archivalTesting.value = true;
  try {
    const token = secureStorage.getItem("token");
    const res = await apiFetch("/api/admin/settings/archival/test", {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      alert("Archival configuration is valid! Successfully triggered an archival job.");
    } else {
      const err = await res.json();
      alert(`Archival test failed: ${err.error}`);
    }
  } catch (err) {
    alert(`Archival test failed: ${err.message}`);
  } finally {
    archivalTesting.value = false;
  }
};
</script>

<style scoped>
/* ── Tab bar ── */
.admin-tab-bar {
  display: flex;
  gap: 0.35rem;
  padding: 0.3rem;
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: 14px;
  width: fit-content;
}

.admin-tab {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.6rem 1.1rem;
  border-radius: 10px;
  font-weight: 800;
  font-size: 0.82rem;
  color: var(--text-mute);
  transition: all 0.2s;
  position: relative;
}
.admin-tab:hover { color: var(--text-main); background: var(--bg-card); }
.admin-tab.active { background: var(--accent); color: #fff; box-shadow: 0 4px 12px rgba(var(--accent-rgb), 0.28); }
.admin-tab.alerts-tab.active { background: linear-gradient(135deg, #f59e0b, #d97706); box-shadow: 0 4px 12px rgba(245, 158, 11, 0.3); }

/* "New" badge on the Alerts tab */
.new-badge {
  font-size: 0.58rem;
  font-weight: 900;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  background: var(--warning);
  color: #fff;
  padding: 0.08rem 0.38rem;
  border-radius: 99px;
  margin-left: 0.1rem;
}
.admin-tab.active .new-badge { background: rgba(255,255,255,0.25); }

/* ── Metric card active state ── */
.page-metric-card.active {
  border-color: rgba(245, 158, 11, 0.4);
  box-shadow: 0 0 0 3px rgba(245, 158, 11, 0.08);
}

/* ── Access denied state ── */
.access-denied {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 4rem 2rem;
  text-align: center;
  gap: 0.75rem;
  color: var(--text-mute);
}
.access-denied svg { color: var(--error); opacity: 0.6; margin-bottom: 0.5rem; }
.access-denied h3 { font-size: 1.15rem; font-weight: 800; color: var(--text-main); }
.access-denied p { font-size: 0.88rem; color: var(--text-mute); max-width: 340px; line-height: 1.6; }

/* ── Settings specific styles ── */
.settings-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 1.5rem;
}

.settings-card {
  padding: 1.25rem;
  border-radius: var(--radius-xl);
  border: 1px solid var(--border);
  background: var(--bg-card);
  display: flex;
  flex-direction: column;
  gap: 1rem;
  transition: all 0.2s ease;
}

.settings-card.interactive {
  cursor: pointer;
}

.settings-card.interactive:hover {
  transform: translateY(-3px);
  border-color: var(--accent);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.08);
}

.card-header.borderless {
  border-bottom: none;
  padding-bottom: 0;
  margin-bottom: 0;
}

.card-header {
  display: flex;
  align-items: flex-start;
  gap: 0.85rem;
}

.card-icon {
  width: 40px;
  height: 40px;
  border-radius: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--accent-soft);
  color: var(--accent);
  border: 1px solid rgba(var(--accent-rgb), 0.12);
  flex-shrink: 0;
}

.card-icon svg { width: 18px; height: 18px; }

.card-header h3 {
  margin: 0;
  font-size: 1rem;
  font-weight: 800;
  color: var(--text-main);
}

.card-desc {
  margin: 0.15rem 0 0;
  font-size: 0.8rem;
  color: var(--text-mute);
}

.card-body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.info-row {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.info-row label {
  font-size: 0.68rem;
  font-weight: 800;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.value-box {
  padding: 0.85rem 1rem;
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-main);
  font-weight: 600;
}

.settings-form {
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}

.input-group label {
  display: block;
  font-size: 0.68rem;
  font-weight: 800;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 0.4rem;
}

.full-width {
  width: 100%;
  justify-content: center;
  margin-top: 0.25rem;
}

.theme-options {
  display: flex;
  gap: 0.35rem;
  padding: 0.25rem;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.theme-options .page-filter-pill {
  flex: 1;
  justify-content: center;
}
</style>
