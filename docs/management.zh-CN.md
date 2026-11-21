# 管理控制台与运行指南

[English](management.md) | 简体中文 | [Tiếng Việt](management.vi.md) | [Italiano](management.it.md)

本文说明本 fork 的管理控制台：只使用 OpenCode Zen 免费匿名通道，服务、推理 API、WebUI 与内部管理接口共用 **8080** 一个端口。服务启动与推理 API 示例见[简体中文 README](../README.zh-CN.md)，完整配置起点为 [config.example.json](../config.example.json)。

| 路径 | 用途 |
| --- | --- |
| `/v1/*` | 推理 API（OpenAI/Anthropic 兼容）。 |
| `/web` | 管理 WebUI 静态资源。 |
| `/api/*` | 管理端 JSON 接口，受 Session/CSRF 保护。 |

本构建不包含上游 Key 池、Go/Zen OAuth、额度查询与模型轮转：相关配置字段可以留在旧文件中，但会被忽略，保存时清除；对应的管理页面与接口已移除。

## 启动与访问

Linux 下使用 Go 1.24 或以上版本，在项目目录以 Bash 编译运行：

```bash
if [ ! -f config.json ]; then
  cp config.example.json config.json
fi
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
```

启动前在本机编辑配置：替换 `server_keys` 和 `webui.password`。无需任何上游 Key，匿名通道 `anonymous` 始终为 `true`；匿名出口由 `proxies`、`proxyfile` 或 `warp` 提供。密码至少 10 字符，示例管理员名为 `admin`。本地管理建议把 `listen` 设为 `127.0.0.1:8080`；示例的 `0.0.0.0:8080` 会监听所有接口。

```bash
./opencode2api -config config.json
```

Windows 原生方式为 `go build -trimpath -o opencode2api.exe ./cmd/opencode2api`，然后在 PowerShell 执行 `.\.\opencode2api.exe -config .\config.json`。Docker/Compose 的卷初始化、文件导入、端口与镜像指引见 README；管理操作需使用实际运行配置，而非仅编辑未重新导入的宿主副本。

配置目录须可写。首次启动会把 `webui.password` 迁移为 Argon2id `password_hash` 并尝试删除包含引导明文密码的备份；这不会加密其他凭证。

## 页面与管理认证

| 页面 | 地址 | 功能 |
| --- | --- | --- |
| 概览 | `/#overview` | 请求、Token、上游尝试和运行资源。 |
| 诊断 | `/#diagnostics` | 模型路由、Playground、实时日志。 |
| 接入指南 | `/#access` | 客户端连接参数。 |
| API Key 管理 | `/#api-keys` | 本地 Key 生命周期与用量。 |
| 配置中心 | `/#config` | 服务密钥、代理、超时、匿名通道、WARP 与日志设置。 |
| 账号安全 | `/#account` | 修改管理员账号和密码。 |

界面支持简中、英语、越南语、意大利语，语言与主题保存在浏览器。旧 `#playground`、`#logs` 会进入诊断，`#availability` 进入概览。

管理员通过用户名/密码登录，获得 `HttpOnly`、`SameSite=Strict` Session Cookie。所有管理 API 除登录均需有效 Session；写操作另需 `X-CSRF-Token`，带 `Origin` 时须与服务来源一致。登录/Session 响应提供 `csrf_token`。本地 `server_keys` 不能代替管理登录。

Session 默认 720 分钟，范围 5–10,080 分钟。修改管理员凭证或重启进程会使现有 Session 失效；改账号接口要求再次验证当前密码。登录有限流；Playground 每直连客户端每分钟最多 12 次。默认不信任 `X-Forwarded-For`，反向代理后的客户端共享对端限制；将代理接入地址列入 `webui.trusted_proxies`（IP 或 CIDR）后，`X-Forwarded-For` 最右侧地址成为各客户端的限流身份，且仅受信代理的 `X-Forwarded-Proto` 会被采纳，代理须自行设置或追加该头。HTTPS 反向代理还须透传原始 Host 头并正确设置 `X-Forwarded-Proto` 为 `https`，它参与 Cookie Secure 标记与来源校验。经 HTTPS 到达的管理响应会携带 HSTS 与 `X-Robots-Tag: noindex`。

## 配置保存与重载

`GET /api/config` 返回运行配置、安全 ID/掩码、实际监听地址及需重启字段。Key/代理更新使用以下一种表示：

```json
{"id":"EXISTING_FINGERPRINT"}
```

```json
{"value":"NEW_VALUE"}
```

`PUT /api/config` 接收完整配置表单，不是局部 PATCH；遗漏的 Key/代理列表可能被视为空列表。保留已有值时提交其 ID。使用页面保存可以保留未在表单中编辑的高级配置。管理员密码由专用接口管理；匿名开关与 `prefer` 由后端固定，浏览器提交的值会被忽略。

保存顺序为校验、构建候选运行时、持久化、切换。校验或配置持久化失败时旧运行时继续服务。配置文件允许 `//`、`/* ... */` 注释，API 请求必须是标准 JSON；未知字段、重复顶层字段与非法值会被拒绝。保存后文件规范化，注释消失。

服务密钥、代理、超时、上游、模型、reasoning 和日志设置热应用到新请求。`listen` 与 `webui.enabled` 必须重启；页面同时显示已配置值和实际监听。文件手动修改后须执行「从磁盘重载」或 `POST /api/config/reload`，程序不自动监视文件。

`POST /api/config/reveal` 要求管理 Session、CSRF 及再次验证密码，可返回服务密钥与代理值；不会返回密码或密码哈希。再次验证失败会与登录共用退避，成功 reveal 会按 Key 指纹记录审计日志。只在确实需要复制时使用，避免把结果写进日志或分享。

常用管理默认值：

| 配置 | 默认 / 范围 |
| --- | --- |
| `webui.enabled` | 内置默认关闭，示例开启。 |
| `logging.level` | `info`；支持 `debug`、`info`、`warn`、`error`。 |
| `logging.ring_size` | `2000`；范围 100–50,000。 |
| `logging.dump_request_bodies` | `false`；还需 debug 才输出，每个准备好的请求体最多 64 KiB。 |
| `models.active_probes` | `false`；真实流量提供默认可用性观察。 |

## 模型与免费可用性

模型发现仍结合上游目录与 OpenCode 价格目录，但路由只接受免费模型。免费判定为模型 ID 不区分大小写包含 `free`，或元数据未弃用且输入/输出价格均为零；判定不保证上游接受。

免费可用性默认来自真实请求。明确的模型不存在/下架错误才可禁用；401、403、429、普通 4xx/5xx、超时、连接错误和畸形响应不能单独证明模型下架。手动恢复保持模型可路由并退出自动探测，恢复自动控制后才重新排程。

`models.active_probes: true` 才启用后台合成推理，调度器每分钟检查到期项，逐模型执行。正常/无法判断的模型通常一小时复查，确认不可用的模型 24 小时复查；单次超时 60 秒。请求只走匿名通道，WARP 开启时使用其出口；没有可用出口就跳过。主动探测会消耗匿名额度，不改变生产代理冷却及请求统计。

| 管理接口 | 用途 |
| --- | --- |
| `GET /api/models/availability` | 读取免费模型状态和 reasoning 探测结果。 |
| `POST /api/models/probe` | 将自动控制模型标为到期并唤醒调度；不绕过 `active_probes` 开关。 |
| `POST /api/models/restore` | `{"model":"MODEL_ID"}`，恢复并交给手动控制。 |
| `POST /api/models/manual` | `{"model":"MODEL_ID","manual":true}`；`false` 恢复自动控制。 |

`reasoning.effort: "auto"` 使用近期成功验证的免费匿名强度上限。主动探测关闭时不会主动学习新上限；手动控制也暂停自动探测。观测结果两小时后失效。客户端明确 effort 始终优先。客户端明确指定付费模型时会被拒绝，而不是回退到其他通道。

## 本地 API Key

管理对象是网关 `server_keys`，与任何上游凭证都无关。创建接口在响应中返回完整新 Key，列表只返回指纹、名称、掩码、状态、时间和用量。页面支持创建后复制 Key 与客户端环境变量。

| 接口 | 请求与结果 |
| --- | --- |
| `GET /api/api-keys` | 安全列表。 |
| `POST /api/api-keys` | `{"name":"client-a"}` 自动生成；可另带 `value` 自定义，成功 201。 |
| `PATCH /api/api-keys/{id}` | `{"name":"client-b","enabled":false}`，可只提交需要修改的字段。 |
| `DELETE /api/api-keys/{id}` | 删除本地 Key。 |

自动 Key 使用 192 位随机数。自定义 Key 长度 16–256，字符为字母、数字、`.`、`_`、`-`；名称 1–80 字符，创建接口最多允许 200 个 Key。至少保留一个启用 Key。停用/删除对新请求立即返回 401，在途请求可能已完成认证。

`server_key_metadata` 按指纹保存名称、启用状态和创建时间，旧 `server_keys` 自动兼容。用量显示近一小时与当前进程累计，请求与 Token 按认证命中的 Key 归类。Playground 单独记为 `playground`，不计入借用的本地 Key。统计跨热重载保留、重启清零，不提供独立配额、模型权限或到期日。

## 代理与 WARP

配置中心支持批量导入内联代理、逐项检查及暂存移除。代理文件 `proxyfile` 的内容应在来源文件管理；相对路径以配置目录为准。移除仅为草稿，保存完整配置后才生效。

`POST /api/proxies/check` 要求 Session、CSRF，以下两种请求只能选一种：

```json
{"id":"SAVED_PROXY_ID"}
```

```json
{"value":"https://proxy.example:8443"}
```

已保存代理的 ID 来自 `GET /api/config`，`value` 用于草稿。单次超时 20 秒；页面最多四并发，服务端最多八并发，超出返回 429、不排队。正常完成返回 HTTP 200，包含 `ok`、`state`、`reason`、`latency_ms` 及可用时的 `status`；认证、CSRF 和格式错误使用各自 HTTP 状态。

检查只验证所选代理能否连接已配置匿名上游：隔离客户端通过该代理向 `upstream.anonymous` 加 `/v1/models` 发无认证 GET，不发送任何 Key 或 `public` 凭证，不推理、不跟随重定向、不用 WARP，也不更改生产健康或代理冷却。

| `state` | 解读 |
| --- | --- |
| `available` | 上游 200–399 或 401；401 的 `authentication_required` 只证明到站。 |
| `unavailable` | 本次确认代理侧拒绝连接/DNS 不存在（`proxy_unreachable`），或代理隧道 407（`proxy_authentication_required`）。 |
| `limited` | 429，不证明代理损坏。 |
| `rejected` | 上游其他拒绝或代理隧道拒绝；上游 407 不等于代理认证失败。 |
| `inconclusive` | 超时、无法定位的传输错误或前提缺失，不能认定损坏。 |

批量清理只应把本次 `unavailable` 作为确认不可用筛选，不能把限流、拒绝、超时都当成永久损坏；正式推理仍可能受模型、出口和策略限制。多出口是本构建提高成功率的主要手段，建议至少配置若干代理或启用 WARP 池。

WARP 默认关闭。开启后匿名请求及对应目录/探测可使用进程内隧道作为出口；静态代理与 WARP 出口由运行时按健康度选择。需要 Cloudflare UDP `2408`，不需要 TUN 或改路由表。`warp.pool_size` 为 1–32 的并发出口上限（默认 2），`warp.stack` 默认为 `ipv6`，也可 `ipv4`/`dual`。初始化失败拒绝启动或候选配置，不静默改走代理。

出口 403/429 可触发退役与补充，重试仍受预算限制；更换出口不保证额外额度。IPv6 `/64`、IPv4 `/32` 出口按网段去重/冷却。`warp.source_ipv6` 要求 IPv6 堆栈、`/64` 或更短前缀；Linux 使用 `IPV6_FREEBIND`，Windows 必须已分配所选源地址。该设置不会配置主机网络。`/healthz` 只提供 WARP 状态/数量，`/api/monitor` 的 `resources.warp` 另含出口信息。

## Playground、日志与监控

Playground 通过 `POST /api/debug/inference` 在进程内调用同一网关，强制非流式。本构建只使用匿名通道：

```json
{"protocol":"chat","key":{"mode":"auto"},"request":{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}}
```

可选 `relay` 用已知索引指定代理，例如 `{"index":0}`。索引必须来自当前诊断目录。诊断不修改生产失败计数或代理健康，但会发真实匿名请求并进入监控；管理响应成功封装为 HTTP 200，以 `ok`、`http_status`、`request_id`、`route`、`response` 表示实际结果。

日志位于 `GET /api/logs?after=SEQUENCE&limit=COUNT` 与 `GET /api/logs/stream`。SSE 支持 `Last-Event-ID` 或 `after` 续接；过期/丢失事件会有 `gap`，无法补回已淘汰数据。Session 失效后服务端结束日志流。

`GET /api/monitor` 提供聚合请求、Token、上游尝试与资源。`GET /api/debug/models` 提供路由、模型元数据、代理和最近诊断结果。Token 只使用上游报告，不估算缺失值；输入包括缓存读写，cached 单列缓存读。`usage.server_keys` 以本地 Key 分组，`playground` 单列。

请求结果表示完整推理：流失败即使 HTTP 200 也记为 `stream_error`，调用方取消为 `client_canceled`。上游 attempt 只测量收到响应头为止的交换，成功率和延迟不可当成整条回答成功率。

## 持久化、健康与安全

| 数据 | 位置与生命周期 |
| --- | --- |
| 配置/上一版本 | `config.json`、`config.json.bak`，包含敏感信息。 |
| 模型/价格缓存 | `config.json.models.catalog.json`、`config.json.models.dev.json`。 |
| 免费可用性 | `config.json.<上游指纹>.availability.json`，按上游隔离。 |
| Session、Token 累计、日志环、最近 Playground | 进程内存，重启清空。 |
| 请求/尝试详情 | 最近一小时，最多 10,000/20,000 条，接口各最多返回 500 条。 |
| 长期日志 | stdout JSON，须自行安全收集与保留。 |

文件前缀以实际 `-config` 路径为准。多个进程不共享 Session、监控或内存计数，避免并发修改同一状态文件。

`GET /healthz` 与 API 同端口，就绪检查无需认证，不触发上游检查或监控计数。目录未就绪/为空、WARP 无活跃出口、必要代理无健康节点时返回 503；可用但过期的目录可返回 200、`degraded`，过期阈值为两倍刷新间隔且至少 60 秒。就绪不等于下次推理一定成功。无 Key 时响应仅含状态与就绪；携带有效 server key 时额外返回资源摘要（目录状态、凭证计数、代理健康、WARP 拓扑），不暴露凭证或代理地址。

本地使用应限制为 loopback；网络管理须用 HTTPS 和访问限制。程序自身不终止 TLS，监听非 loopback 地址时会在启动日志中告警。配置/备份含本地 Key 与代理密码：Unix 启动时会把过宽配置文件收紧为仅所有者可读写，Windows 须自行设置 ACL；分享源码时排除。调试请求体输出虽脱敏且截断，仍可能含私人对话，应按需短时开启。不要把完整配置、reveal、Cookie、授权头或真实推理内容作为排错附件。

配置保存与重载、账户修改、密钥 reveal、API Key 创建/修改/删除都会向 stdout 日志写入非敏感审计事件，包含客户端 IP、Key 指纹与重启字段，不包含明文 Key 或密码。

## 验证与维护

本地开发检查：

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
for file in webui/js/*.js webui/js/pages/*.js; do
  node --input-type=module --check < "$file" || exit 1
done
```

正常部署验收需核实：

- `http://127.0.0.1:8080/web` 可打开并完成管理登录与 CSRF。
- `http://127.0.0.1:8080/healthz` 返回就绪状态；`/v1/models` 用本地 Key 可读取免费模型。
- 管理 API 全部位于 `/api`，不存在 `8081` 监听。
- 配置保存与监听重启、Key 停用/恢复、模型发现、三种消息协议与 SSE 错误正常。
- 配置的代理/WARP 出口可完成一次真实免费推理；静态核对不能替代这一验证。

升级前安全备份程序、实际配置和状态，停止进程后替换程序；回退需恢复兼容的配置与状态。Compose 用户注意保存卷中的实际文件。不要把凭证、日志、运行状态或本地验证产物加入源码。
