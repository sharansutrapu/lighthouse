import { reactive } from 'vue';
import { secureStorage } from './storage';
import { apiFetch } from './apiFetch';

// This module holds the single reactive `sharedState` object used across the
// whole app (current user, theme, sidebar state, global toast, server-side
// feature flags) plus the helper functions that read/write it, so every
// component sees the same live values without prop-drilling.

// getSystemTheme reads the OS/browser's preferred color scheme.
export function getSystemTheme() {
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function normalizeThemePreference(value) {
  if (value === 'system') return 'auto';
  if (value === 'light' || value === 'dark' || value === 'auto') return value;
  return 'auto';
}

function isAutoTheme(preference) {
  return preference === 'auto' || preference === 'system';
}

function loadThemePreference() {
  const stored = secureStorage.getItem('theme');
  if (!stored) return 'auto';
  return normalizeThemePreference(stored);
}

export function resolveTheme(preference = loadThemePreference()) {
  if (isAutoTheme(preference)) return getSystemTheme();
  return preference;
}

const initialThemePreference = loadThemePreference();
const initialResolvedTheme = resolveTheme(initialThemePreference);

if (typeof document !== 'undefined') {
  document.documentElement.setAttribute('data-theme', initialResolvedTheme);
}

export const sharedState = reactive({
  currentUser: null,
  systemStats: { cpu: 0, usedMemGB: 0, memory: '0 / 0' },
  // Note: searchQuery intentionally removed — each page manages its own local search ref.
  themePreference: initialThemePreference,
  theme: initialResolvedTheme,
  showPasswordModal: false,
  forcePasswordChange: false,
  dashboardSidebarOpen: typeof window !== 'undefined' ? window.innerWidth > 1024 : true,
  adminSidebarOpen: typeof window !== 'undefined' ? window.innerWidth > 1024 : true,
  toast: {
    visible: false,
    title: '',
    message: '',
    type: 'success'
  },
  configLoaded: false,
  envStartPermission: false,
  envStopPermission: false,
  envRestartPermission: false,
  envDeletePermission: false,
  envShellPermission: false,
  isBackendDisconnected: false,
});

// applyTheme sets the active theme preference ("light", "dark", or "auto"),
// persists it, and updates the <html data-theme> attribute used by CSS.
export function applyTheme(preference) {
  const normalized = normalizeThemePreference(preference);
  sharedState.themePreference = normalized;
  sharedState.theme = resolveTheme(normalized);
  secureStorage.setItem('theme', normalized);
  document.documentElement.setAttribute('data-theme', sharedState.theme);
}

// toggleTheme flips between light and dark (used by the manual theme switch).
export function toggleTheme() {
  applyTheme(sharedState.theme === 'dark' ? 'light' : 'dark');
}

// initThemeListener subscribes to OS theme changes while preference is
// "auto", returning an unsubscribe function for cleanup on unmount.
export function initThemeListener() {
  if (typeof window === 'undefined') return () => {};

  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
  const handleChange = () => {
    if (!isAutoTheme(sharedState.themePreference)) return;
    sharedState.theme = getSystemTheme();
    document.documentElement.setAttribute('data-theme', sharedState.theme);
  };

  mediaQuery.addEventListener('change', handleChange);
  return () => mediaQuery.removeEventListener('change', handleChange);
}

// showToast displays a global toast notification for 4 seconds.
export const showToast = (title, message, type = 'success') => {
  sharedState.toast.title = title;
  sharedState.toast.message = message;
  sharedState.toast.type = type;
  sharedState.toast.visible = true;
  setTimeout(() => {
    sharedState.toast.visible = false;
  }, 4000);
};

// fetchCurrentUser loads the logged-in user's profile into sharedState,
// clearing the stored session if the backend reports it's no longer valid (403).
export const fetchCurrentUser = async () => {
  const token = secureStorage.getItem('token');
  if (!token) return { status: 'missing', user: null };
  try {
    const res = await apiFetch('/api/user/me', {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      sharedState.currentUser = await res.json();
      return { status: 'ok', user: sharedState.currentUser };
    }
    if (res.status === 403) {
      sharedState.currentUser = null;
      secureStorage.removeItem('token');
      secureStorage.removeItem('user');
      return { status: 'forbidden', user: null };
    }
  } catch (e) {
    console.error('Failed to fetch user:', e);
  }
  return { status: 'error', user: null };
};

// fetchSystemStats polls the host CPU/memory snapshot into sharedState, also
// toggling the "backend unreachable" banner based on request success/failure.
export const fetchSystemStats = async () => {
  const token = secureStorage.getItem('token');
  if (!token) return;
  try {
    const res = await apiFetch('/api/system/stats', {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (res.ok) {
      if (sharedState.isBackendDisconnected) {
        sharedState.isBackendDisconnected = false;
        showToast("Backend Reconnected", "Connection to the server has been restored", "success");
      }
      const data = await res.json();
      sharedState.systemStats = {
        cpu: data.cpu,
        cores: data.cores || 1,
        memory: data.memory,
        total_memory: data.total_memory
      };
    } else {
      handleBackendError();
    }
  } catch (e) {
    handleBackendError();
    console.error('Failed to fetch system stats:', e);
  }
};

// handleBackendError shows the "server unreachable" toast once (not on every
// failed poll) until connectivity is restored.
const handleBackendError = () => {
  if (!sharedState.isBackendDisconnected) {
    sharedState.isBackendDisconnected = true;
    showToast("Server Unreachable", "Cannot connect to the backend server. Please check if it is running.", "error");
  }
};

// userCanStart/Stop/Restart/Delete/Shell mirror the backend's two-layer
// permission model: admins always pass; everyone else needs both the
// server-wide env toggle (envXPermission, from GET /api/config) AND their own
// can_x flag. Purely a UI convenience — the backend re-checks independently.
export function userCanStart(user) {
  if (user?.is_admin) return true;
  return sharedState.envStartPermission && user?.can_start === true;
}

export function userCanStop(user) {
  if (user?.is_admin) return true;
  return sharedState.envStopPermission && user?.can_stop === true;
}

export function userCanRestart(user) {
  if (user?.is_admin) return true;
  return sharedState.envRestartPermission && user?.can_restart === true;
}

export function userCanDelete(user) {
  if (user?.is_admin) return true;
  return sharedState.envDeletePermission && user?.can_delete === true;
}

export function userCanShell(user) {
  if (user?.is_admin) return true;
  return sharedState.envShellPermission && user?.can_shell === true;
}

// formatBytes renders a byte count as a human-readable string (e.g. "1.5MB").
export function formatBytes(bytes) {
  if (!bytes || bytes <= 0 || isNaN(bytes)) return '0B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + sizes[i];
}
