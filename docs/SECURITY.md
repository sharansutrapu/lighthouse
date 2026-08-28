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

### API Tokens (Personal Access Tokens)
Users and AI agents (via MCP) can authenticate with long-lived `lh_pat_...` tokens instead of a JWT session.
- **Shown once**: the plaintext token is only ever returned in the response to its creation call. It is never logged and cannot be retrieved again afterward.
- **Hashed at rest**: only a SHA-256 hash of the token is stored in the database. A leaked database backup does not expose usable credentials — lookups hash the incoming `Authorization: Bearer` value and compare against the stored hash.
- **Revocation**: deleting a token from the UI immediately invalidates it for all future requests.

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

### 2. Broken Object Level Authorization (BOLA) Defenses
LightHouse enforces strict ownership and visibility boundaries:
- **Containers**: Users can only interact with or stream logs for containers that match their assigned `AllowedContainers` regex patterns.
- **GitOps Projects**: Users are strictly limited to deploying and viewing GitOps projects associated with their assigned Teams. 
- **Teams & Users**: Standard users cannot modify or view other users or teams.

### 3. GitOps Workspace Isolation (Path Traversal)
When syncing GitOps projects, the `ComposePath` variable is strictly sanitized using `filepath.Clean`. API validation ensures no traversal elements (`../`) or absolute paths (`/`) are passed, preventing users from executing `docker compose` operations outside of the secure `/tmp/lighthouse-gitops` temporary workspaces.

### 4. Command Injection Prevention
All `exec.Command` calls involving user inputs (such as Git branch names or repository URLs) use `--` command terminators (`git clone -b <branch> -- <repo_url> .`) and strict API payload validation to prevent injection of malicious shell flags or commands.

### 5. Alerting Engine Cooldowns
To prevent malicious or runaway processes from flooding external webhooks (Slack/Discord/Email) and exhausting rate limits, the Alerting Engine implements a mandatory cooldown period (e.g., 5 minutes) per alert rule per container.

### 6. MCP Server Security
The Model Context Protocol (MCP) server integration requires explicit API tokens that map to a specific user. The backend strictly binds the AI agent to the **exact same RBAC policies** and container visibility filters (`AllowedContainers`) as the user who generated the token. This guarantees that an AI assistant cannot access, view, or inspect containers that its human operator is unauthorized to see.

### 7. Automated Security Validation
The backend security constraints are continuously verified by a comprehensive End-to-End validation suite (`e2e_validator.py`). This script executes a battery of assertions against the REST API to guarantee that BOLA defenses, password requirements, and API key restrictions cannot silently regress during future development.

### 8. Secret Masking in API Responses
`GET /api/settings` and `GET /api/teams` never return SMTP passwords, the Google OAuth client secret, cloud backup/archival credentials (including full GCS service-account JSON keys), or Slack/MS Teams/Google Chat/generic webhook URLs (which embed a bearer-equivalent secret in their path) in plaintext. Previously-saved values are represented as `********`; the corresponding `PUT` handlers detect and preserve that placeholder instead of overwriting the real secret, so the settings/team-edit forms round-trip safely without ever re-displaying a stored credential.

### 9. SMTP Header Injection Prevention
Email alert delivery strips CR/LF characters from every value interpolated into SMTP headers (`From`, `To`, `Cc`, `Subject`) before sending, preventing an attacker-influenced container name or alert payload from injecting additional headers or forging recipients.

### 10. Constant-Time Shared-Secret Comparison
The Hub validates the Spoke's `HUB_TOKEN` using `crypto/subtle.ConstantTimeCompare` rather than a plain string comparison, removing a timing side-channel on this network-facing credential check.

## 🛡️ Best Practices

1.  **Reverse Proxy**: Always run LightHouse behind a reverse proxy (Nginx, Traefik, Caddy) to handle SSL/TLS termination.
2.  **Docker Socket**: Be careful with mounting the Docker socket. Only expose LightHouse to trusted networks or use a VPN.
3.  **Password Policy**: Minimum 8 characters. First login requires a mandatory password change. For maximum security, the backend strictly requires the user to provide their temporary `current_password` when executing this forced password change, preventing unauthenticated session hijacking. Default credentials are `admin` / `admin123` — change immediately after first login.

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
| WebSocket (Vue UI in browser) | Valid Origin (browsers cannot set custom WS headers) |

Environment variables:

- `CLIENT_ACCESS=strict` — default; set `off` only for local debugging
- `ALLOWED_ORIGINS` — comma-separated extra web origins
- `TRUST_PROXY=true` — honor `X-Forwarded-Host` / `X-Forwarded-Proto` (only when behind a trusted reverse proxy)
- `ENV=production` — disables localhost origin bypass

LightHouse also sends standard security headers (CSP, `X-Frame-Options`, etc.) on all responses.
