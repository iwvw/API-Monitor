# Security Baseline

The `Security Baseline` workflow ([`.github/workflows/security.yml`](../../.github/workflows/security.yml))
runs four independent scanners on every push and pull request to `main`/`dev`,
plus a full-history scan weekly.

| Job | What it catches |
| --- | --- |
| `npm audit` | Known advisories in JavaScript dependencies |
| `govulncheck` | Advisories in Go dependencies **and** whether the vulnerable code is actually reachable |
| `cargo audit` | Advisories in Rust dependencies |
| `gitleaks` | Committed secrets, in the pushed range and (weekly) the whole history |

> **The jobs are deliberately separate.** They previously ran as steps in a
> single job, so the first failure aborted the rest and the run told you nothing
> about the other three scanners. Do not merge them back together.

## When a run goes red

### A dependency advisory

The gate fails on **every** advisory. There are exactly two legitimate
responses:

1. **Upgrade the dependency.** Always prefer this. The gate refuses to let you
   allowlist an advisory that has a released fix.
2. **If no fix exists yet**, record a reviewed, expiring exception (below).

Transient registry/network failures are reported as *infrastructure* errors
("could not reach the advisory database"), not as security findings. If you see
one of those, re-run the job.

### A gitleaks finding

Take it seriously first; assume it is real. A finding means either:

- **It is a real credential.** Rotate it immediately. Removing the file is *not*
  enough — it stays in git history and must be treated as compromised. Then
  remove it from the code and, if the repository is shared, purge history
  (`git filter-repo`) and force-push with the team's knowledge.
- **It is genuinely not a credential.** Add a narrow exemption to
  [`.gitleaks.toml`](../../.gitleaks.toml), with a comment explaining why.

## Recording a dependency exception

Edit [`.github/security/dependency-allowlist.json`](./dependency-allowlist.json):

```json
{
  "entries": [
    {
      "id": "GHSA-xxxx-xxxx-xxxx",
      "ecosystem": "npm",
      "reason": "Only reachable via the unused admin CLI; tracked in #123.",
      "owner": "@your-handle",
      "expires": "2026-01-31"
    }
  ]
}
```

Enforced by [`tools/dependency-audit.mjs`](../../tools/dependency-audit.mjs):

- `id`, `ecosystem`, `reason`, `owner` and `expires` are **all required**.
- An advisory **with an available fix cannot be allowlisted** — upgrade instead.
- Entries **expire**. After `expires`, the entry stops suppressing and the build
  fails on that advisory again. Expiry is deliberate friction: it forces a
  re-assessment rather than silent, permanent debt.
- The entry's `ecosystem` must match, so an npm entry can never silence a Rust
  advisory.
- Malformed or expired entries are printed as warnings on every run.

Keep this file short. A growing allowlist means the real fix — upgrading or
dropping a dependency — is being avoided.

## Editing `.gitleaks.toml`

**Read the header of that file before editing it.** In a gitleaks
`[[allowlists]]` block, `paths` and `regexes` combine as a **union**, not an
intersection. Adding a `regexes` entry to a path-scoped block silently makes it
a **repository-wide** exception. This is easy to get wrong and impossible to
notice by reading the diff.

The rule:

- exempt a **location** → a block containing **only** `paths`
- exempt a **value** → a block containing **only** `regexes`, where the pattern
  cannot match a real credential

### Verify your change

```pwsh
pwsh -File tools/gitleaks-config-check.ps1
```

This suite runs on every CI build too. It asserts four things:

1. the real repository history scans clean;
2. a realistic secret in ordinary source is still **caught**;
3. a path exemption does not leak to sibling files;
4. an exempted credential is still caught when **moved out** of its path.

Checks 3 and 4 exist specifically to catch the union trap above. If you widen an
allowlist too far, this suite fails — that is the point.

## Known, reviewed exemptions

Everything currently exempted is documented inline in
[`.gitleaks.toml`](../../.gitleaks.toml) with its justification. Two categories
are worth knowing about up front:

- **Antigravity / Gemini CLI OAuth application credentials.** These are the
  well-known *public* client credentials of that OAuth application, shipped
  identically inside the official client. They identify the application, not a
  user, and are required for the OAuth flow to authorize at all. They are
  exempted only in the two files that hold them; the same value appearing
  anywhere else still fails the build.
- **Historical findings from deleted code.** The removed `openaibeta` plugin and
  a local `agent-go/config.json` contained public Google client constants and a
  local dev key. Those paths no longer exist on `dev`, so the findings only
  appear in the weekly full-history scan. They are exempted by **path prefix**,
  not by commit, so reintroducing those values in live code still trips the gate.
