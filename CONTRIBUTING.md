# Contributing to AstraQuant

This project uses parallel worktrees for larger changes. Please keep changes
scoped to the thread or module you are working on, and avoid mixing unrelated
formatting or cleanup into feature branches.

## Parallel Development Rules

- Read `ARCHITECTURE.md`, `CONVENTIONS.md`, `TASK_SPLIT.md`,
  `THREAD_STARTUP.md`, `THREAD_BRIEFS.md`, and `WORKTREE_PLAN.md` before
  starting a thread.
- Work on a dedicated branch named `codex/<module>-<short-task>`.
- Modify only the files allowed by the thread brief.
- Do not merge `main` or another thread branch into a thread worktree unless the
  coordinator asks for it.
- Do not revert or overwrite another thread's work.

## Safety and Secrets

- Never commit `.env`, private keys, seed phrases, API keys, OAuth tokens,
  database dumps, or local dependency directories.
- Keep live trading disabled unless a reviewed, explicit live path is being
  tested.
- Use `go/.env.example` for placeholders only. Real values belong in local
  `.env` files or a secret manager.
- CI includes a lightweight secret scan. Treat any finding as a release blocker
  until it is either removed or documented as a false positive with coordinator
  approval.

## Local Checks

For backend-adjacent changes:

```bash
cd go
make test-ci
```

For DevEx changes:

```bash
cd go
make -n build test test-ci docker-up docker-down docker-ps
docker compose -f docker-compose.yml config
```

## Handoff Requirements

Thread handoffs and PR descriptions must be written in Chinese and include:

- Thread name, branch, commit hash, and push status.
- Completed work.
- Main files changed.
- API or contract changes, or an explicit statement that there are none.
- Test commands and results.
- Remaining risks and suggested next steps.
