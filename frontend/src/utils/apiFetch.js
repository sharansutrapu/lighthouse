export function apiFetch(input, init = {}) {
  const headers = new Headers(init.headers || {});
  if (!headers.has("X-LightHouse-Client")) {
    headers.set("X-LightHouse-Client", "web");
  }
  return fetch(input, { ...init, headers });
}
