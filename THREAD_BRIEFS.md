# Thread Briefs

本文档提供每个线程的启动简报。协调线程启动某个线程时，应复制对应 brief 给该线程。

## T0-Coordinator

角色：

- 总设计/协调/集成线程。

目标：

- 维护架构边界。
- 分配 worktree。
- 审核 contract 变更。
- 在干净集成 worktree 或 PR 中统一合并。

允许修改：

- 规划文档。
- PR/CI/集成相关文件。
- 经确认后的共享 contract。

禁止：

- 在未授权情况下写具体业务功能。
- 直接合并未通过测试的线程分支。

## T1-Secrets

目标：

- 实现密钥隔离基础，不让交易所 key 进入前端持久化或全局环境变量。

允许修改：

- `go/internal/secrets/**`
- `go/pkg/exchange/config.go`
- `go/etc/exchange.yaml`
- `go/.env.example`
- 必要测试。

禁止修改：

- `web/**`
- `go/pkg/executor/**`
- `go/pkg/manager/policy.go`，除非协调线程批准。

需先输出 contract：

- `SecretStore` interface。
- secret lookup key。
- secret redaction 规则。

## T2-Audit

目标：

- 建立可追溯审计日志。

允许修改：

- `go/internal/persistence/**`
- `go/pkg/repo/**`
- `go/internal/model/**`
- `go/migrations/**`
- `go/pkg/manager/persistence.go`

禁止修改：

- `web/**`
- `go/pkg/exchange/**`

需先输出 contract：

- Audit event schema。
- decision/order 状态枚举。

## T3-PaperExchange

目标：

- 完善模拟盘交易行为。

允许修改：

- `go/pkg/exchange/sim/**`
- `go/pkg/exchange/*_test.go`

禁止修改：

- `go/pkg/manager/**`
- `web/**`

需协调：

- 如果必须修改 `go/pkg/exchange/interface.go` 或 `types.go`，先停下来请求协调线程确认。

## T4-APIContract

目标：

- 提供 Trader、Decision、Audit、Order、Position API contract。

允许修改：

- `go/nof0.api`
- `go/internal/handler/**`
- `go/internal/logic/**`
- `go/internal/svc/**`

禁止修改：

- `web/**`
- `go/pkg/exchange/**`

前置等待：

- T1 Secret contract。
- T2 Audit contract。

## T5-WebConsole

目标：

- 构建 AstraQuant 控制台。

允许修改：

- `web/**`

禁止修改：

- `go/**`

前置等待：

- T4 API contract 稳定。

安全要求：

- 不得将 API key/secret 保存到 localStorage。
- 不得在 URL/query/log 中展示 secret。

## T6-DevEx

目标：

- 强化 CI、Docker、文档、secret scan。

允许修改：

- `.github/**`
- `README.md`
- `NOTICE.md`
- `go/Makefile`
- `go/docker-compose.yml`
- `go/.env.example`
- 文档。

禁止修改：

- 业务逻辑文件，除非协调线程批准。

## T7-LLMDecision

目标：

- 多模型决策、反方审查、prompt 版本管理。

允许修改：

- `go/pkg/executor/**`
- `go/etc/prompts/**`
- `go/schemas/**`

禁止修改：

- `go/pkg/exchange/**`
- `web/**`

前置等待：

- Audit event contract。

## T8-IntelIngestion

目标：

- 将情报系统接入 AI 决策上下文。

允许修改：

- 新目录 `go/internal/intel/**`
- 新目录 `go/pkg/intel/**`
- 相关 repo/model/migration，需协调。

禁止修改：

- `go/pkg/exchange/**`
- `web/**`

前置等待：

- Executor context contract。

