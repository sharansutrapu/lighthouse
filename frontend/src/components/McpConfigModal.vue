<template>
  <div v-if="modelValue" class="modal-overlay" @click.self="close">
    <div class="modal-content large" :class="sharedState.theme">
      <div class="modal-header">
        <div class="modal-title-group">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 22v-5"></path>
            <path d="M9 8V2"></path>
            <path d="M15 8V2"></path>
            <path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"></path>
          </svg>
          <h2>MCP & API Tokens</h2>
        </div>
        <button class="close-btn" @click="close">&times;</button>
      </div>

      <div class="modal-body">
        <p class="modal-description">
          Generate long-lived Personal Access Tokens (PATs) to connect your AI assistants, scripts, and MCP clients to Lighthouse securely.
        </p>

        <div class="token-generation-section">
          <h3>Generate New Token</h3>
          <div class="generate-form">
            <input 
              v-model="newTokenName" 
              type="text" 
              placeholder="e.g. Cursor IDE, Claude Desktop" 
              class="form-input"
              @keyup.enter="generateToken"
            />
            <button class="btn btn-primary" @click="generateToken" :disabled="!newTokenName.trim() || isGenerating">
              Generate
            </button>
          </div>
          
          <div v-if="newlyGeneratedToken" class="generated-token-alert success-alert">
            <p><strong>Success!</strong> Copy your token now. You won't be able to see it again.</p>
            <div class="token-display">
              <code>{{ newlyGeneratedToken }}</code>
              <button class="copy-btn" @click="copyToken(newlyGeneratedToken)" title="Copy">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                  <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                </svg>
              </button>
            </div>
            
            <div class="mcp-config-snippet">
              <p class="snippet-title"><strong>Client Configuration</strong></p>
              <p class="snippet-desc">Add this to your Claude Desktop or Cursor config:</p>
              <div class="code-block-wrapper">
                <pre><code>{{ getMcpConfig(newlyGeneratedToken) }}</code></pre>
                <button class="copy-btn snippet-copy" @click="copyToken(getMcpConfig(newlyGeneratedToken))" title="Copy Config">
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div class="tokens-list-section">
          <h3>Active Tokens</h3>
          <div v-if="isLoading" class="loading-state">Loading tokens...</div>
          <div v-else-if="tokens.length === 0" class="empty-state">
            No active tokens found.
          </div>
          <div v-else class="tokens-table-container">
            <table class="tokens-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Created</th>
                  <th>Last Used</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="token in tokens" :key="token.id">
                  <td><strong>{{ token.name }}</strong></td>
                  <td>{{ formatDate(token.created_at) }}</td>
                  <td>{{ formatDate(token.last_used) }}</td>
                  <td>
                    <button class="btn-icon btn-danger" @click="promptRevoke(token)" title="Revoke Token">
                      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                      </svg>
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
    
    <!-- Revoke Confirmation Modal -->
    <Teleport to="body">
      <Transition name="fade">
        <div v-if="tokenToRevoke" class="modal-overlay" style="z-index: 10001" @click.self="tokenToRevoke = null">
          <div class="modal-content shadow-2xl">
            <div class="modal-icon error">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="24" height="24">
                <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </div>
            <h3>Revoke Token</h3>
            <p>
              Are you sure you want to revoke the token <strong>{{ tokenToRevoke.name }}</strong>? Any applications using it will immediately lose access.
            </p>
            <div class="modal-actions" style="margin-top: 1.5rem">
              <button @click="tokenToRevoke = null" class="modal-btn cancel">Cancel</button>
              <button @click="executeRevoke" class="modal-btn confirm error">Confirm revoke</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue';
import { apiFetch } from '../utils/apiFetch';
import { sharedState, showToast } from '../utils/sharedState';
import { secureStorage } from '../utils/storage';

const props = defineProps({
  modelValue: Boolean
});

const emit = defineEmits(['update:modelValue']);

const tokens = ref([]);
const isLoading = ref(false);
const newTokenName = ref('');
const isGenerating = ref(false);
const newlyGeneratedToken = ref('');
const tokenToRevoke = ref(null);

const close = () => {
  newlyGeneratedToken.value = '';
  newTokenName.value = '';
  emit('update:modelValue', false);
};

const fetchTokens = async () => {
  isLoading.value = true;
  try {
    const res = await apiFetch('/api/tokens');
    if (res.ok) {
      tokens.value = await res.json();
    }
  } catch (err) {
    console.error("Failed to fetch tokens:", err); showToast('Error', 'An error occurred. Check console for details.', 'error');
  } finally {
    isLoading.value = false;
  }
};

const generateToken = async () => {
  if (!newTokenName.value.trim()) return;
  isGenerating.value = true;
  
  try {
    const res = await apiFetch('/api/tokens', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ name: newTokenName.value })
    });
    
    if (res.ok) {
      const data = await res.json();
      newlyGeneratedToken.value = data.token;
      newTokenName.value = '';
      await fetchTokens(); // Refresh list
    } else {
      const data = await res.json();
      showToast("Error", data.error || "Failed to generate token", "error");
    }
  } catch (err) {
    showToast("Error", "Connection error", "error");
  } finally {
    isGenerating.value = false;
  }
};

const promptRevoke = (token) => {
  tokenToRevoke.value = token;
};

const executeRevoke = async () => {
  if (!tokenToRevoke.value) return;
  const id = tokenToRevoke.value.id;
  
  try {
    const res = await apiFetch(`/api/tokens/${id}`, {
      method: 'DELETE'
    });
    
    if (res.ok) {
      showToast("Success", "Token revoked", "success");
      await fetchTokens();
    } else {
      showToast("Error", "Failed to revoke token", "error");
    }
  } catch (err) {
    showToast("Error", "Connection error", "error");
  } finally {
    tokenToRevoke.value = null;
  }
};

const copyToken = async (tokenStr) => {
  try {
    await navigator.clipboard.writeText(tokenStr);
    showToast("Success", "Token copied to clipboard!", "success");
  } catch (err) {
    showToast("Error", "Failed to copy token", "error");
  }
};

const formatDate = (dateStr) => {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'Never';
  const d = new Date(dateStr);
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
};

const getMcpConfig = (token) => {
  return JSON.stringify({
    mcpServers: {
      "lighthouse-mcp": {
        command: "npx",
        args: [
          "-y",
          "@cloudmcp/connect",
          "--url",
          "https://lighthouse.sirgiving.org/api/mcp/sse",
          "--header",
          `Authorization: Bearer ${token}`
        ]
      }
    }
  }, null, 2);
};

watch(() => props.modelValue, (newVal) => {
  if (newVal) {
    fetchTokens();
  }
});
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 9999;
}

.modal-content.large {
  max-width: 600px;
}

.modal-content {
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 16px;
  width: 90%;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
  display: flex;
  flex-direction: column;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  border-bottom: 1px solid var(--border);
}

.modal-title-group {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  color: var(--text-main);
}

.modal-title-group h2 {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
}

.close-btn {
  background: none;
  border: none;
  color: var(--text-mute);
  font-size: 1.5rem;
  cursor: pointer;
  padding: 0.5rem;
  line-height: 1;
  transition: color 0.2s;
}

.close-btn:hover {
  color: var(--text-main);
}

.modal-body {
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.modal-description {
  color: var(--text-mute);
  margin: 0;
  font-size: 0.95rem;
  line-height: 1.5;
}

.token-generation-section, .tokens-list-section {
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 1.25rem;
}

h3 {
  margin: 0 0 1rem 0;
  font-size: 1.1rem;
  color: var(--text-main);
}

.generate-form {
  display: flex;
  gap: 0.75rem;
}

.form-input {
  flex: 1;
  padding: 0.75rem 1rem;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-main);
  font-size: 0.95rem;
}

.form-input:focus {
  outline: none;
  border-color: var(--accent);
}

.btn {
  padding: 0.75rem 1.5rem;
  border-radius: 8px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  transition: all 0.2s;
}

.btn-primary {
  background: var(--accent);
  color: white;
}

.btn-primary:hover:not(:disabled) {
  opacity: 0.9;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.success-alert {
  margin-top: 1rem;
  padding: 1rem;
  background: rgba(16, 185, 129, 0.1);
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: 8px;
  color: #10b981;
}

.success-alert p {
  margin: 0 0 0.5rem 0;
}

.token-display {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: rgba(0, 0, 0, 0.2);
  padding: 0.75rem 1rem;
  border-radius: 6px;
  border: 1px solid rgba(16, 185, 129, 0.2);
}

.token-display code {
  font-family: monospace;
  font-size: 0.9rem;
  color: var(--text-main);
  word-break: break-all;
}

.copy-btn {
  background: none;
  border: none;
  color: #10b981;
  cursor: pointer;
  padding: 0.25rem;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.2s;
}

.copy-btn:hover {
  opacity: 0.7;
}

.mcp-config-snippet {
  margin-top: 1.5rem;
}

.mcp-config-snippet .snippet-title {
  margin: 0 0 0.25rem 0;
  color: var(--text-main);
}

.mcp-config-snippet .snippet-desc {
  margin: 0 0 0.75rem 0;
  font-size: 0.9rem;
  color: var(--text-mute);
}

.code-block-wrapper {
  position: relative;
  background: rgba(0, 0, 0, 0.4);
  border-radius: 8px;
  border: 1px solid var(--border);
  overflow: hidden;
}

.code-block-wrapper pre {
  margin: 0;
  padding: 1rem;
  overflow-x: auto;
}

.code-block-wrapper code {
  font-family: monospace;
  font-size: 0.85rem;
  color: var(--text-main);
  white-space: pre;
}

.snippet-copy {
  position: absolute;
  top: 0.5rem;
  right: 0.5rem;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0.4rem;
  color: var(--text-main);
}

.snippet-copy:hover {
  background: var(--bg-subtle);
  color: #10b981;
}

.tokens-table-container {
  overflow-x: auto;
}

.tokens-table {
  width: 100%;
  border-collapse: collapse;
}

.tokens-table th,
.tokens-table td {
  padding: 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
  color: var(--text-main);
  font-size: 0.9rem;
}

.tokens-table th {
  color: var(--text-mute);
  font-weight: 600;
  background: var(--bg-panel);
}

.btn-icon {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.5rem;
  border-radius: 6px;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  justify-content: center;
}

.btn-danger {
  color: #ef4444;
}

.btn-danger:hover {
  background: rgba(239, 68, 68, 0.1);
}

.empty-state {
  text-align: center;
  padding: 2rem;
  color: var(--text-mute);
  background: var(--bg-panel);
  border-radius: 8px;
  border: 1px dashed var(--border);
}
</style>
