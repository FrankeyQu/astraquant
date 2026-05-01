# E2E 联调报告：控制面与订单预览闭环

日期：2026-05-02
分支：`codex/e2e-control-loop`
基线：`707ba87 merge: integrate web control console`

## 结论

本轮已跑通本地 Go 后端到 Web 代理的核心控制链路：

- `GET /api/traders` 可读取 manager 配置中的交易员。
- `POST /api/traders/:traderId/start|pause|resume|stop` 可完成 control-plane 状态流转。
- `GET /api/traders/:traderId/status` 可读到最新运行态、暂停原因和 correlation id。
- 未知 trader 会被安全拒绝，不会排队、不提交订单。
- `POST /api/orders/preview` 可生成 preview-only 结果，明确 `submitted=false`。
- `POST /api/decisions/:decisionId/approve` 当前返回 `not_implemented`，不会排队或提交订单。
- Web `GET/POST /api/nof1/*` 代理可转发到本地 Go 后端。

当前还没有形成“AI 决策 -> 审批 -> 真实执行 -> 审计落库 -> 订单 read model”的完整闭环。原因是决策审批队列尚未实现，且本地未接入 Postgres/Redis，因此 `/api/orders` 和 `/api/audit-events` 返回 `audit_repo_unavailable`。

## 本轮修复

修复了两个 HTTP contract 解码问题：

- `TraderControlRequest.effective_until` 原 tag 是 `omitempty`，Go Zero HTTP 校验仍视为必填，导致 `start/resume/stop` 请求 400。已改为 `json:"effective_until,optional"`。
- `OrderPreviewRequest` 复用 `Order` 类型，但 `id/trader_id/status` 等响应字段被 HTTP 校验成请求必填；同时 `risk_context interface{}` 不能接收 JSON object。已将预览输入所需的可选字段标成 `optional`，并将 `risk_context` 收窄为 `map[string]interface{}`。

## 启动命令

Windows 本地启动后端需要规避 cgo 编译问题，并补齐当前配置的必填环境变量：

```powershell
cd D:\PRJ\lianghua\nof0-e2e\go
$env:CGO_ENABLED = "0"
$env:Cache__0__Tls = "false"
$env:ZENMUX_API_KEY = "e2e-dummy-key"
$env:ASTRAQUANT_HYPERLIQUID_PRIVATE_KEY = "0x0000000000000000000000000000000000000000000000000000000000000001"
go run nof0.go
```

新增 E2E profile 后，可以改用 paper-only 配置启动；该方式不需要 `ZENMUX_API_KEY` 或 Hyperliquid 私钥：

```powershell
cd D:\PRJ\lianghua\nof0-e2e\go
$env:CGO_ENABLED = "0"
go run nof0.go -f etc/nof0.e2e.yaml
```

该 profile 使用：

- `manager.e2e.yaml`：两个 `paper` trader，`auto_start=false`
- `exchange.e2e.yaml`：只加载 `paper_trading` sim provider
- `llm.e2e.yaml`：dummy LLM endpoint/key，不会误用真实模型 key
- `market.e2e.yaml`：保留 Hyperliquid testnet market provider，启动时不需要密钥

Web 代理联调：

```powershell
cd D:\PRJ\lianghua\nof0-e2e\web
npm ci
npx next dev --webpack -p 3003
```

## API Smoke 结果

后端直连 `http://127.0.0.1:8888/api`：

- `GET /traders`：200，返回 2 个 testnet trader。
- `POST /traders/trader_aggressive_short/start`：200，`accepted=true`，`control_state=running`，`control_plane_only=true`。
- `POST /traders/trader_aggressive_short/pause`：200，`accepted=true`，`control_state=paused`，`paused_until=2026-05-02T04:00:00Z`。
- `POST /traders/trader_aggressive_short/resume`：200，`accepted=true`，`control_state=running`。
- `POST /traders/trader_aggressive_short/stop`：200，`accepted=true`，`control_state=stopped`。
- `POST /traders/unknown_trader/start`：200，`accepted=false`，`status=rejected`。
- `POST /orders/preview`：200，`accepted=true`，`status=preview_only`，`submitted=false`。
- `POST /decisions/e2e-decision-004/approve`：200，`accepted=false`，`status=not_implemented`。
- `GET /orders`：200，`status=not_available`，`source=audit_repo_unavailable`。
- `GET /audit-events`：200，空数组，`source=audit_repo_unavailable`。

Web 代理 `http://127.0.0.1:3003/api/nof1`：

- `GET /traders`：200，成功透传本地 Go 后端数据。
- `OPTIONS /traders/trader_aggressive_short/start`：200，允许 `GET,POST,OPTIONS`。
- `POST /traders/trader_aggressive_short/start`：200，成功透传控制请求。

## 测试

已通过：

```powershell
cd D:\PRJ\lianghua\nof0-e2e\go
$env:CGO_ENABLED = "0"
go test ./internal/logic ./internal/types ./pkg/manager ./pkg/repo
```

结果：

- `internal/logic`：通过
- `internal/types`：无测试文件
- `pkg/manager`：通过
- `pkg/repo`：通过

未完全通过/未完成：

- `go test ./...` 在 Windows 上仍会遇到既有 cgo/secp256k1 与路径分隔符测试问题。
- `npm run build` 在新 worktree 中会长时间卡住无错误输出，需要后续单独排查 Next 16 + Windows worktree 构建问题。
- Next dev 的页面入口 `/`、`/leaderboard` 在本机请求会挂起，但 `/api/nof1/*` 代理正常。

## 遗留风险

- 当前控制面只记录状态，不会启动真实 manager loop，也不会提交订单。这是安全的，但还不是完整交易闭环。
- 本地启动需要 dummy LLM key 和 dummy Hyperliquid private key；缺少专门的 dev/test profile。
- 未接入 DB/Redis 时，审计事件和订单 read model 不可用，无法验证落库链路。
- 前端页面入口存在本地运行卡顿，控制台 UI 还没有做浏览器点击级验证。

## 下一步建议

1. 增加 `etc/nof0.e2e.yaml` 或 dev profile，让本地 smoke 默认使用 `paper_trading` / dummy LLM / 无 Redis，无需真实交易所密钥。
2. 修复 Next build/dev 页面入口在 Windows worktree 下卡住的问题。
3. 实现决策审批队列或 manager command queue，把 `approve/reject` 从 `not_implemented` 推进到可审计的状态机。
4. 接入本地 Postgres/Redis docker compose，验证 audit event 写入和 orders read model。
5. 用浏览器自动化补一条 UI 点击 smoke：TradersPanel -> 选择 trader -> start/pause/resume/stop -> order preview。
