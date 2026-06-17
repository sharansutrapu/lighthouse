import { createRouter, createWebHistory } from 'vue-router';
import { secureStorage, parseJwt } from '../utils/storage';
import { sharedState } from '../utils/sharedState';
import { apiFetch } from '../utils/apiFetch';

const routes = [
  { path: '/', redirect: to => ({ path: '/dashboard', query: to.query }) },
  { 
    path: '/dashboard', 
    name: 'Dashboard', 
    component: () => import('../views/Dashboard.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Dashboard' }
  },
  { 
    path: '/containers', 
    name: 'Containers', 
    component: () => import('../views/Containers.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Container Management' }
  },
  {
    path: '/containers/:id',
    name: 'ContainerDetail',
    component: () => import('../views/ContainerDetail.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Container Details' }
  },
  { 
    path: '/logs', 
    name: 'Logs', 
    component: () => import('../views/Logs.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Live Log Stream' }
  },
  {
    path: '/shell',
    name: 'Shell',
    component: () => import('../views/Shell.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Container Shell' }
  },
  { 
    path: '/health', 
    name: 'Health', 
    component: () => import('../views/Health.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'System Health' }
  },
  { 
    path: '/admin', 
    name: 'Admin', 
    component: () => import('../views/Admin.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, layout: 'main', title: 'Admin Control Center' }
  },
  { 
    path: '/audit', 
    name: 'Audit', 
    component: () => import('../views/Audit.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, layout: 'main', title: 'Security Audits' }
  },
  { 
    path: '/gitops', 
    name: 'GitOps', 
    component: () => import('../views/GitOps.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'GitOps Automations' }
  },
  {
    path: '/scans',
    name: 'Scans',
    component: () => import('../views/Scans.vue'),
    meta: { requiresAuth: true, layout: 'main', title: 'Vulnerability Scans' }
  },
  { 
    path: '/login', 
    name: 'Login', 
    component: () => import('../views/Login.vue'),
    meta: { title: 'Sign In' }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('../views/NotFound.vue'),
    meta: { title: 'Page Not Found' }
  }
];

const router = createRouter({
  history: createWebHistory(),
  routes
});

router.beforeEach(async (to, from, next) => {
  if (!sharedState.configLoaded) {
    try {
      const res = await apiFetch('/api/config');
      if (res.ok) {
        const data = await res.json();
        sharedState.envStartPermission = data.allow_start !== false;
        sharedState.envStopPermission = data.allow_stop !== false;
        sharedState.envRestartPermission = data.allow_restart !== false;
        sharedState.envDeletePermission = data.allow_delete !== false;
        sharedState.envShellPermission = data.allow_shell === true;
      }
    } catch (e) {
      console.error('Failed to load auth config:', e);
    }
    sharedState.configLoaded = true;
  }

  // Update Page Title
  const baseTitle = 'LightHouse';
  document.title = to.meta.title ? `${to.meta.title} | ${baseTitle}` : baseTitle;

  // Handle OAuth Token exchange via URL code
  if (to.query.code) {
    try {
      const res = await apiFetch('/api/token/exchange', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: `code=${encodeURIComponent(to.query.code)}`
      });
      if (res.ok) {
        const data = await res.json();
        secureStorage.setItem('token', data.access_token);
        secureStorage.setItem('refresh_token', data.refresh_token);
      }
    } catch (e) {
      console.error('Failed to exchange auth code:', e);
    }
    const q = { ...to.query };
    delete q.code;
    return next({ path: to.path, query: q, replace: true });
  }

  // Handle legacy OAuth Token passing via URL (just in case)
  if (to.query.token) {
    secureStorage.setItem('token', to.query.token);
    const q = { ...to.query };
    delete q.token;
    return next({ path: to.path, query: q, replace: true });
  }

  const token = secureStorage.getItem('token');
  const claims = parseJwt(token);
  const isAdmin = claims?.is_admin === true;
  const isExpired = claims?.exp ? (claims.exp * 1000 < Date.now()) : false;

  if (to.meta.requiresAuth && (!token || isExpired)) {
    if (isExpired) {
      secureStorage.removeItem('token');
      secureStorage.removeItem('user');
    }
    next({ path: '/login', query: to.query });
  } else if (to.meta.requiresAdmin && !isAdmin) {
    next('/dashboard');
  } else if (to.path === '/health' && !isAdmin) {
    next('/dashboard');
  } else {
    next();
  }
});

export default router;
