import { reactive } from 'vue';
import { secureStorage } from './storage.js';

export const apiStatus = reactive({});
const inventoryPaths = new Set(['/api/containers', '/api/images', '/api/volumes', '/api/networks']);
const latestRequests = new Map();
let requestSequence = 0;
let statusToken = null;

export function unavailableNodes(response) {
  try {
    const nodes = JSON.parse(response.headers.get('X-LightHouse-Unavailable-Nodes') || '[]');
    return Array.isArray(nodes) ? [...new Set(nodes.filter((node) => typeof node === 'string' && node.trim()))] : [];
  } catch {
    return [];
  }
}

export async function getAPIError(response, fallback = 'Request failed.') {
  try {
    const body = await response.clone().json();
    return typeof body?.error === 'string' && body.error ? body.error : fallback;
  } catch {
    return fallback;
  }
}

// apiFetch wraps the native fetch() so every REST call automatically carries
// the X-LightHouse-Client header (required by the backend's client-access
// check) and the current session's Bearer token, without every call site
// having to repeat that boilerplate.
export async function apiFetch(input, init = {}) {
  const request = input instanceof Request ? input : null;
  const headers = new Headers(request?.headers);
  new Headers(init.headers || {}).forEach((value, key) => headers.set(key, value));
  if (!headers.has("X-LightHouse-Client")) {
    headers.set("X-LightHouse-Client", "web");
  }
  
  const token = secureStorage.getItem('token');
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  
  if (token !== statusToken) {
    for (const path of Object.keys(apiStatus)) delete apiStatus[path];
    statusToken = token;
  }
  const path = new URL(request?.url || input, globalThis.location?.origin || 'http://localhost').pathname;
  const tracked = inventoryPaths.has(path) && (init.method || request?.method || 'GET').toUpperCase() === 'GET';
  const sequence = ++requestSequence;
  if (tracked) latestRequests.set(path, sequence);
  const isCurrent = () => tracked && latestRequests.get(path) === sequence && secureStorage.getItem('token') === token;
  try {
    const response = await fetch(input, { ...init, headers });
    if (isCurrent()) {
      apiStatus[path] = {
        unavailableNodes: unavailableNodes(response),
        error: response.ok ? '' : `Inventory could not be refreshed (HTTP ${response.status}).`,
      };
    }
    return response;
  } catch (error) {
    if (isCurrent() && error.name !== 'AbortError') {
      apiStatus[path] = { unavailableNodes: [], error: 'Inventory could not be refreshed. The server is unreachable.' };
    }
    throw error;
  }
}
