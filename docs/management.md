# Management console and operations guide

[English](management.md) | [简体中文](management.zh-CN.md) | [Tiếng Việt](management.vi.md) | [Italiano](management.it.md)

This guide documents this fork's management console. It uses only the OpenCode Zen free anonymous lane, and the service, inference API, WebUI, and management API share a single port, **8080**. See the [English README](../README.md) for startup and inference API examples, and [config.example.json](../config.example.json) for a configuration starting point.

| Path | Purpose |
| --- | --- |
| `/v1/*` | Inference API (OpenAI/Anthropic compatible). |
| `/web` | Management WebUI assets. |
| `/api/*` | Management JSON endpoints, protected by session/CSRF. |

This build has no upstream key pools, Go/Zen OAuth, quota views, or model rotation. Those fields may remain in an old file but are ignored and removed on save; their pages and endpoints no longer exist.

## Startup and access

On Linux, use Go 1.24 or later and build with Bash from the project directory:

```bash
if [ ! -f config.json ]; then
  cp config.example.json config.json
fi
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
```

Before starting, edit the configuration locally: replace `server_keys` and `webui.password`. No upstream key is needed; `anonymous` is always `true`, and exits come from `proxies`, `proxyfile`, or `warp`. The password must contain at least 10 characters; the example administrator name is `admin`. For local management, set `listen` to `127.0.0.1:8080`; the example's `0.0.0.0:8080` listens on all interfaces.

```bash
./opencode2api -config config.json
```

On native Windows, build with `go build -trimpath -o opencode2api.exe ./cmd/opencode2api`, then run `.\opencode2api.exe -config .\config.json` in PowerShell. The README documents Docker/Compose volume initialization, configuration imports, ports, and images. Manage the configuration actually used by the running service; editing a host copy without importing it again does not change the active volume configuration.

The configuration directory must be writable. On first startup, `webui.password` is migrated to an Argon2id `password_hash`, and the program attempts to remove the backup containing the bootstrap plaintext password. Other credentials are not encrypted by this migration.

## Pages and management authentication

| Page | Address | Purpose |
| --- | --- | --- |
| Overview | `/#overview` | Requests, tokens, upstream attempts, and runtime resources. |
| Diagnostics | `/#diagnostics` | Model routes, Playground, and live logs. |
| Client access | `/#access` | Client connection settings. |
| API key management | `/#api-keys` | Local key lifecycle and usage. |
| Configuration | `/#config` | Server keys, proxies, timeouts, anonymous lane, WARP, and logging. |
| Account security | `/#account` | Change the administrator username and password. |

The UI supports Simplified Chinese, English, Vietnamese, and Italian. Language and theme choices are stored in the browser. The old `#playground` and `#logs` links open Diagnostics; `#availability` opens Overview.

Administrators log in with a username and password and receive an `HttpOnly`, `SameSite=Strict` session cookie. Every management API except login requires a valid session. Writes also require `X-CSRF-Token`; when an `Origin` header is present, it must match the service's origin. Login and session responses provide `csrf_token`. Local `server_keys` do not grant management access.

Sessions default to 720 minutes, with a range of 5–10,080 minutes. Changing administrator credentials or restarting the process invalidates existing sessions; the account update endpoint requires the current password again. Login is throttled. Playground allows at most 12 requests per direct client per minute. By default the service does not trust `X-Forwarded-For`, so clients behind a reverse proxy share its limits. Listing the proxy's address in `webui.trusted_proxies` (IP or CIDR) makes the rightmost `X-Forwarded-For` entry each client's identity for those limits and restricts `X-Forwarded-Proto` handling to trusted peers; the proxy must set or append that header itself. An HTTPS reverse proxy must also pass the original Host header and set `X-Forwarded-Proto` to `https`, because it affects the cookie's Secure flag and origin checks. Management responses reached over HTTPS carry HSTS and `X-Robots-Tag: noindex`.

## Saving and reloading configuration

`GET /api/config` returns the runtime configuration, safe IDs/masked values, effective listen address, and fields requiring restart. A key/proxy update uses one of these representations:

```json
{"id":"EXISTING_FINGERPRINT"}
```

```json
{"value":"NEW_VALUE"}
```

`PUT /api/config` accepts a complete configuration form rather than a partial PATCH. Omitted key/proxy lists may be treated as empty. Submit the existing value's ID to retain it. Saving through the UI preserves advanced configuration that the form does not edit. Administrator passwords have a dedicated endpoint; the anonymous flag and `prefer` are fixed by the backend, and values submitted by the browser are ignored.

Saving validates the candidate, builds its runtime, persists the configuration, and switches to it. If validation or configuration persistence fails, the existing runtime continues serving. Configuration files allow `//` and `/* ... */` comments; API bodies require standard JSON. Unknown fields, repeated top-level fields, and invalid values are rejected. Saving normalizes the file and removes comments.

Server keys, proxies, timeouts, upstreams, models, reasoning, and logging apply to new requests without a restart. Changes to `listen` and `webui.enabled` require restart; the UI shows configured and effective listeners. After editing the file manually, select the disk reload action or call `POST /api/config/reload`. The program does not watch the file automatically.

`POST /api/config/reveal` requires a management session, CSRF, and password verification again. It can return server keys and proxy values, but never the password or password hash. A failed re-verification shares the login backoff, and successful reveals are audited by key fingerprint. Use it only when copying a value is necessary, and keep its output out of logs and shared material.

Common management defaults:

| Setting | Default / range |
| --- | --- |
| `webui.enabled` | Disabled in built-in defaults; enabled in the example. |
| `logging.level` | `info`; supports `debug`, `info`, `warn`, and `error`. |
| `logging.ring_size` | `2000`; range 100–50,000. |
| `logging.dump_request_bodies` | `false`; also requires debug logging, capped at 64 KiB per prepared body. |
| `models.active_probes` | `false`; real traffic supplies default availability observations. |

## Models and free availability

Discovery still combines catalog and price metadata, but routing accepts only free models. A model is free when its ID contains `free` case-insensitively, or when non-deprecated metadata reports zero input and output cost; eligibility does not guarantee upstream acceptance.

Free-model availability is learned from real requests by default. Only explicit model-not-found/retired evidence can disable a model. Statuses 401, 403, 429, generic 4xx/5xx, timeouts, transport errors, and malformed replies alone do not establish retirement. Manual restore keeps a model routable and excludes it from automatic probes until automatic control is restored.

Background synthetic inference requires `models.active_probes: true`. The scheduler checks due records every minute and processes models sequentially. Healthy/inconclusive models are normally checked after one hour; confirmed unavailable models after 24 hours. Each check has a 60-second timeout. Probes use the anonymous lane and WARP exits when enabled; models without a usable exit are skipped. Active probes consume anonymous allowance without changing production proxy cooldowns or request statistics.

| Management endpoint | Purpose |
| --- | --- |
| `GET /api/models/availability` | Free-model state and reasoning-probe results. |
| `POST /api/models/probe` | Mark automatically controlled models due and wake the scheduler; does not override `active_probes`. |
| `POST /api/models/restore` | `{"model":"MODEL_ID"}` restores a model and puts it under manual control. |
| `POST /api/models/manual` | `{"model":"MODEL_ID","manual":true}`; `false` restores automatic control. |

`reasoning.effort: "auto"` uses a recently validated effort ceiling for free anonymous models. New ceilings are not actively learned with active probes disabled; manual control also pauses automatic probes. Observations expire after two hours. Explicit client effort always takes precedence. Requests naming a paid model are rejected rather than falling back to another lane.

## Local API keys

These endpoints manage the gateway's `server_keys`; they are unrelated to any upstream credential. Creation returns the complete new key; lists contain only fingerprint, name, masked value, state, timestamps, and usage. The UI can copy the key and client environment variables after creation.

| Endpoint | Request and result |
| --- | --- |
| `GET /api/api-keys` | Safe key list. |
| `POST /api/api-keys` | `{"name":"client-a"}` generates a key; add `value` for a custom key. Returns 201 on success. |
| `PATCH /api/api-keys/{id}` | `{"name":"client-b","enabled":false}`; only fields being changed are required. |
| `DELETE /api/api-keys/{id}` | Delete a local key. |

Generated keys contain 192 random bits. Custom keys accept 16–256 letters, digits, `.`, `_`, or `-`. Names accept 1–80 characters, and the creation endpoint allows at most 200 keys. At least one key must remain enabled. Disabled/deleted keys receive 401 on new requests; in-flight requests may already have authenticated.

`server_key_metadata` stores names, enabled state, and creation times by fingerprint. Existing `server_keys` remain compatible. Usage shows the last hour and process lifetime, attributing requests and tokens to the key that authenticated them. Playground is recorded separately under `playground`, without charging its borrowed local key. Statistics survive hot reloads and reset on restart. There are no per-key quotas, model permissions, or expiration dates.

## Proxies and WARP

Configuration supports bulk import of inline proxies, individual checks, and staged removal. Manage `proxyfile` entries in their source file; relative paths are resolved against the configuration directory. Removal remains a draft until the complete configuration is saved.

`POST /api/proxies/check` requires a session and CSRF. Submit exactly one of these requests:

```json
{"id":"SAVED_PROXY_ID"}
```

```json
{"value":"https://proxy.example:8443"}
```

Saved proxy IDs come from `GET /api/config`; `value` is for drafts. Each check times out after 20 seconds. The UI runs at most four concurrently; the server accepts at most eight and returns 429 for excess checks without queuing. A completed check returns HTTP 200 with `ok`, `state`, `reason`, `latency_ms`, and `status` when available. Authentication, CSRF, and input errors use their corresponding HTTP statuses.

A check only determines whether the selected proxy can reach the configured anonymous upstream. An isolated client sends an unauthenticated GET through that proxy to `upstream.anonymous` plus `/v1/models`. It sends no key or `public` credential; performs no inference; follows no redirects; does not use WARP; and does not change production health or proxy cooldowns.

| `state` | Meaning |
| --- | --- |
| `available` | Upstream 200–399 or 401. For 401, `authentication_required` proves reachability. |
| `unavailable` | Confirmed proxy-side connection refusal/DNS-not-found (`proxy_unreachable`) or proxy tunnel 407 (`proxy_authentication_required`) on this check. |
| `limited` | 429; does not establish a broken proxy. |
| `rejected` | Other upstream refusal or proxy tunnel rejection. An origin 407 is not a proxy-authentication failure. |
| `inconclusive` | Timeout, unlocalized transport failure, or missing prerequisites; not proof of a broken proxy. |

For bulk cleanup, only `unavailable` is a confirmed-unavailable filter for that check. Rate limits, rejections, and timeouts do not establish permanent failure. Production inference can still fail because of model, exit, or policy constraints. Multiple exits are the main reliability lever in this build; configure several proxies or enable the WARP pool.

WARP is disabled by default. When enabled, anonymous requests and their discovery/probe paths can use an in-process tunnel as an exit; the runtime selects among static proxies and WARP exits by health. It needs outbound Cloudflare UDP on port `2408`, without TUN or routing-table changes. `warp.pool_size` is a concurrency-driven exit ceiling of 1–32 (default 2). `warp.stack` defaults to `ipv6`, with `ipv4`/`dual` also available. Initialization failure rejects startup or the candidate configuration without silently falling back to proxies.

Exit statuses 403/429 can trigger retirement and replacement, with retries still bounded by their budget. A new exit does not guarantee additional allowance. IPv6 `/64` and IPv4 `/32` egress blocks are deduplicated/cooled. `warp.source_ipv6` requires the IPv6 stack and a prefix of `/64` or shorter. Linux uses `IPV6_FREEBIND`; Windows requires the selected source addresses to be assigned already. This setting does not configure host networking. `/healthz` reports WARP state/counts only; `resources.warp` in `/api/monitor` also contains exit information.

## Playground, logs, and monitoring

Playground invokes the same gateway in-process through `POST /api/debug/inference`, forcing non-streaming output. This build only uses the anonymous lane:

```json
{"protocol":"chat","key":{"mode":"auto"},"request":{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}}
```

Optional `relay` pins a known proxy index, for example `{"index":0}`. Use an index from the current diagnostic catalog. Diagnostics preserve production failure counts and proxy health, but still make real anonymous requests and appear in monitoring. A completed management call wraps the result in HTTP 200, with the actual outcome in `ok`, `http_status`, `request_id`, `route`, and `response`.

Logs are available at `GET /api/logs?after=SEQUENCE&limit=COUNT` and `GET /api/logs/stream`. SSE resumes using `Last-Event-ID` or `after`; expired/missed events produce `gap`, and evicted data cannot be replayed. The server ends a log stream when its session becomes invalid.

`GET /api/monitor` provides aggregate requests, tokens, upstream attempts, and resources. `GET /api/debug/models` provides routes, model metadata, proxies, and the latest diagnostic result. Tokens use only upstream-reported usage, without estimating missing values. Input includes cache reads/writes; cached tokens separately count reads. `usage.server_keys` groups local-key usage, with `playground` separate.

Request outcomes describe the complete inference. Failed streams count as `stream_error` even after HTTP 200; client cancellation is `client_canceled`. An upstream attempt measures the exchange only until response headers arrive, so its success rate and latency are not the success rate and latency of the complete answer.

## Persistence, health, and security

| Data | Location and lifecycle |
| --- | --- |
| Configuration/previous version | `config.json`, `config.json.bak`; contain sensitive information. |
| Model/price caches | `config.json.models.catalog.json`, `config.json.models.dev.json`. |
| Free-model availability | `config.json.<upstream-fingerprint>.availability.json`, isolated by upstream. |
| Sessions, token totals, log ring, latest Playground | Process memory; cleared on restart. |
| Request/attempt details | Last hour, capped at 10,000/20,000 records; APIs return at most 500 of each. |
| Durable logs | JSON on stdout; collect and retain securely yourself. |

Prefixes use the actual `-config` path. Processes do not share sessions, monitoring, or in-memory counters. Avoid concurrent writes to the same state files.

`GET /healthz` shares the API port, requires no authentication for readiness, and triggers no upstream check or monitoring count. It returns 503 while the catalog is pending/empty, WARP has no active exit, or required proxies have no healthy node. A usable stale catalog may return 200 with `degraded`; staleness means twice the refresh interval, at least 60 seconds. Readiness does not guarantee the next inference. Without a server key the response contains only state and readiness; a valid server key also receives the resource summary (catalog state, credential counts, proxy health, WARP topology), without secret values or proxy addresses.

Restrict local use to loopback. Network management needs HTTPS and access controls; the program does not terminate TLS itself, and a listener beyond loopback is logged as a public-listener warning at startup. Configuration/backups contain local keys and proxy passwords: on Unix startup narrows a loose config file to owner-only, while Windows needs an explicit ACL; exclude them from source sharing. Even redacted, truncated request-body dumps can contain private conversations; enable them only briefly when needed. Do not attach full configuration, reveal output, cookies, authentication headers, or real inference content to troubleshooting reports.

Configuration saves and reloads, account changes, secret reveals, and API key creation, updates, and deletion emit non-secret audit events to the stdout log. They carry client IP, key fingerprints, and restart fields, never plaintext keys or passwords.

## Verification and maintenance

Local development checks:

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
for file in webui/js/*.js webui/js/pages/*.js; do
  node --input-type=module --check < "$file" || exit 1
done
```

Normal deployment acceptance should verify:

- `http://127.0.0.1:8080/web` opens and completes management login and CSRF.
- `http://127.0.0.1:8080/healthz` reports readiness; `/v1/models` lists free models with a local key.
- Every management route lives under `/api`, and nothing listens on `8081`.
- Configuration saves and listener restarts, key disable/restore, model discovery, the three message protocols, and SSE errors work.
- A configured proxy/WARP exit completes one real free inference; static checks do not replace this.

Before upgrading, securely back up the executable, active configuration, and state. Stop the process before replacing the executable; rollback needs compatible configuration and state. Compose users must preserve the actual files in the volume. Keep credentials, logs, runtime state, and local verification artifacts out of source control.
