# API Monitor Toolbox Refactor Handoff

Generated: 2026-06-08

## Suggested Skills

- `handoff`: refresh this file when the next session ends.
- `diagnose`: use for module regressions, broken backend routes, startup errors, or performance issues.
- `browser:control-in-app-browser` or `agent-browser-cli`: use after frontend changes for local visual and interaction verification.
- `to-issues`: use if the PRDs need to be split into implementation tickets.

## Primary Entry Points

- `E:\Code\API-Monitor\docs\refactor-next.md`
- `E:\Code\API-Monitor\docs\refactor-progress.md`
- `E:\Code\API-Monitor\docs\refactor-verification.md`
- `E:\Code\API-Monitor\docs\toolbox-modules-refactor-prd.md`
- `E:\Code\API-Monitor\docs\uptime-kuma-aligned-prd.md`

## Current State

The React + Kumo migration is the current frontend baseline, but the toolbox modules are not product-complete. The next implementation phase should treat 双因子认证, 音乐, 可用性监测, 文件柜, 通知, and 系统 as a coordinated frontend/backend refactor, not as isolated style fixes.

The newly added PRDs are the source of truth for the next session:

- `toolbox-modules-refactor-prd.md` covers the six toolbox modules, shared architecture, API/data model direction, migration phases, and acceptance criteria.
- `uptime-kuma-aligned-prd.md` covers the Uptime Kuma-aligned monitoring module in more detail.

## Highest Priority

Start with the shared foundation before feature work:

1. `SecureSecretStore`
2. `AuditService`
3. `ToolboxEventBus`
4. `JobScheduler`
5. unified API response and error handling
6. settings registry and migration self-checks

Then continue with high-risk persistence/security migrations: encrypted TOTP secrets, Filebox metadata JSON to SQLite, and system settings schema consistency.

## Important Constraints

- Use Kumo components only for frontend controls unless there is a documented exception.
- Default buttons to `size="sm"`.
- Align Button/Input/Select heights when they sit in one row.
- Internal tabs should use compact Kumo Tabs, usually `size="sm"`.
- Tables should avoid wrapping; prefer horizontal scroll, truncation, and tooltips.
- Use Kumo `DeleteResource` for destructive confirmation.
- Use Kumo `ClipboardText` for copyable commands, tokens, URLs, and IDs.
- Do not revert unrelated dirty worktree changes.
- The user normally restarts services manually.

## Verification Status

This handoff update is docs-only. No lint/build/browser smoke was run for this specific update. The next code-changing session should run targeted tests plus `npm run lint` and `npm run build` before handoff.

## Redactions

No secrets, install tokens, cookies, or credential values are included here. If an agent needs to inspect install commands or auth flows, read local source and logs without copying sensitive values into documentation.
