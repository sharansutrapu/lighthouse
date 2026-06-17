<template>
  <div class="login-overlay">
    <div class="login-card glass animate-slide-up">
      <div class="login-header">
        <div class="logo-box">
          <img :src="logoSrc" alt="LightHouse" class="login-logo-img" />
        </div>
        <h1>LightHouse</h1>
        <p class="login-subtitle">Container observability</p>
      </div>

      <div class="login-form">
        <form @submit.prevent="login" style="display: flex; flex-direction: column; gap: 1.25rem;">
          <div class="input-group">
            <label>Username</label>
            <div class="premium-input-wrapper">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
                <circle cx="12" cy="7" r="4"></circle>
              </svg>
              <input v-model="username" type="text" placeholder="Enter username" required />
            </div>
          </div>
          <div class="input-group">
            <label>Password</label>
            <div class="premium-input-wrapper">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect>
                <path d="M7 11V7a5 5 0 0 1 10 0v4"></path>
              </svg>
              <input v-model="password" type="password" placeholder="••••••••" required />
            </div>
          </div>
          <button type="submit" :disabled="loading" class="premium-btn primary full-width login-btn">
            {{ loading ? "Authenticating..." : "Access Dashboard" }}
          </button>
        </form>

        <div style="display: flex; align-items: center; text-align: center; color: var(--text-mute); font-size: 0.75rem; font-weight: 600; letter-spacing: 0.05em; margin: 0.5rem 0;">
          <div style="flex: 1; border-bottom: 1px solid var(--border);"></div>
          <span style="padding: 0 10px;">OR</span>
          <div style="flex: 1; border-bottom: 1px solid var(--border);"></div>
        </div>

        <button @click="handleGoogleLogin" :disabled="loading" class="premium-btn primary full-width login-btn" style="background: white; color: black; margin-top: 0;">
          <svg viewBox="0 0 24 24" width="18" height="18" xmlns="http://www.w3.org/2000/svg" style="margin-right: 8px;">
            <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
            <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
            <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
            <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
          </svg>
          {{ loading ? "Redirecting..." : "Sign in with Google" }}
        </button>

        <Transition name="fade">
          <p v-if="error" class="error-msg">{{ error }}</p>
        </Transition>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { secureStorage } from "../utils/storage";
import { apiFetch } from "../utils/apiFetch";
import { sharedState } from "../utils/sharedState";
import pkg from "../../package.json";

const appVersion = pkg.version;

const router = useRouter();
const route = useRouter().currentRoute;

const loading = ref(false);
const error = ref("");
const username = ref("");
const password = ref("");

const logoSrc = computed(() => "/lighthouse-logo.svg");

const login = async () => {
  loading.value = true;
  error.value = "";
  
  try {
    const formData = new FormData();
    formData.append("username", username.value);
    formData.append("password", password.value);
    
    // Check invite token to associate user
    const inviteToken = route.value.query.invite_token;
    if (inviteToken) {
      formData.append("invite_token", inviteToken);
    }

    const res = await apiFetch("/api/token", {
      method: "POST",
      body: formData,
      noAuth: true,
    });
    
    const data = await res.json();
    
    if (res.ok && data.access_token) {
      secureStorage.setItem("token", data.access_token);
      sharedState.token = data.access_token;
      
      const userRes = await apiFetch("/api/user/me", {
        headers: { Authorization: `Bearer ${data.access_token}` }
      });
      if (userRes.ok) {
        const userData = await userRes.json();
        sharedState.currentUser = userData;
        secureStorage.setItem("user", JSON.stringify(userData));
        router.push("/dashboard");
      }
    } else {
      error.value = data.error || "Login failed.";
    }
  } catch (err) {
    error.value = "A network error occurred.";
    console.error(err);
  } finally {
    loading.value = false;
  }
};

const handleGoogleLogin = () => {
  loading.value = true;
  error.value = "";
  
  const inviteToken = route.value.query.invite_token;
  let url = "/auth/google";
  if (inviteToken) {
    url += "?invite_token=" + encodeURIComponent(inviteToken);
  }
  
  window.location.href = url;
};

onMounted(() => {
  if (route.value.query.error) {
    error.value = route.value.query.error;
    // Clean up the URL
    router.replace({ query: { ...route.value.query, error: undefined } });
  }
});
</script>

<style scoped>
.login-overlay {
  position: fixed;
  inset: 0;
  background: var(--bg-main);
  background-image:
    radial-gradient(ellipse 60% 50% at 20% 0%, rgba(var(--accent-rgb), 0.18), transparent 55%),
    radial-gradient(ellipse 50% 40% at 100% 100%, rgba(var(--success-rgb), 0.08), transparent 50%);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem;
}

.login-overlay::before {
  content: "";
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(var(--border) 1px, transparent 1px),
    linear-gradient(90deg, var(--border) 1px, transparent 1px);
  background-size: 48px 48px;
  opacity: 0.12;
  mask-image: radial-gradient(circle at 50% 40%, black, transparent 75%);
  pointer-events: none;
}

.login-card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 440px;
  padding: 2.75rem 2.5rem;
  border-radius: var(--radius-2xl);
  border: 1px solid var(--border);
  background: var(--glass-bg);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  box-shadow: 0 24px 64px -16px var(--shadow);
}

.login-header {
  text-align: center;
  margin-bottom: 2.5rem;
}

.logo-box {
  width: 88px;
  height: 88px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 1.25rem;
}

.login-logo-img {
  width: 88px;
  height: 88px;
  object-fit: contain;
  border-radius: var(--radius-lg);
  filter: drop-shadow(0 12px 28px rgba(var(--accent-rgb), 0.25));
}

.login-header h1 {
  font-size: 2rem;
  font-weight: 800;
  letter-spacing: -0.04em;
  color: var(--text-main);
  margin: 0;
}

.login-header p {
  font-size: 0.9rem;
  font-weight: 500;
  margin-top: 0.5rem;
  color: var(--text-mute);
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.input-group label {
  display: block;
  font-size: 0.7rem;
  font-weight: 700;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-bottom: 0.6rem;
}

.premium-input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.premium-input-wrapper svg {
  position: absolute;
  left: 1rem;
  color: var(--text-mute);
  transition: color 0.2s;
  pointer-events: none;
}

.premium-input-wrapper input {
  width: 100%;
  padding: 0.95rem 1rem 0.95rem 3rem;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-main);
  font-size: 0.95rem;
  font-weight: 500;
  transition: border-color 0.2s, box-shadow 0.2s, background 0.2s;
}

.premium-input-wrapper input:focus {
  outline: none;
  background: var(--bg-subtle);
  border-color: var(--accent);
  box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.12);
}

.premium-input-wrapper:focus-within svg {
  color: var(--accent);
}

.login-btn {
  height: 52px;
  font-size: 0.95rem;
  margin-top: 0.5rem;
}

.error-msg {
  text-align: center;
}

.login-footer {
  margin-top: 2.5rem;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  font-size: 0.65rem;
  font-weight: 700;
  color: var(--text-mute);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.dot-sep {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--border);
}

@media (max-width: 480px) {
  .login-card {
    padding: 1.75rem 1.25rem;
    border-radius: var(--radius-xl);
  }
  .login-header h1 {
    font-size: 1.65rem;
  }
  .login-header {
    margin-bottom: 2rem;
  }
  .logo-box {
    width: 72px;
    height: 72px;
  }
  .login-logo-img {
    width: 72px;
    height: 72px;
  }
}
</style>
