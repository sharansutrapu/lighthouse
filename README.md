# LightHouse 🐳

<p align="center">
  <img src="frontend/public/lighthouse-logo.svg" alt="LightHouse Logo" width="220">
</p>

<p align="center">
  <strong>High-performance, real-time Docker log viewer built for teams.</strong>
</p>

<p align="center">
  <a href="https://lighthouses.digital">Official Website</a> | <a href="https://lighthouses.digital/guide">Online Documentation</a>
</p>

<p align="center">
  Lightweight. Secure. Modern. Built for real-world Docker environments.
</p>

<p align="center">
  LightHouse provides real-time log streaming, RBAC, audit logging, system monitoring, Docker container management, and optional Kubernetes visibility in a clean modern interface.
</p>

<p align="center">
  <a href="https://hub.docker.com/r/sharankumar619/lighthouse"><img src="https://img.shields.io/docker/pulls/sharankumar619/lighthouse" alt="Docker Pulls"></a>
  <a href="https://github.com/sharansutrapu/lighthouse/blob/main/LICENSE"><img src="https://img.shields.io/github/license/sharansutrapu/lighthouse" alt="License"></a>
  <img src="https://img.shields.io/github/v/release/sharansutrapu/lighthouse" alt="Version">
  <img src="https://img.shields.io/badge/Backend-Go-00add8" alt="Backend">
  <img src="https://img.shields.io/badge/Frontend-Vue--3-42b883" alt="Frontend">
  <a href="https://github.com/sharansutrapu/lighthouse"><img src="https://img.shields.io/github/stars/sharansutrapu/lighthouse?style=social" alt="GitHub stars"></a>
</p>

---

> ⚡ **Average setup time: under 2 minutes.**
> 
> LightHouse focuses on fast deployment, low resource usage, and team-safe Docker visibility without requiring heavyweight observability tooling.

---

# ✨ Core Features

### 🏢 Architecture Options (Standalone vs. Hub & Spoke)
LightHouse is built to scale with your infrastructure, offering two distinct deployment models:
- **Standalone Mode:** Perfect for a single server. Operates with a lightweight, embedded **SQLite** database and requires zero external dependencies.
- **Hub & Spoke Mode:** Designed for distributed environments. Deploy a central **Hub** backed by **PostgreSQL** to manage multiple remote nodes. Lightweight **Spoke** agents run on your worker nodes and establish secure, persistent WebSocket connections back to the Hub, streaming logs and metrics in real-time.

### 🔄 GitOps Auto-Deployments
Manage your Docker infrastructure using modern GitOps practices:
- **Continuous Sync:** Connect GitHub, GitLab, or Bitbucket repositories directly to LightHouse.
- **Automated Deployments:** LightHouse automatically polls for changes and executes `docker compose up -d` whenever your `docker-compose.yml` updates.
- **Private Repositories:** Full support for authentication tokens to securely pull private stacks.
- **Deployment History:** Maintains a complete, easily accessible history of all deployment attempts, sync statuses, and execution logs.

### 🛡️ Vulnerability Scanning (Trivy)
Keep your infrastructure secure with native image scanning:
- **Trivy Integration:** Built-in wrapper for `aquasec/trivy`, the industry standard for container security.
- **Instant Scans:** Scan any running container's image directly from the Container Details dashboard with a single click, or **Scan All** containers at once.
- **Detailed Reporting:** View comprehensive CVE reports, severity badges (Critical, High, Medium, Low), and identify exactly which packages are vulnerable without leaving the UI.

### 🤖 MCP Support (Model Context Protocol)
Supercharge your AI agents with direct access to your Docker infrastructure:
- **Seamless Integration:** Native support for the Model Context Protocol (MCP) using SSE (Server-Sent Events) and stateless message exchanges.
- **Secure Access:** Generate, manage, and revoke dedicated API tokens for your AI assistants directly from the UI.
- **RBAC Enforced:** AI Agents are bound by the exact same Role-Based Access Control (RBAC) and visibility filters as the user who generated their token. An AI cannot see or interact with a container its owner doesn't have access to.
- **Easy Configuration:** Get instant, copy-paste ready `npx` connection commands from the dedicated MCP Configuration panel.
- **AI-Driven DevOps:** Allow your LLMs and agents to query container health, read logs, and trigger deployments safely.

### 🚨 Alerting & Webhooks
Never miss a critical event with the highly customizable Alerting Engine:
- **Extensive Rules:** Comes with 17 default alert rules covering CPU/Memory spikes, container crashes, OOM kills, and more.
- **Resource Thresholds:** Set specific CPU and Memory limits (e.g., alert if CPU > 80% for 5 minutes).
- **Log Pattern Matching:** Trigger alerts when specific Regex patterns or error strings appear in a container's log stream.
- **System & Feature Events:** Trigger notifications on critical platform events like container crashes, OOM kills, Vulnerability Scan results, GitOps deployment status, and database Backup results.
- **Targeted Monitoring:** Apply rules globally, or restrict them to specific containers using Regex names (e.g., `^prod-.*$`).
- **Team-Based Routing:** Configure unique Webhooks and Email addresses per Team. Alerts are intelligently grouped and routed only to the Teams that have visibility of the affected container, drastically reducing noise.
- **SMTP Optimized:** Intelligently groups email recipients into a single CC batch to preserve your SMTP provider limits.
- **Flexible Dispatch:** Instantly route notifications to Slack, Discord, MS Teams, Email, or any custom Webhook endpoint.
- **Spam Prevention:** Built-in cooldown mechanisms ensure your channels aren't flooded during persistent issues.

### 💾 Automated Cloud Backups
Protect your infrastructure configuration and historical metrics with native cloud backups:
- **Multi-Cloud Support:** Seamlessly backup your `lighthouse.db` to AWS S3, MinIO, Google Cloud Storage, or Azure Blob Storage.
- **Cron Scheduling:** Configure precise, automated backup schedules using standard Cron expressions.
- **Compression:** Databases are automatically wrapped into `.tar.gz` archives to save bandwidth and storage space.
- **Zero-Dependency:** Utilizes native provider SDKs so you don't need any external backup daemon or script running on the host.

### 🗄️ Long-Term Storage Archival
Preserve your container lifecycle footprints to remote storage indefinitely, optimizing your local SQLite disk utilization:
- **Metrics & Logs Archival:** Compress and archive container JSONL metrics and `.tar.gz` multiplexed logs.
- **Cloud Providers:** Supports Amazon S3, Google Cloud Storage, and Azure Blob Storage integrations.
- **Independent Schedulers:** Driven by `robfig/cron/v3`, running independently of system backups. 

### 📜 Real-Time Logs & 💻 Interactive Shell
Unparalleled visibility and control over your running containers:
- **Live Log Streaming:** Lightning-fast WebSocket log streaming with infinite scroll, manual history loading, and smart auto-scroll.
- **Search & Highlighting:** Powerful regex-based search with real-time text highlighting and safe HTML rendering.
- **Interactive Terminal:** Open a secure, fully interactive bash/sh shell inside any container directly from your browser—no SSH required.
- **Subprotocol Auth:** Both logs and shell sessions are secured via WebSocket JWT subprotocol authentication, preventing token leakage.

### 🔐 Advanced RBAC & Audit Logs
Enterprise-grade security controls to keep your team and infrastructure safe:
- **Multi-Team Management:** Organize users into logical Teams and map multiple Teams to environments, users, or projects seamlessly.
- **Granular Permissions:** Assign specific operational rights to users, including `Start`, `Stop`, `Restart`, `Delete`, and `Shell` access.
- **Regex-Based Visibility:** Restrict which containers a user can see or manage using wildcards (`backend-*`) or full regular expressions (`^prod-.*$`).
- **BOLA Protection:** Robust Broken Object Level Authorization (BOLA) defenses ensure that users can only access endpoints and actions authorized for their assigned containers and GitOps projects.
- **Complete Audit Trails:** Every administrative action, shell session, and container operation is permanently recorded in the Audit Logs, tracking exactly *who* did *what* and *when*.
- **Single Sign-On (SSO):** Authenticate using Google OAuth securely.
- **Automated Validation:** The platform is rigorously tested with an automated End-to-End validation suite (`e2e_validator.py`) to prevent regressions in security policies and critical path APIs.

---

# 📸 Preview

## 📊 Dashboard
Real-time Docker monitoring with lightweight system metrics and container controls across your entire cluster.

## 🐳 Container Management & GitOps
Monitor, control, and deploy containers with fast operational actions. Connect Git repositories to track deployment history and sync states.

## 📜 Real-Time Logs & Shell
Stream container logs live with search, highlighting, and auto-scroll. Access secure, fully interactive bash shells inside containers.

## 🛡️ Security & Audits
Run deep vulnerability scans on running images, manage granular RBAC policies, and view complete audit trails of all actions.

---

# 🛠 Tech Stack

| Layer            | Technology                |
| ---------------- | ------------------------- |
| Backend          | Go + Echo                 |
| Frontend         | Vue 3 + Vite              |
| ORM / Database   | GORM (PostgreSQL & SQLite)|
| Streaming        | WebSockets                |
| Container Engine | Docker SDK (Moby)         |

---

# ⚙️ Configuration & Environment Variables

LightHouse reads several environment variables to configure its runtime behavior:

| Environment Variable | Default Value | Available Options | Description |
| :--- | :--- | :--- | :--- |
| **`LIGHTHOUSE_MODE`** | `standalone` | `standalone`, `hub`, `spoke` | The node operational mode. |
| **`NODE_NAME`** | *Hostname* | Any unique string | Unique identifier for the node (used in metrics partitioning). |
| **`DB_TYPE`** | `sqlite` | `sqlite`, `postgres` | The database engine type. |
| **`DB_DSN`** | *None* | Connection URL string | The database connection string (DSN) for PostgreSQL or SQLite. |
| **`DB_PATH`** | `/app/data/lighthouse.db` | Directory path or `:memory:` | The path to the SQLite file. Falls back to `:memory:` if auth is disabled. |
| **`PORT`** | `8000` | Any port number | The network serving port. |
| **`SECRET_KEY`** | `secret-key-change-this`| Any secure string | The signing key for JWT tokens. **Must be changed in production.** |
| **`CLIENT_ACCESS`** | `strict` | `strict`, `off` | Enforces CORS Origin header validation for clients. |
| **`ALLOWED_ORIGINS`**| *None* | Comma-separated domains | Allowed Origin domains for CORS checks. |
| **`TRUST_PROXY`** | `false` | `true`, `false` | Trust incoming `X-Forwarded-Host`/`Proto` headers. |
| **`ALLOW_START`** | `false` | `true`, `false` | Global setting to allow starting containers. |
| **`ALLOW_STOP`** | `false` | `true`, `false` | Global setting to allow stopping containers. |
| **`ALLOW_RESTART`** | `false` | `true`, `false` | Global setting to allow restarting containers. |
| **`ALLOW_DELETE`** | `false` | `true`, `false` | Global setting to allow removing containers. |
| **`ALLOW_SHELL`** / **`ALLOW_BASH`**| `false` | `true`, `false` | Global setting to allow terminal shell access. |
| **`EXCLUDE_CONTAINERS`** | *None* | Comma-separated list | List of container names to hide from the dashboard. |
| **`HUB_URL`** | *None* | Host endpoint address | The WebSocket endpoint of the Hub (required for Spokes). |
| **`HUB_TOKEN`** | *None* | Secure token string | Shared authorization token (required for Spokes and Hub). |

*(Note: The LightHouse container itself is always hidden from the dashboard)*

---

# 🚀 Deployment

## Standalone (SQLite)
The quickest way to get started on a single node:

```yaml
version: '3.8'
services:
  lighthouse:
    image: sharankumar619/lighthouse:latest
    container_name: lighthouse
    restart: unless-stopped
    ports:
      - "8000:8000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - lighthouse-data:/app/data
    environment:
      - SECRET_KEY=generate-a-secure-random-string
      - DB_TYPE=sqlite
      - DB_PATH=/app/data/lighthouse.db

volumes:
  lighthouse-data:
```

## Hub & Spoke (PostgreSQL)

**1. Deploy the Hub**
```yaml
version: '3.8'
services:
  lighthouse-hub:
    image: sharankumar619/lighthouse:latest
    environment:
      - LIGHTHOUSE_MODE=hub
      - DB_TYPE=postgres
      - DB_DSN=host=db user=lighthouse password=secure dbname=lighthouse port=5432 sslmode=disable
      - SECRET_KEY=your-secure-secret
```

**2. Deploy a Spoke on a target node**
```yaml
version: '3.8'
services:
  lighthouse-spoke:
    image: sharankumar619/lighthouse:latest
    environment:
      - LIGHTHOUSE_MODE=spoke
      - HUB_URL=ws://hub-address.com
      - HUB_TOKEN=generated-spoke-token
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

---

# 🔐 First Login
By default, LightHouse creates an administrator account on first boot:
- **Username:** `admin`
- **Password:** `admin123`

*You will be forced to change this password immediately upon logging in.*

---

## Architecture & Lifecycle Deep Dive

### 🏗️ Micro-Architecture
```mermaid
graph TD
    subgraph Frontend [Frontend Vue 3 Client]
        UI[Glassmorphic UI]
        Term[Terminal xterm.js]
    end

    subgraph Backend [Backend Engine Go / Echo]
        Router[Echo Routing Engine]
        AM[Alert Manager]
        GitMgr[GitOps Manager]
        Sch[Backup & Archival Schedulers]
        DockerSDK[Docker Socket Client]
    end

    subgraph ClusteringLayer [Clustering Layer]
        Hub[Hub Multiplexer]
        Spoke[Spoke Node Agent]
    end

    subgraph Database [Database]
        GORM[SQLite / Postgres]
    end

    UI -->|HTTP / WebSocket| Router
    Term -->|WS Subprotocol| Router
    Router --> GORM
    Router --> DockerSDK
    
    AM -->|Subscribe| DockerSDK
    AM -->|Record| GORM
    
    GitMgr -->|Shell exec| CMD[Docker Compose CLI]
    Sch -->|Cloud SDKs| Cloud[S3 / MinIO / GCS / Azure]
    
    Hub <-->|WebSocket RPC| Spoke
    Spoke -->|Docker API| LocalSocket["/var/run/docker.sock"]
```

### 📡 Spoke Node connecting to the Hub
This sequence diagram illustrates the spoke registration handshake, heartbeat loop, and multiplexed data synchronization channels.

```mermaid
sequenceDiagram
    autonumber
    participant Spoke as Spoke Node (Agent)
    participant Hub as Hub (Control Plane)
    participant DB as Central Database (GORM)

    Note over Spoke, Hub: Spoke starts connection loop
    Spoke->>Hub: Dial HTTP GET /api/spoke/connect?token=HUB_TOKEN&node_id=NODE_ID
    Note over Hub: Hub validates token matching HUB_TOKEN
    alt Invalid Token
        Hub-->>Spoke: HTTP 401 Unauthorized
    else Valid Token
        Hub->>Spoke: WebSocket Handshake Upgraded
        Hub->>Hub: Register connection in GlobalHub.Spokes[NODE_ID]
        Spoke->>Spoke: Connected successfully
        par Background Sync (Every 5s)
            Spoke->>Spoke: Poll Docker daemon ContainerList()
            Spoke->>Hub: Push WS Text Frame: {type: "containers", data: [...] }
            Hub->>Hub: Update GlobalHub.SpokeContainers[NODE_ID]
        and Stats Push (Every 30s)
            Spoke->>Spoke: Collect stats CPU / Mem / Network / IO
            Spoke->>Hub: Push WS Text Frame: {type: "stat" / "system_stat", data: [...] }
            Hub->>DB: Save stat row to Central DB
        end
    end
```

### 🔄 GitOps Commit Hook & Polling Lifecycle
This sequence diagram shows the step-by-step process of project synchronization, code compilation, and Docker Compose deployment.

```mermaid
sequenceDiagram
    autonumber
    participant Mgr as GitOps Manager Loop (Goroutine)
    participant DB as Central Database (GORM)
    participant Git as Git Repository (Remote)
    participant Docker as Docker daemon (compose up)

    Note over Mgr: Runs every 30s (processProjects)
    Mgr->>DB: Fetch all GitProject records
    DB-->>Mgr: List of projects
    loop For each GitProject
        alt SourceType == "inline"
            Mgr->>Mgr: Write ComposeContent string to /tmp/.../docker-compose.yml
            Mgr->>Mgr: Compute SHA-256 hash as Pseudo-Commit SHA
        else SourceType == "git"
            Mgr->>Mgr: Check if .git exists in workDir
            alt Not cloned
                Mgr->>Git: Clone repository (branch, URL, token)
            else Already cloned
                Mgr->>Git: git fetch && git reset --hard origin/branch
            end
            Mgr->>Mgr: Read commit SHA via "git rev-parse HEAD"
        end
        
        Mgr->>DB: Read last_commit and status
        alt Commit SHA is different OR Status != "synced"
            Mgr->>DB: Update project status to "pending"
            Mgr->>Docker: Execute "docker compose -f docker-compose.yml up -d"
            alt Success
                Mgr->>DB: Insert GitDeployment (status: "success", logs: stdout)
                Mgr->>DB: Update GitProject (status: "synced", last_commit: SHA)
            else Failure
                Mgr->>DB: Insert GitDeployment (status: "failed", logs: stderr)
                Mgr->>DB: Update GitProject (status: "failed")
            end
        else No Changes
            Note over Mgr: Skip deployment
        end
    end
```

### 💻 Live Log & Shell Request Lifecycle
This sequence diagram illustrates the lifecycle of a real-time terminal shell session running inside a target container.

```mermaid
sequenceDiagram
    autonumber
    participant UI as Browser Web App (Vue 3)
    participant Gateway as API Gateway (Echo / WebSocket upgrade)
    participant Docker as Docker SDK / Socket

    Note over UI, Gateway: Custom WebSocket subprotocol auth
    UI->>Gateway: Connect ws://host/ws/shell/:id with subprotocol ["lighthouse-auth", "JWT_TOKEN"]
    Gateway->>Gateway: Extract token from Sec-WebSocket-Protocol headers
    Gateway->>Gateway: Verify JWT validity, role template, and allowed_containers regex
    alt Invalid Auth
        Gateway-->>UI: HTTP 401 Unauthorized / Connection Close
    else Authorized
        Gateway->>Gateway: Set upgrade header: Sec-WebSocket-Protocol = "lighthouse-auth"
        Gateway->>UI: Upgrade HTTP to WebSocket Connection
        Gateway->>Docker: Execute exec command (/bin/sh) & Attach stdin/stdout TTY
        Docker-->>Gateway: Connection socket (attachResult.Conn & Reader)
        par Thread 1: Read WS, Write Shell Input
            loop UI text input
                UI->>Gateway: Send WebSocket Message (binary/text)
                Gateway->>Docker: Write bytes to attachResult.Conn
            end
        and Thread 2: Read Shell Output, Write WS
            loop Shell output streaming
                Docker->>Gateway: Read buffer from attachResult.Reader
                Gateway->>UI: Send WebSocket text frame
            end
        end
    end
```

