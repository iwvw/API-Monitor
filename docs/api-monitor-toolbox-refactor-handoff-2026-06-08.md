# API Monitor Toolbox Refactor Handoff

Generated: 2026-06-09

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

React + Kumo is the current frontend baseline. The toolbox PRDs have been implemented to the current product baseline, and the latest work focused on dashboard/server UX polish, cloud-persisted shell preferences, runtime fixes, and restoring collapsible animations.

The PRDs remain useful as product scope references:

- `toolbox-modules-refactor-prd.md` covers the six toolbox modules, shared architecture, API/data model direction, migration phases, and acceptance criteria.
- `uptime-kuma-aligned-prd.md` covers the Uptime Kuma-aligned monitoring module in more detail.

## Highest Priority

Continue with real-environment validation and visual regression:

1. Host Agent install/upgrade, CPU/GPU temperature and power telemetry, and 1.5s chart update cadence.
2. SSH terminal, SFTP follow-active-session behavior, multi-tab and split-pane terminal workflows.
3. Docker installed-state detection, update checks, one-click updates, and table horizontal scrolling.
4. Music real NCM login/playback/unblock proxy.
5. Notification real Email/Telegram delivery and rule suppression.
6. Uptime real probes, public status pages, push tokens, and badge routes.
7. Plugin ZIP download in Linux containers.

## Important Constraints

- Use Kumo components only for frontend controls unless there is a documented exception.
- Default buttons to `size="sm"`.
- Align Button/Input/Select heights when they sit in one row.
- Internal tabs should use compact Kumo Tabs, usually `size="sm"`.
- Tables should avoid wrapping; prefer horizontal scroll, truncation, and tooltips.
- Use Kumo `DeleteResource` for destructive confirmation.
- Use Kumo `ClipboardText` for copyable commands, tokens, URLs, and IDs.
- Use `AnimatedCollapse` for expand/collapse; it must stay backed by Kumo `Collapsible` and Base UI height variables.
- Do not revert unrelated dirty worktree changes.
- The user normally restarts services manually.

## Verification Status

Latest code validation before this docs refresh:

- `npm run lint` passed.
- `npm run build` passed with only the existing Vite chunk size warning.
- Collapsible CSS output includes `--collapsible-panel-height`, `[data-open]`, `[data-closed]`, `[data-ending-style]`, and height/opacity transition rules.

Full authenticated browser smoke is still pending.

## Redactions

No secrets, install tokens, cookies, or credential values are included here. If an agent needs to inspect install commands or auth flows, read local source and logs without copying sensitive values into documentation.
