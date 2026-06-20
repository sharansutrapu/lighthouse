import { secureStorage } from './storage';

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
