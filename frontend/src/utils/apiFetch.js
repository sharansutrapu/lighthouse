import { secureStorage } from './storage';

// apiFetch wraps the native fetch() so every REST call automatically carries
// the X-LightHouse-Client header (required by the backend's client-access
// check) and the current session's Bearer token, without every call site
// having to repeat that boilerplate.
export function apiFetch(input, init = {}) {
  const headers = new Headers(init.headers || {});
  if (!headers.has("X-LightHouse-Client")) {
    headers.set("X-LightHouse-Client", "web");
  }
  
  const token = secureStorage.getItem('token');
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  
  return fetch(input, { ...init, headers });
}
