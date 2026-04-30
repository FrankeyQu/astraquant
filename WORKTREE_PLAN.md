# Worktree Plan

本文档定义 AstraQuant 多线程开发的 worktree 目录、分支和清理规则。

## 1. 基本原则

- 每个开发线程必须使用独立 worktree。
- 每个 worktree 对应一个功能分支。
- 线程不得直接在主工作区开发。
- 线程完成后只 push 功能分支，不直接合并。
- 协调线程在干净集成 worktree 或 PR 中统一合并。

## 2. 推荐目录

主仓库：

```text
D:\PRJ\lianghua\nof0
```

线程 worktree：

```text
D:\PRJ\lianghua\astraquant-t1-secrets
D:\PRJ\lianghua\astraquant-t2-audit
D:\PRJ\lianghua\astraquant-t3-paper-exchange
D:\PRJ\lianghua\astraquant-t4-api-contract
D:\PRJ\lianghua\astraquant-t5-web-console
D:\PRJ\lianghua\astraquant-t6-devex
D:\PRJ\lianghua\astraquant-integration
```

## 3. 分支规划

| 线程 | Worktree | Branch |
| --- | --- | --- |
| T1-Secrets | `astraquant-t1-secrets` | `codex/t1-secrets` |
| T2-Audit | `astraquant-t2-audit` | `codex/t2-audit` |
| T3-PaperExchange | `astraquant-t3-paper-exchange` | `codex/t3-paper-exchange` |
| T4-APIContract | `astraquant-t4-api-contract` | `codex/t4-api-contract` |
| T5-WebConsole | `astraquant-t5-web-console` | `codex/t5-web-console` |
| T6-DevEx | `astraquant-t6-devex` | `codex/t6-devex` |
| Integration | `astraquant-integration` | `codex/integration-wave-1` |

## 4. 创建命令

示例：

```powershell
cd D:\PRJ\lianghua\nof0
git fetch astraquant
git worktree add ..\astraquant-t1-secrets -b codex/t1-secrets astraquant/main
git worktree add ..\astraquant-t2-audit -b codex/t2-audit astraquant/main
git worktree add ..\astraquant-t3-paper-exchange -b codex/t3-paper-exchange astraquant/main
```

如果分支已存在：

```powershell
git worktree add ..\astraquant-t1-secrets codex/t1-secrets
```

## 5. 集成策略

推荐：

1. 每个线程 push 自己的 `codex/...` 分支。
2. 协调线程创建 PR 或在 integration worktree 中逐个 merge。
3. 每合并一个线程分支，立即运行相关测试。
4. 所有第一波线程合并后，跑完整 CI。
5. CI 通过后再合并到 `main`。

禁止：

- 在线程 worktree 中互相 merge。
- 在线程分支中解决其他线程的大量冲突。
- 未经协调修改共享 contract。

## 6. 清理规则

远端 merge 完成后才能清理对应 worktree。

清理前确认：

```powershell
git status -sb
git branch --contains <commit>
git ls-remote astraquant main
```

确认条件：

- 线程分支已 push。
- 远端目标分支已包含该提交。
- 本地 worktree 没有未提交改动。

清理命令：

```powershell
git worktree remove D:\PRJ\lianghua\astraquant-t1-secrets
```

如需删除本地分支：

```powershell
git branch -d codex/t1-secrets
```

必须确认远端已包含后再删除。

## 7. 禁止事项

- 禁止在未确认 merge 成功前删除 worktree。
- 禁止用 `git reset --hard` 清理工作区。
- 禁止粗暴删除 worktree 目录。
- 禁止在线程中修改未授权目录。
- 禁止多个线程同时修改共享文件，除非协调线程明确允许。
- 禁止把无关改动混入功能分支。

