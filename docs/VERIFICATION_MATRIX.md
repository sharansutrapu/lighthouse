# Verification Matrix

This matrix records the 2026-10-31 to 2026-11-01 verification work. It
describes executed tests, not a guarantee that every possible input,
deployment, or vulnerability has been eliminated.

## Test environments

- Go unit and regression tests run in Linux containers with Go 1.26.
- The real integration fleet has three independent Docker-in-Docker daemons,
  a PostgreSQL-backed hub, two spokes using a TLS reverse proxy, and a
  standalone instance using SQLite. The standalone instance shares only the
  dedicated hub test daemon, not the developer's Docker socket.
- Chromium browser checks use the real test APIs at 1440x900 and 390x844.
  External Google Fonts requests are stubbed; application images are checked
  for successful loading.
- Integration resources use the `lighthouse-verification` Compose project.
  Host ports bind only to loopback. Credentials in the Compose file are test
  fixtures and must never be used for deployment.

## Coverage by area

| Area | Verification |
| --- | --- |
| CPU calculations | Exact first-sample, one/multiple-core, fractional, idle, zero-delta and counter-reset assertions |
| Memory/network/disk metrics | Working-set subtraction and exact interface/device accumulation; persisted per-node values compared to API history |
| System history | SQLite and PostgreSQL paths; node isolation; exact numeric fields; date ranges; invalid duration fallback; empty results |
| Container history | Container/node isolation, UTC date filtering, bounded results and explicit database errors |
| Hub/spoke transport | Real TLS-connected spokes; capability negotiation; correlated RPC; wrong-node response rejection; cancellation and stream cleanup |
| Failure recovery | Stop one spoke while other nodes remain usable; reject offline actions; restart hub/spoke; retain PostgreSQL history |
| Container lifecycle | Real local/remote start, stop and restart; SDK error cases; permission denial; owner-specific dispatch |
| Logs | Real TTY and multiplexed streams, multiline/partial-line parsing, browser disconnect cleanup and reconnection state |
| Shell | Real local/remote terminal commands; browser commands in both modes/viewports; delayed-profile initialization regression |
| Images, volumes, networks | Real inventory across engines; identical-name resources on separate nodes; targeted deletion/pruning; Docker error cases |
| Vulnerability scans | Real Trivy scans in standalone and spoke modes, second scans/cache reuse, database/API persistence and visible failure records |
| Scan authorization | Permission and container visibility enforced independently; malformed references and remote dispatch regressions |
| Inspect confidentiality | Secrets redacted in parsed Docker data, duplicate raw JSON and MCP output; non-secret values preserved |
| Authentication/tokens | Required password change, unauthenticated denial, JWT/PAT tests, hashed storage and revocation |
| Authorization | Restricted-user cross-node denial, forbidden WebSocket access, live permission changes and current user/team health permissions |
| Users/teams/settings/audit | Existing handler CRUD/error tests plus browser data-loading checks; masked settings round trips |
| MCP | Tool authorization, token/context injection, inspect redaction and SSE fixture synchronization |
| Alerts | Rule matching, thresholds, cooldown/grouping, routing, delivery errors, timer cancellation and race checks |
| Email/webhooks | Mock SMTP and implicit-TLS success/failure stages, header handling, webhook SSRF/address checks and delivery parsing |
| Backup/archival | Existing S3/GCS/Azure provider tests; local S3-compatible upload; serialization failures; archival window preservation and concurrency |
| GitOps | Repository validation, clone/fetch/compose failure paths, polling and cancellation lifecycle; browser view loading |
| CLI/database | Existing CLI/reset-password/migration tests plus real PostgreSQL integration and SQLite standalone startup |
| Frontend | Nine transport/config tests and 68 Chromium cases across hub/standalone and desktop/mobile; JS/Vue errors, overflow, navigation, filtering and theme controls |
| Build/security | Frozen-lockfile builds, Go vet, race checks, dependency audits, image high/critical scan and workflow validation |

## Defects exposed by stronger tests

- RFC3339 string comparisons missed SQLite history rows; invalid hour values
  could remove the system-history time filter.
- Local container history mixed records from another node with the same ID
  and returned success after database failures.
- Docker inspect's duplicate `Raw` JSON bypassed environment redaction; MCP
  inspect also returned unredacted environment values.
- Detailed system history lacked server-side health permission enforcement.
- Container-permission revocation remained stale in the compiled-pattern
  cache. Generation invalidation now prevents stale in-flight cache fills.
- Local scan permission did not enforce the caller's container visibility.
- Shell initialization could reject an authorized user before the profile
  request completed.
- Archival runs raced on their window timestamp and could report success
  after failure, advancing past unarchived data.
- Delayed alert deliveries survived shutdown/test cleanup, and SSE/GitOps
  tests used unsynchronized waits instead of completion signals.

## Repeatable commands

Prerequisites: Docker with Linux containers, Node 22.12+, pnpm 10.34.9, and
approval to run privileged Docker-in-Docker test daemons. Install browser
dependencies first:

```text
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend exec playwright install chromium
pnpm --dir frontend test
node tests/integration/run.mjs --phase=unit
node tests/integration/run.mjs --phase=race
node tests/integration/run.mjs --allow-privileged
```

On Linux CI, use `playwright install --with-deps chromium`. Set
`DOCKER_BINARY` if Docker is not on PATH. The full integration command builds
the image, runs all fleet/recovery/scan/browser phases, and cleans up its own
containers, network and volumes in `finally`. `--keep` retains the test fleet
for debugging. Never substitute production endpoints or mount a host Docker
socket into these test services.

Logs, coverage and phase results are written under
`tests/integration/artifacts/`; browser results, screenshots and failure
traces are under `frontend/test-results/` and `frontend/playwright-report/`.
These generated artifacts are ignored by Git. Go gates fail if any test is
skipped; Playwright forbids focused-only tests and does not retry failures.

CI runs the fleet before publishing and uploads the verification artifacts.

## Compatibility and operational limits

- Detailed system history/storage/info now require admin or the current
  user/team `can_view_system_health` permission. Previously accepted callers
  without that permission now receive HTTP 403.
- Local scans now return errors for nonexistent or unauthorized containers
  before starting background work. Authorized scans remain asynchronous.
- Inspect responses retain their shapes but redact secret values in every
  returned representation. Clients must not depend on reading secrets there.
- Archival upload/serialization failures are now reported as failures and do
  not advance the archive window. No database migration is required.
- The real fleet is isolated within Docker Desktop, not deployed on separate
  physical machines. Large-fleet load, long-duration soak, arbitrary proxies,
  and production network conditions are not certified by these tests.
- Live Google OAuth, customer SMTP inbox delivery, and AWS/GCS/Azure account
  behavior require appropriate credentials and environments. Their local
  handler/provider tests are executed, but live external-service success is
  not claimed.
- Remote GitOps deployment, remote per-container live charts, and full remote
  historical log download remain existing capability limitations. Tests do
  not turn unsupported features into implemented ones.
- Dependency scans detect known advisories, not every security defect.
  `GO-2026-5932` remains a module-level advisory for unused OpenPGP packages;
  the unstripped-binary/package checks distinguish that from an imported path.