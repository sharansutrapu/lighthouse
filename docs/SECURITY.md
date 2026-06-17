# Security & RBAC

LightHouse is built with security as a first-class citizen. It implements a multi-layer security model to ensure that container management is both accessible and restricted.

## 🔑 Authentication

LightHouse uses **JWT (JSON Web Tokens)** for stateless authentication.
- **Token Issue**: Tokens are issued upon successful login at `/api/token`.
- **Encryption**: Tokens are signed using the `SECRET_KEY` environment variable.
- **Expiry**: Tokens are valid for 24 hours by default.

### Generating a Secure Secret Key
For production deployments, you **must** generate a unique secret key. You can use the following command:
```bash
openssl rand -base64 32
```
Pass this value to the `SECRET_KEY` environment variable.

## 🔐 Role-Based Access Control (RBAC)

Permissions are divided into two categories: **Visibility** and **Actions**.

### 1. Visibility (Regex Filtering)
Administrators can restrict which containers a user can see using **Allowed Containers** patterns.
- **Full Access**: `.*` (Regex for "everything")
- **Specific Match**: `redis` (Only matches the exact name)
- **Wildcard**: `backend*` (Matches `backend-api`, `backend-db`, etc.)
- **Multiple**: `api-*, db-*` (Comma-separated patterns)

The backend translates glob wildcards into anchored regex patterns to prevent accidental exposure.

### 2. Action Rights
Container actions use the same two-layer model as shell access:

1. **Server env** — the action must be enabled (`ALLOW_START`, `ALLOW_STOP`, `ALLOW_RESTART`, `ALLOW_DELETE`, `ALLOW_SHELL`).
2. **User DB flags** — every account (including administrators) also needs the matching `can_*` permission.

Action flags:
- `can_start`: Start stopped containers (requires `ALLOW_START=true`).
- `can_stop`: Stop running containers (requires `ALLOW_STOP=true`).
- `can_restart`: Restart containers (requires `ALLOW_RESTART=true`).
- `can_delete`: Remove containers (requires `ALLOW_DELETE=true`).
- `can_shell`: Interactive shell (requires `ALLOW_SHELL=true` or `ALLOW_BASH=true`).

## 🕵️ Audit Logging

LightHouse maintains a permanent record of all sensitive actions in the `audit_logs` table.
Each entry includes:
- **Timestamp**
- **User ID & Username**
- **Action Performed** (e.g., `START`, `STOP`, `RESET_PASSWORD`)
- **Target Resource** (Container ID or Name)
- **Status** (Success/Failure/Forbidden)

Administrators can view these logs directly in the **Admin Panel**.

## 🛡️ Attack Surface Mitigations

LightHouse includes several protections against common attack vectors:

### 1. Brute Force Protection (Rate Limiting)
The `/api/token` login endpoint implements IP-based rate limiting. Users are restricted to a maximum of 10 failed login attempts per 15-minute window. Exceeding this limit triggers a system alert and blocks further attempts from that IP.

### 2. GitOps Workspace Isolation (Path Traversal)
When syncing GitOps projects, the `ComposePath` variable is strictly sanitized using `filepath.Clean`. API validation ensures no traversal elements (`../`) or absolute paths (`/`) are passed, preventing users from executing `docker compose` operations outside of the secure `/tmp/lighthouse-gitops` temporary workspaces.

### 3. Command Injection Prevention
All `exec.Command` calls involving user inputs (such as Git branch names or repository URLs) use `--` command terminators (`git clone -b <branch> -- <repo_url> .`) and strict API payload validation to prevent injection of malicious shell flags or commands.

## 🛡️ Best Practices

1.  **Reverse Proxy**: Always run LightHouse behind a reverse proxy (Nginx, Traefik, Caddy) to handle SSL/TLS termination.
2.  **Docker Socket**: Be careful with mounting the Docker socket. Only expose LightHouse to trusted networks or use a VPN.
3.  **Password Policy**: Minimum 8 characters. First login requires a password change. Default credentials are `admin` / `admin123` — change immediately after first login.

### Emergency password reset (CLI)

If an administrator is locked out, reset the password from the host (do not use `sqlite3` on the live DB — the server holds a write lock):

```bash
docker exec lighthouse lighthouse reset-password admin 'YourNewPassword123'
```

If the database is locked, stop the service and run a one-off container with the same `DB_PATH` volume, then start again.

## 🌐 Client Access Control

LightHouse rejects direct `/api` and `/ws` calls from arbitrary browser origins.

| Client | Requirements |
| --- | --- |
| Vue web UI (browser) | `X-LightHouse-Client: web` + `Origin` or `Referer` matching the server or `ALLOWED_ORIGINS` |
| Native mobile app (Flutter, Android/iOS) | No browser Origin; JWT authentication after login |
| WebSocket (Vue UI in browser) | Valid Origin (browsers cannot set custom WS headers) |
| WebSocket (native mobile app) | No Origin + JWT subprotocol |

The Flutter app (`com.lighthouse.app`) is **native mobile only** — it is not a web client. Do not use Flutter Web; use the bundled Vue dashboard in the browser.

Environment variables:

- `CLIENT_ACCESS=strict` — default; set `off` only for local debugging
- `ALLOWED_ORIGINS` — comma-separated extra web origins
- `TRUST_PROXY=true` — honor `X-Forwarded-Host` / `X-Forwarded-Proto` (only when behind a trusted reverse proxy)
- `ENV=production` — disables localhost origin bypass

LightHouse also sends standard security headers (CSP, `X-Frame-Options`, etc.) on all responses.

Native mobile apps connect with JWT after login (no browser Origin). Mobile setup docs belong in your private Flutter repo.
