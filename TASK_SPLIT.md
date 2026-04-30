# AstraQuant Task Split

本文档定义后续多线程并行开发的任务拆分、启动顺序和依赖关系。

## 1. 第一波线程建议

第一波目标是补齐平台安全与可运行闭环，不先做复杂 AI 增强。

| 线程名称 | 模块 | 独立 worktree | 是否需要 subagents | 推荐启动顺序 | 前置依赖 |
| --- | --- | --- | --- | --- | --- |
| T0-Coordinator | 总设计/集成 | 是 | 否 | 0 | 无 |
| T1-Secrets | 密钥隔离 | 是 | 可选 | 1 | SecretStore contract |
| T2-Audit | 审计日志 | 是 | 可选 | 1 | Audit event contract |
| T3-PaperExchange | 模拟盘完善 | 是 | 可选 | 1 | Exchange contract |
| T4-APIContract | Trader/API contract | 是 | 否 | 2 | T1/T2 基础 contract |
| T5-WebConsole | Web 控制台 | 是 | 可选 | 3 | T4 API contract 稳定 |
| T6-DevEx | CI/Docker/文档 | 是 | 否 | 2 | 无 |

## 2. 第二波线程建议

| 线程名称 | 模块 | 独立 worktree | 是否需要 subagents | 推荐启动顺序 | 前置依赖 |
| --- | --- | --- | --- | --- | --- |
| T7-LLMDecision | 多模型决策 | 是 | 可选 | 4 | Audit + Prompt contract |
| T8-IntelIngestion | 市场情报接入 | 是 | 可选 | 4 | Executor context contract |
| T9-BacktestReplay | 回放/评估 | 是 | 可选 | 5 | Audit event schema |
| T10-OpenSourceOps | License/贡献治理 | 是 | 否 | 2 | 上游 license 确认 |

## 3. 模块任务详情

### T1-Secrets

负责：

- 定义 `SecretStore` interface。
- 实现 local env/file provider。
- 禁止 secret 进入全局状态或日志。
- 为 exchange provider 提供按 trader/session 注入的 credential context。

不负责：

- Web UI。
- 交易策略。
- 下单风控。

允许修改：

- `go/internal/secrets/**`
- `go/pkg/exchange/**` 中 credential 注入相关文件。
- `go/internal/svc/servicecontext.go`，需协调。
- `go/.env.example`
- `go/etc/exchange.yaml`

禁止修改：

- `web/**`
- `go/pkg/executor/**`
- `go/pkg/manager/policy.go`，除非协调线程批准。

共享文件需协调：

- `go/pkg/exchange/config.go`
- `go/internal/svc/servicecontext.go`
- `go/etc/exchange.yaml`

输出：

- SecretStore contract。
- 测试。
- 中文交接说明。

### T2-Audit

负责：

- 定义 audit event schema。
- 记录 decision generated/validated/rejected/approved/submitted/filled/failed。
- 提供 repo/service 查询接口。

不负责：

- 风控判断。
- UI 图表。
- exchange 细节。

允许修改：

- `go/internal/persistence/**`
- `go/pkg/repo/**`
- `go/internal/model/**`
- `go/migrations/**`
- `go/pkg/manager/persistence.go`

禁止修改：

- `go/pkg/exchange/**`
- `web/**`

共享文件需协调：

- `go/internal/model/generated_compat.go`
- `go/internal/svc/servicecontext.go`
- `go/pkg/manager/manager.go`

输出：

- Audit event contract。
- migration/model/repo。
- manager 挂钩方案。

### T3-PaperExchange

负责：

- 完善 sim exchange。
- 手续费、滑点、保证金、PnL。
- 对 reduce-only 和 close 语义补测试。

不负责：

- manager 策略。
- UI。
- secret store。

允许修改：

- `go/pkg/exchange/sim/**`
- `go/pkg/exchange/*sim*_test.go`

共享文件需协调：

- `go/pkg/exchange/interface.go`
- `go/pkg/exchange/types.go`

输出：

- 更稳定的 paper provider。
- 行为测试。

### T4-APIContract

负责：

- Trader 管理 API。
- Decision/Audit/Order/Position 查询 API。
- API contract 文档。

不负责：

- Web 实现。
- 底层交易逻辑。

允许修改：

- `go/nof0.api`
- `go/internal/handler/**`
- `go/internal/logic/**`
- `go/internal/svc/**`

共享文件需协调：

- `go/pkg/manager/**` public methods。
- `go/pkg/repo/**` interfaces。

前置等待：

- T1 SecretStore contract。
- T2 Audit event contract。

### T5-WebConsole

负责：

- Web 控制台。
- Trader 列表/详情。
- 决策和审计记录展示。
- paper/testnet/live 模式提示。

不负责：

- 后端业务规则。
- secret 明文持久化。

允许修改：

- `web/**`

禁止修改：

- `go/**`，除非协调线程明确批准。

前置等待：

- T4 API contract 稳定。

### T6-DevEx

负责：

- CI 扩展。
- Docker Compose。
- secret scan。
- README/启动文档。

不负责：

- 业务功能。

允许修改：

- `.github/**`
- `README.md`
- `NOTICE.md`
- `go/Makefile`
- `go/docker-compose.yml`
- `go/.env.example`
- 文档文件。

共享文件需协调：

- `.gitignore`
- license/notice。

### T7-LLMDecision

负责：

- 多模型投票。
- 反方审查。
- Prompt 版本管理。
- 决策 schema 演进。

允许修改：

- `go/pkg/executor/**`
- `go/etc/prompts/**`
- `go/schemas/**`

前置等待：

- Audit event schema。
- Executor context contract。

### T8-IntelIngestion

负责：

- 接入情报数据。
- 与现有 `alpha-trading-bot` 做 adapter 设计。
- 将情报提供给 executor context。

允许修改：

- 新目录 `go/internal/intel/**`
- 新目录 `go/pkg/intel/**`
- 相关 repo/model/migration，需协调。

前置等待：

- Executor context contract。
- DB schema 协调。

## 4. 推荐启动顺序

第一批：

1. T0-Coordinator：创建 worktree/branch，冻结共享 contract 变更窗口。
2. T1-Secrets、T2-Audit、T3-PaperExchange 并行启动。
3. T6-DevEx 可同时启动，但不能改业务 contract。

第二批：

4. T4-APIContract 等 T1/T2 contract 草案稳定后启动。
5. T5-WebConsole 等 API contract 稳定后启动。

第三批：

6. T7-LLMDecision 等 Audit + Prompt contract 稳定后启动。
7. T8-IntelIngestion 等 Executor context contract 稳定后启动。

## 5. 不建议第一波启动的任务

暂缓：

- 实盘 provider 深度改造。
- 多交易所适配。
- 复杂前端动效。
- 全量回测系统。
- 自动策略优化。

原因：

- Secret、Audit、Paper、API contract 未稳定前，这些任务容易产生大规模冲突。

## 6. 每个线程交付标准

必须完成：

- 独立 worktree。
- 独立 feature branch。
- commit。
- push 到远端功能分支。
- 中文交接说明。
- 必要测试通过。

禁止：

- 直接 merge 到当前主工作区。
- 修改未授权目录。
- 混入无关格式化。

