import { secureStorage } from "./storage";

// createAuthenticatedWebSocket opens a WebSocket to path, carrying the
// current session's JWT as a Sec-WebSocket-Protocol entry (browsers cannot
// set custom headers during a WS handshake, so the backend reads the token
// from here instead — see auth_helpers.go's extractWSToken).
export function createAuthenticatedWebSocket(path) {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const token = secureStorage.getItem("token");
  return new WebSocket(`${protocol}//${location.host}${path}`, [
    "lighthouse-auth",
    token,
  ]);
}
