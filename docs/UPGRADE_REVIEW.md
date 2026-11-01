# Upgrade and Reliability Review

Review dates: 2026-10-31 to 2026-11-01. Baseline: `31c0be2`, pulled once from
`main` before review.

## Applied fixes

- Close only the affected node's browser log and shell streams on spoke
  disconnect or replacement; reject cross-node stream output.
- Isolate spoke writes by node, enforce write deadlines, and reconnect after
  failed writes. Send inventory immediately and join the cancelled inventory
  worker before reconnecting.
- Accept HTTP(S), WS(S), trailing slashes, and proxy prefixes in `HUB_URL`.
  Keep bearer authentication and legacy query-token compatibility on the hub.
- Preserve healthy spoke inventories when local Docker or a resource RPC
  fails, and identify incomplete results in the API and UI.
- Persist and forward failed spoke scan results instead of leaving old scan
  history indistinguishable from a successful latest run.
- Handle TTY and multiplexed standalone logs; cancel quiet Docker streams
  when the browser disconnects.
- Split multiline and partial log messages, bound the log buffer, clean up
  stats retries, and display connection state without restarting on unchanged
  inventory updates.
- Prevent overlapping shared inventory polls and cross-session stale results;
  display actionable server errors for container actions.
- Fix the missing container-action loading binding and duplicate mobile
  log/shell header logo. Add development API/WebSocket proxy routing.

## Security and tooling upgrades

| Component | Before | After |
| --- | --- | --- |
| Echo | 4.15.4 | 4.16.3 |
| `golang.org/x/net` | 0.58.0 | 0.62.0 |
| `golang.org/x/crypto` | 0.55.0 | 0.58.0 |
| `go-jose/v4` | 4.1.4 | 4.1.7 |
| `x/sync`, `x/sys`, `x/text` | 0.22.0, 0.47.0, 0.41.0 | 0.24.0, 0.49.0, 0.43.0 |
| Vite | 8.2.2 | 8.3.5 |
| `source-map-js` | 1.2.1 | 1.2.2 |
| Frontend build image | Node 20 | Node 24 on Alpine 3.24 |
| Runtime image | Alpine 3.21 | Alpine 3.24 |
| pnpm | Unpinned installer | 10.34.9 |

Go's five `x/net` findings and seven frontend advisories were addressed.
The image scan also identified five Echo advisories, including critical
`CVE-2026-118264`; the Echo update addresses these. Alpine 3.21 reaches its
published end-of-support date on 2026-11-01.

CI uses verified stable releases of checkout/setup-go/setup-node and Docker
actions. Publishing requires Go tests, repository-wide race checks, frontend
tests, frontend audit/build, an unstripped Go binary scan, the real
fleet/browser integration job (privileged Docker-in-Docker), and an amd64
runtime-image scan for high/critical advisories. The image scanner is pinned
by digest.

## Compatibility

- **Tooling change:** local frontend development now requires Node 22.12 or
  newer; Node 24 LTS is recommended. Docker builds provide their own Node.
- No database migration, API route removal, credential reset, or hub/spoke
  protocol-version change is introduced by this review.
- Upgrade the hub first, then spokes, to benefit from capability negotiation
  and remote fixes. Old spokes do not gain new capabilities until upgraded.
- Keep `NODE_NAME` stable and unique and use the same strong `HUB_TOKEN` on
  both sides. Duplicate node names cause connection replacement loops.
- Use `https://` or `wss://` outside a trusted local network. Embedded URL
  credentials and URL fragments are rejected; bearer secrets belong in
  `HUB_TOKEN`.
- In hub mode, HTTP 200 inventory responses may be partial. Clients should
  inspect `X-LightHouse-Unavailable-Nodes`; the included UI does this.
- Remote per-container live charts and full historical log download remain
  capability-limited. GitOps execution remains local to the hub.

## Further upgrades to schedule

These versions were available during the review but were not required to fix
the verified defects. Upgrade them in focused batches with their integration
tests, rather than combining API migrations with incident fixes.

| Component | Current | Available | Main verification needed |
| --- | --- | --- | --- |
| Vue / Vue plugin | 3.5.42 / 6.0.8 | 3.5.44 / 6.0.10 | UI and compiler regressions |
| Chart.js | 4.5.1 | 4.6.1 | Health/dashboard charts |
| Vue Router | 4.6.4 | 5.4.0 | Major-version routing/auth migration |
| pnpm | 10.34.9 | 12.16.1 | Major-version installer/lockfile migration |
| MCP SDK | 0.58.0 | 1.5.0 | Major-version SSE/tools/auth compatibility |
| Moby API / client | 1.55.0 / 0.5.1 | 1.56.1 / 0.6.2 | Docker API compatibility, exec/log streams |
| Google storage / API | 1.66.0 / 0.294.0 | 1.70.0 / 0.310.0 | Cloud credentials, backup/restore |
| Azure Blob SDK | 1.8.0 | 1.8.2 | Backup/restore and authentication |
| MinIO SDK | 7.3.0 | 7.4.1 | S3-compatible provider integration |
| PostgreSQL driver / pgx | 1.6.2 / 5.10.0 | 1.6.3 / 5.12.0 | PostgreSQL migrations/pooling |
| modernc SQLite | 1.57.0 | 1.61.0 | Persistence, migration, backup/restore |

## Verification and limits

The subsequent full-test pass is detailed in
[VERIFICATION_MATRIX.md](VERIFICATION_MATRIX.md), including real independent
Docker daemons, PostgreSQL, TLS-connected spokes, recovery, Trivy execution,
68 browser cases, newly discovered defects, and tightened authorization.

- All eight Go packages pass `go test ./... -count=1` (938 tests, none
  skipped); `go vet ./...` passes, including the `integration`-tagged package.
- All eight Go packages also pass `go test -race ./...`, including real
  authenticated WebSocket inventory, inspection, action RPC, and connection
  cleanup.
- Standalone TTY/multiplexed log and disconnect regressions pass repeatedly.
- Nine frontend transport/configuration tests pass. Production frontend and
  Docker builds succeed with the frozen pnpm lockfile.
- Browser checks include controlled error fixtures and real isolated
  standalone/hub APIs at 1440x900 and 390x844. They do not use production.
- Frontend package audit reports no known vulnerabilities. Go package and
  unstripped-binary scans report no affected imported packages or symbols.
- The final linux/amd64 image scan reports zero high/critical vulnerabilities
  in both Alpine 3.24.3 and the application binary, after the Echo upgrade.
- `GO-2026-5932` remains a module-level advisory for the unused, unmaintained
  `x/crypto/openpgp` packages. The stripped binary scanner reports wildcard
  symbols for it; package-graph and unstripped-binary checks distinguish this
  from an actual OpenPGP dependency. No advisory suppression was added.
- Real local hub/spoke logs, shells, actions, scans, and recovery are exercised.
  External SMTP delivery, customer cloud restore, and production rollout are
  not claimed. Multi-architecture publishing remains configured in CI; the
  local image build/scan targets linux/amd64.