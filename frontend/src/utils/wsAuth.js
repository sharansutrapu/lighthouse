import { secureStorage } from "./storage";

export function createAuthenticatedWebSocket(path) {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const token = secureStorage.getItem("token");
  return new WebSocket(`${protocol}//${location.host}${path}`, [
    "lighthouse-auth",
    token,
  ]);
}
