import { createApp } from 'vue';
import './style.css';
import App from './App.vue';
import router from './router';
import { sharedState } from './utils/sharedState';
import { secureStorage } from './utils/storage';

// Application entry point: patches the global fetch() once to auto-logout on
// an expired/invalid session or a deactivated account, then mounts the app.

// forceLogout clears the stored session and sends the user back to /login.
const forceLogout = () => {
  secureStorage.removeItem('token');
  secureStorage.removeItem('user');
  sharedState.currentUser = null;
  sharedState.showPasswordModal = false;
  sharedState.forcePasswordChange = false;

  if (router.currentRoute.value.path !== '/login') {
    router.replace('/login').catch(() => {});
  }
};

// Monkey-patch window.fetch exactly once so every API call in the app
// (regardless of which component made it) automatically triggers a logout on
// a 401 (expired/invalid session) or a 403 flagged ACCOUNT_DEACTIVATED,
// without every call site needing its own error-handling boilerplate.
if (!window.__lighthouseFetchPatched) {
  const originalFetch = window.fetch.bind(window);

  window.fetch = async (...args) => {
    const response = await originalFetch(...args);

    if (response?.status === 401) {
      const requestUrl = typeof args[0] === 'string' ? args[0] : (args[0] && (args[0].url || args[0].href || String(args[0])));
      const isLoginRequest = typeof requestUrl === 'string' && requestUrl.includes('/api/token');
      if (!isLoginRequest) {
        forceLogout();
      }
    } else if (response?.status === 403) {
      try {
        const contentType = response.headers.get('content-type') || '';
        if (contentType.includes('application/json')) {
          const payload = await response.clone().json();
          if (payload?.code === 'ACCOUNT_DEACTIVATED') {
            forceLogout();
          }
        }
      } catch {
        // Ignore malformed responses and leave normal error handling intact.
      }
    }

    return response;
  };

  window.__lighthouseFetchPatched = true;
}

const app = createApp(App);
app.use(router);
app.mount('#app');
