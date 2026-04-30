# Thread Startup Guide

本文档用于后续每个开发线程启动时读取。线程开始前必须确认本文件和对应 `THREAD_BRIEFS.md` 条目。

## 1. 启动前检查

每个线程开始前必须确认：

- 当前任务属于哪个线程。
- 当前线程允许修改哪些目录。
- 当前线程禁止修改哪些目录。
- 是否依赖其他线程 contract。
- 是否需要等待协调线程确认。
- 当前 worktree 是否干净。
- 当前分支是否为本线程专用分支。

## 2. 标准启动步骤

由协调线程执行：

```powershell
git fetch astraquant
git worktree add ..\astraquant-<thread-name> -b codex/<thread-name> astraquant/main
```

线程进入自己的 worktree：

```powershell
cd ..\astraquant-<thread-name>
git status -sb
```

如果不是干净状态，必须停止并反馈协调线程。

## 3. 线程工作规则

线程必须：

- 只修改授权目录。
- 先读相关代码，再动手。
- 变更公共 contract 前先写出变更说明，等待协调线程确认。
- 对风险路径补测试。
- 保持 commit 小而清晰。

线程不得：

- 使用 `git reset --hard`。
- 删除其他线程 worktree。
- 修改其他线程正在负责的模块。
- 把临时文件、密钥、依赖目录提交。
- 直接合并 main。

## 4. 测试规则

按模块运行测试：

Manager/Policy：

```powershell
cd go
go test ./pkg/manager
go test ./pkg/executor ./pkg/manager ./pkg/exchange/sim
```

Persistence/Repo：

```powershell
cd go
go test ./internal/model ./internal/persistence/engine ./internal/persistence/market ./pkg/repo
```

Exchange sim：

```powershell
cd go
go test ./pkg/exchange/sim ./pkg/exchange
```

API：

```powershell
cd go
go test ./internal/handler ./internal/logic ./internal/svc
```

如果包不存在或没有测试，线程需在交接说明中说明。

## 5. 提交与推送

线程完成后：

```powershell
git status -sb
git add <authorized-files>
git commit -m "feat: short English summary"
git push -u astraquant codex/<thread-name>
```

必须只 stage 授权文件。

## 6. 中文交接模板

线程完成后必须提供：

```text
线程：T?-Name
分支：codex/...
Commit：...
Push：已推送 / 未推送

完成内容：
- ...

主要修改文件：
- ...

API / Contract 变化：
- ...

测试：
- 命令：...
- 结果：...

遗留风险：
- ...

下一步建议：
- ...
```

## 7. 交接后禁止事项

线程交接后不得继续往同一分支追加改动，除非协调线程要求返工。

工作区保留，等待：

- PR 创建。
- CI。
- 集成验证。
- merge。
- 远端目标分支确认包含提交。

之后才能清理 worktree。

