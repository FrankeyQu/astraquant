# AstraQuant 项目进度总览

更新时间：2026-05-01

## 当前项目定位

AstraQuant 是从 nof0 AI trading arena 演进出来的 AI-native crypto trading platform。当前重点不是旧的情报系统，而是构建一个安全优先的自动化交易研究平台：

- 模型可以生成交易决策。
- 任何可执行订单必须经过确定性风控、审批令牌、状态记录和最终执行前校验。
- 默认面向 paper/testnet，live trading 必须保持显式配置门禁。
- Web Console 以只读观测、审计追踪、控制平面状态展示为主，暂不打开真实下单入口。

活跃代码目录：

- `go/`：Go-Zero 后端、manager、exchange、policy、persistence、REST API。
- `web/`：Next/React 控制台。
- `go/docs/API_CONTRACT.md`：当前 REST contract。

## 已完成的关键工作

### 1. AstraQuant 基础重命名与治理

已完成：

- 根目录 README 已引入 AstraQuant 定位、安全说明和开发入口。
- `ARCHITECTURE.md`、`CONVENTIONS.md`、`TASK_SPLIT.md`、`THREAD_BRIEFS.md`、`WORKTREE_PLAN.md` 已形成多线程开发协作框架。
- 明确第一阶段开发边界：Secrets、Audit、Paper Exchange、API Contract、Web Console、DevEx。
- 仓库远端 `astraquant` 指向 `https://github.com/FrankeyQu/astraquant.git`。

### 2. 后端安全控制平面与 API contract

主线已有能力：

- Trader 查询与状态 API。
- Trader lifecycle 控制平面 API：start、stop、pause、resume。
- 控制平面只记录状态，不启动真实 manager trading loop，不绕过 `manager.ApproveDecision`，不提交订单。
- live trader start/resume 默认受环境变量硬门禁保护。
- Decision approve/reject、Order approve/reject 仍是安全占位：`accepted=false`、`status=not_implemented`、`queued=false`。
- Order preview 是 preview-only，只做形状标准化和检查，不提交、不排队。

参考文档：

- `go/docs/API_CONTRACT.md`

### 3. 审计日志与订单只读投影

已完成分支：

- `codex/orders-audit-readmodel`
- commit：`7ca2767 feat: expose audit-backed orders read model`
- 已推送：`astraquant/codex/orders-audit-readmodel`

能力：

- `GET /api/orders` 从 `order_submitted` / `order_failed` audit events 生成只读订单流。
- 支持过滤：`trader_id`、`symbol`、`status`、`limit`、`offset`。
- 返回 `types.Order`，包含 trader、symbol、side、type、quantity、limit_price、correlation_id、detail 等。
- 当 audit repository 未接入时安全降级为空数据，`status=not_available`。
- 保持安全边界：没有打开 submit、queue、approve 的真实执行路径。

已验证：

- `CGO_ENABLED=0 go test ./internal/logic`
- `CGO_ENABLED=0 go test ./internal/logic ./internal/svc ./pkg/manager`
- `CGO_ENABLED=0 go test ./internal/handler/... ./internal/logic/...`

### 4. Web Console 订单审计面板

已完成分支：

- `codex/web-orders-panel`
- commit：`fbd893a feat: add orders audit panel`
- 已推送：`astraquant/codex/web-orders-panel`

能力：

- 右侧控制台新增“订单”标签。
- 接入 `GET /api/orders`。
- 新增 `useOrders` SWR hook。
- 支持订单状态筛选：全部、已提交、失败。
- 展示订单时间、模型/交易员、symbol、side、type、quantity、price、status、correlation id。
- `/api/nof1/[...path]` 代理层对 `orders` 设置 10 秒缓存，与现有 live-ish endpoints 对齐。
- 保持纯只读 UI，不增加任何下单按钮或执行入口。

已验证：

- 触碰文件范围 ESLint 通过。
- `npm run build` 通过。
- 全量 `npm run lint` 仍被项目既有 lint 问题阻塞，非本次新增代码引入。

PR 地址：

- `https://github.com/FrankeyQu/astraquant/pull/new/codex/web-orders-panel`

### 5. Web Console 审计事件面板

已完成分支：

- `codex/web-audit-events-panel`
- commit：`57c6e69 feat: add audit events panel`
- 已推送：`astraquant/codex/web-audit-events-panel`

能力：

- 右侧控制台新增“审计”标签。
- 接入 `GET /api/audit-events`。
- 新增 `useAuditEvents` SWR hook。
- 支持事件类型筛选：
  - `decision_generated`
  - `decision_validation_failed`
  - `policy_rejected`
  - `approved`
  - `order_submitted`
  - `order_failed`
- 展示事件时间、模型/交易员、事件类型、symbol、reason/error/action、cycle id、correlation id。
- `/api/nof1/[...path]` 代理层对 `audit-events` 设置 10 秒缓存。
- 页面仍为只读观测面，不引入任何交易动作。

已验证：

- 触碰文件范围 ESLint 通过。
- `npm run build` 通过。

PR 地址：

- `https://github.com/FrankeyQu/astraquant/pull/new/codex/web-audit-events-panel`

### 6. Web Console 交易员面板（已完成）

当前状态：

- 已完成 `codex/web-traders-panel`。
- 接入 `GET /api/traders`、`GET /api/traders/:traderId/status`，并用只读详情补齐提示词摘要与账户快照字段。
- UI 维持密集、中文标签、低风险只读风格，支持状态筛选和 execution_mode 筛选。
- 已完成本地 `eslint` 与 `npm run build` 验证，准备 commit/push。

## 当前分支堆叠关系

当前工作分支：

- `codex/web-traders-panel`

当前堆叠链路：

```text
main
  -> codex/orders-audit-readmodel
  -> codex/web-orders-panel
  -> codex/web-audit-events-panel
  -> codex/web-traders-panel
```

对应能力链路：

```text
Audit events persistence/query
  -> audit-backed readonly orders API
  -> Web orders panel
  -> Web audit events panel
  -> Web trader panel
```

## 当前系统能力快照

### 后端

已具备：

- 基础 REST API contract。
- Trader list/detail/status。
- Trader lifecycle control-plane state recording。
- Audit events query。
- Audit-backed readonly orders query。
- Trades/positions existing feed with optional filters。
- Safe placeholders for decision/order approve/reject。
- Preview-only order normalization。

待加强：

- Secrets isolation 的完整生产化注入与审计。
- Paper exchange 的保证金、手续费、滑点、reduce-only/close 行为测试继续补齐。
- 审计事件的更多 manager 执行路径覆盖。
- 真实 guarded queue 尚未实现，approve/reject 不应接入执行。
- Alembic/迁移体系不是当前 Go 后端主线，Go 侧 schema/model/repo 仍需统一治理。

### 前端

已具备：

- 控制台主页：价格条、账户曲线、持仓、模型对话、成交、订单、审计、README。
- 排行榜/模型相关页面基础能力。
- SWR 数据层。
- 本地 `/api/nof1/[...path]` 代理，避免 CORS 并做短 TTL 缓存。
- 订单与审计两个只读安全面板。

待加强：

- 全量 ESLint 仍有较多历史问题，需要单独 cleanup 分支。
- 控制台可继续补 Playwright 冒烟测试、筛选记忆与更细的只读状态展示。
- 审计/订单面板可以继续增加 trader/symbol/correlation 搜索，但仍应保持只读。
- 需要 Playwright 冒烟测试覆盖关键页面和 tabs。

## 已知验证状态

Go 后端最近关键验证：

```powershell
cd go
$env:CGO_ENABLED='0'
go test ./internal/logic
go test ./internal/logic ./internal/svc ./pkg/manager
go test ./internal/handler/... ./internal/logic/...
```

Web 最近关键验证：

```powershell
cd web
npx eslint <touched files>
npm run build
```

注意：

- `web/node_modules` 是本地安装产物，不应提交。
- `npm install` 后 npm audit 显示 7 个依赖审计项，尚未自动修复，避免引入无关依赖升级。
- 全量 `npm run lint` 当前仍因既有文件中的 `any`、React hooks/ref 规则等问题失败。

## 建议下一步

优先级从高到低：

1. 合并当前三段堆叠分支，或创建一个集成 PR，把 orders API、订单面板、审计面板放到同一条 review 线中。
2. 新开 `codex/web-lint-cleanup`：只清理 Web 全量 lint 的既有问题，不混入功能。
3. 新开 `codex/control-plane-smoke-tests`：为 safe control-plane endpoints 增加更完整的 handler/API smoke tests。
4. 继续补 Paper Exchange 行为测试，尤其是手续费、滑点、保证金和 close/reduce-only 语义。

## 安全边界提醒

当前阶段必须继续遵守：

- Web Console 不展示 secret。
- 不在 URL、query、log、localStorage 中保存 API key 或 private key。
- 任何 live trading 路径都必须显式门禁，默认拒绝。
- 审计、订单、trader runtime 页面默认只读。
- approve/reject 在真实 guarded queue 完成前继续保持安全占位。
