# Architecture Overview

LightHouse is designed as a lightweight, secure bridge between teams and the Docker daemon.

## 🏗 High-Level Architecture

LightHouse supports two distinct deployment models to fit your infrastructure:

### Standalone Mode
Ideal for single-server setups. Everything runs in one lightweight process.

```mermaid
graph TD
    User((User)) -->|HTTPS/WS| FE[Vue 3 Frontend]
    FE -->|API/WebSockets| BE[Go Backend]
    BE -->|SQLite| DB[(SQLite DB)]
    BE -->|Unix Socket| DS[Docker Socket]
    DS -->|Logs/Stats| BE
```

### Hub & Spoke Mode
Designed for distributed environments. A central Hub manages multiple remote Nodes (Spokes).

```mermaid
graph TD
    User((User)) -->|HTTPS/WS| HubFE[Hub Frontend]
    HubFE -->|API/WebSockets| HubBE[Hub Backend]
    HubBE -->|PostgreSQL| HubDB[(PostgreSQL)]
    
    Spoke1[Spoke Node 1] -->|WebSocket WSS| HubBE
    Spoke2[Spoke Node 2] -->|WebSocket WSS| HubBE
    
    Spoke1 -->|Unix Socket| DS1[Docker Socket]
    Spoke2 -->|Unix Socket| DS2[Docker Socket]
```

### 1. The Backend (Go)
The backend is the core of the application. It handles:
- **Authentication**: JWT-based auth with `SECRET_KEY` signing, and OAuth 2.0 integrations (Google SSO).
- **RBAC Enforcement**: Middleware that validates every request against user permissions stored in the database.
- **Docker Interaction**: Communicates with the local Docker daemon via the standard Moby SDK.
- **Real-time Streaming**: Efficiently tails Docker logs and streams them to the client via WebSockets.
- **GitOps Management**: Clones remote Git repositories and orchestrates deployments using `docker compose` in isolated workspaces.
- **Security Scanning**: Integrates with Trivy to execute localized image vulnerability scans directly against the Docker daemon.
- **Alerting Engine**: Monitors container health and logs, dispatching alerts to webhooks and email based on customizable rules.
- **Cloud Backups**: Natively pushes scheduled `lighthouse.db` backups to AWS S3, Google Cloud Storage, or Azure Blob Storage.
- **Log Archival**: Compresses and archives container log streams to cold cloud storage.

### 2. The Frontend (Vue 3)
A modern Single Page Application (SPA) that provides:
- **Dashboard**: Real-time log viewer and container management.
- **Admin Panel**: Interface for managing users, permissions, and viewing audit logs.
- **Security**: Enforces password changes and hides unauthorized actions.

### 3. Data Storage (SQLite & PostgreSQL)
Depending on your deployment mode, LightHouse uses either a local `lighthouse.db` (SQLite) or a centralized PostgreSQL database to store:
- **User Accounts**: Credentials (hashed) and permission profiles.
- **Audit Logs**: Every administrative and container action is recorded for traceability.
- **Container Stats**: Historical performance data (CPU/Memory).
- **GitOps Projects**: Repository configuration and sync history.
- **Alerting Rules**: Definitions for when to trigger notifications.

## 🔐 Security Model

LightHouse uses a "Defense in Depth" approach:
1.  **Transport Security**: Should be deployed behind a reverse proxy (like Nginx or Traefik) for TLS.
2.  **Authentication**: Every request requires a valid JWT.
3.  **Authorization**: Even with a valid token, the backend re-validates permissions for the specific resource (container) on every request.
4.  **Audit Trail**: Every action is logged, creating a permanent record of who did what and when.
