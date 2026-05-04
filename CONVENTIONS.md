# AstraQuant Development Conventions

本文档是所有并行开发线程必须遵守的开发共识。

## 1. 命名规范

Go：

- package 使用小写短名，例如 `manager`, `exchange`, `secrets`。
- public type 使用清晰业务名，例如 `ApprovalToken`, `ExecutionMode`, `TraderConfig`。
- interface 名称优先表达能力，例如 `SecretStore`, `AuditRecorder`。
- error message 使用小写开头，带模块前缀，例如 `manager policy: live trading is disabled in CI`。
- 环境变量使用 `ASTRAQUANT_` 前缀。

API：

- 路径使用复数资源名，例如 `/traders`, `/orders`, `/audit-events`。
- JSON 字段使用 snake_case。
- 状态枚举使用小写 snake_case。

分支：

- 功能分支：`codex/<module>-<short-task>`。
- 示例：`codex/secrets-store`, `codex/audit-log`, `codex/paper-exchange-pnl`。

Commit：

- 使用英文 conventional commit。
- 示例：
  - `feat: add secret store interface`
  - `fix: block live execution in CI`
  - `test: add audit event persistence tests`

交接：

- 线程交接、PR 描述、最终汇总必须使用中文。

## 2. 分层规则

后端分层：

- controller/handler：只负责 HTTP 参数解析、响应映射、调用 logic。
- logic/application service：负责用例编排、权限/输入校验、调用 domain service。
- domain service：负责业务规则，例如 manager、policy、executor。
- repo：负责数据访问抽象。
- model：负责 DB row 映射。
- provider：负责外部系统适配，例如 exchange、market、LLM、secret backend。

禁止：

- handler 直接调用 DB。
- handler 直接下单。
- repo 内写业务风控。
- provider 内读取前端 session。
- manager 直接读取前端 localStorage 或全局用户态。

## 3. Controller / Service / Model 规则

Controller/handler：

- 输入校验只做轻量格式校验。
- 不写交易策略。
- 不拼 SQL。
- 不处理 secret 明文。

Service/logic：

- 编排 use case。
- 对 contract 做兼容转换。
- 将错误映射为稳定业务错误。

Model/repo：

- model 只表示数据结构。
- repo 只封装查询和事务。
- migration 必须与 model/repo 同步。

Domain：

- PolicyGateway、ExecutionMode、ApprovalToken 属于 domain。
- 所有下单路径必须经过 domain 审批。

## 4. 公共层修改规则

公共层包括：

- `go/pkg/exchange/interface.go`
- `go/pkg/exchange/types.go`
- `go/pkg/executor/types.go`
- `go/pkg/manager/config.go`
- `go/pkg/manager/persistence.go`
- `go/internal/svc/servicecontext.go`
- `go/nof0.api`
- `go/internal/model/**`
- `go/migrations/**`

规则：

- 修改公共层前必须在协调线程确认。
- 修改公共层必须说明向后兼容影响。
- 修改公共层必须补测试。
- 多线程不得同时修改同一公共文件，除非协调线程明确允许。

## 5. 状态流转规则

新增或修改状态必须同时更新：

- `ARCHITECTURE.md`
- 对应 Go enum/const。
- API schema。
- 测试。

状态不可随意跳转。涉及资金、订单、审批的状态必须可审计。

## 6. API / Contract 变更规则

任何 API 变更必须包含：

- request/response 示例。
- 错误码或错误消息规则。
- 是否兼容旧客户端。
- 对 web 线程的影响。
- 至少一个测试或 contract check。

禁止：

- 未协调直接改 `go/nof0.api`。
- 前端线程自造后端字段。
- 后端线程删除前端正在使用的字段。

## 7. 测试要求

最低要求：

- Go 改动必须跑相关包测试。
- 后端或 CI 覆盖面改动建议跑 `make test-ci`，该 target 使用 `CGO_ENABLED=0 go test ./...` 覆盖全部 Go 包。
- manager/policy 改动必须跑：
  - `go test ./pkg/manager`
  - `go test ./pkg/executor ./pkg/manager ./pkg/exchange/sim`
- persistence/repo 改动必须跑：
  - `go test ./internal/model ./internal/persistence/engine ./internal/persistence/market ./pkg/repo`
- CI 改动必须在 GitHub Actions 通过。

新增测试建议：

- 风控拒绝路径必须有测试。
- live/testnet/paper 边界必须有测试。
- secret 不落盘、不进日志必须有测试。
- API contract 变更必须有 handler/logic 测试或 smoke test。

## 8. 线程交接要求

每个线程完成后必须提供中文说明：

- 本线程完成了什么。
- 修改了哪些主要文件。
- 新增或变更了哪些 API / contract。
- 跑了哪些测试，结果如何。
- 遗留风险或下一步建议。

交接中必须明确：

- 分支名。
- commit hash。
- 是否已 push。
- 是否有未提交文件。

## 9. 中文说明要求

以下内容必须中文：

- 线程交接。
- PR 描述。
- 最终汇总。
- 风险说明。
- 架构决策说明。

Commit message 可以英文。

## 10. Merge 后 Worktree 清理规则

远端 merge 完成后才能清理对应 worktree。

清理前必须确认：

- 线程分支已 push。
- 远端目标分支已包含该提交。
- 本地 worktree 没有未提交改动。

推荐命令：

```powershell
git worktree remove <path>
```

如需删除本地分支，必须确认远端已包含后再删除。

禁止：

- 未确认 merge 成功前删除 worktree。
- 使用 `git reset --hard` 清理工作区。
- 粗暴删除 worktree 目录。
- 在线程中修改未授权目录。
- 多线程同时修改共享文件。
- 把无关改动混入功能分支。

## 11. Secret 与安全规则

禁止提交：

- `.env`
- `VPS.env`
- API key
- private key
- seed phrase
- exchange secret
- 本地数据库
- virtualenv/node_modules

禁止日志输出：

- 交易所私钥。
- API secret。
- OAuth token。
- 完整 Authorization header。

所有 live trading 相关能力必须默认关闭。

