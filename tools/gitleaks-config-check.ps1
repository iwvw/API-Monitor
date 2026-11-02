# Regression suite for .gitleaks.toml
#
# Asserts the two properties that matter:
#   * CLEAN  - the repo's real history scans clean (no false positives)
#   * STRICT - a realistic secret is still caught, including inside paths and
#              positions that an over-broad allowlist would have hidden.
#
# Run: pwsh -File tools/gitleaks-config-check.ps1
# Requires: gitleaks on PATH (or -GitleaksPath), and git.

[CmdletBinding()]
param(
    [string]$GitleaksPath = 'gitleaks',
    [string]$RepoRoot = (Join-Path $PSScriptRoot '..')
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path $RepoRoot).Path
$config = Join-Path $repo '.gitleaks.toml'

if (-not (Test-Path $config)) { throw "missing config: $config" }

$failures = @()

function New-ProbeRepo {
    param([string]$Name)
    $dir = Join-Path ([System.IO.Path]::GetTempPath()) "gitleaks-probe-$Name"
    if (Test-Path $dir) { Remove-Item -Recurse -Force $dir }
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Push-Location $dir
    git init -q .
    git config user.email 'probe@example.invalid'
    git config user.name 'probe'
    Copy-Item $config (Join-Path $dir '.gitleaks.toml') -Force
    Pop-Location
    return $dir
}

function Invoke-Scan {
    param([string]$Dir)
    Push-Location $Dir
    try {
        & $GitleaksPath git --redact --config (Join-Path $Dir '.gitleaks.toml') --exit-code 1 *> $null
        return $LASTEXITCODE
    } finally { Pop-Location }
}

function Commit-All {
    param([string]$Dir, [string]$Message)
    Push-Location $Dir
    try {
        git add -A | Out-Null
        git commit -q -m $Message
    } finally { Pop-Location }
}

# ---------------------------------------------------------------------------
# 1. The real repository history must scan clean.
# ---------------------------------------------------------------------------
Write-Host '[1/4] real repository history scans clean ...' -NoNewline
Push-Location $repo
try {
    & $GitleaksPath git --redact --config $config --exit-code 1 *> $null
    $code = $LASTEXITCODE
} finally { Pop-Location }
if ($code -eq 0) { Write-Host ' OK' } else {
    Write-Host " FAIL (exit $code)"
    $failures += 'repository history is not clean'
}

# ---------------------------------------------------------------------------
# 2. Realistic secrets outside exempted paths must be caught.
#    These are randomly-shaped strings, NOT containing the word EXAMPLE,
#    which gitleaks' own denylist would otherwise skip.
# ---------------------------------------------------------------------------
Write-Host '[2/4] realistic secrets in normal source are caught ...' -NoNewline
$probe = New-ProbeRepo -Name 'strict'
Set-Content (Join-Path $probe 'app.js') 'const awsKey = "AKIA3XQZ7WPLM2NRTVBC";'
Set-Content (Join-Path $probe 'gh.js') 'const t = "ghp_9fK2mQ7xZpL3vRtY8nWcB4dHsJ6aE1gUoXzM";'
Commit-All -Dir $probe -Message strict
if ((Invoke-Scan -Dir $probe) -ne 0) { Write-Host ' OK' } else {
    Write-Host ' FAIL (secrets were not detected)'
    $failures += 'realistic secrets in normal source were not detected'
}
Remove-Item -Recurse -Force $probe -ErrorAction SilentlyContinue

# ---------------------------------------------------------------------------
# 3. A secret must NOT be hidden merely because it sits in a *different* file
#    inside a directory whose sibling file is exempted. Guards against the
#    "paths and regexes are a union" trap.
# ---------------------------------------------------------------------------
Write-Host '[3/4] path exemptions do not leak into siblings ...' -NoNewline
$probe = New-ProbeRepo -Name 'scope'
$antigravity = Join-Path $probe 'backend-go/internal/antigravity/engine/pkg/antigravity'
New-Item -ItemType Directory -Force -Path $antigravity | Out-Null
# exempted file: public app credential, should pass
Set-Content (Join-Path $antigravity 'oauth.go') 'var defaultClientSecret = "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"'
Commit-All -Dir $probe -Message exempted
$exemptedOk = (Invoke-Scan -Dir $probe) -eq 0
# non-exempted file elsewhere: must still fail
Set-Content (Join-Path $probe 'leaked.go') 'var awsKey = "AKIA3XQZ7WPLM2NRTVBC"'
Commit-All -Dir $probe -Message leaked
$siblingCaught = (Invoke-Scan -Dir $probe) -ne 0
if ($exemptedOk -and $siblingCaught) { Write-Host ' OK' } else {
    Write-Host " FAIL (exemptedOk=$exemptedOk siblingCaught=$siblingCaught)"
    $failures += 'path exemption leaked outside its scope'
}
Remove-Item -Recurse -Force $probe -ErrorAction SilentlyContinue

# ---------------------------------------------------------------------------
# 4. The same credential, moved OUT of its exempted path, must be caught.
# ---------------------------------------------------------------------------
Write-Host '[4/4] exempted credential is caught when relocated ...' -NoNewline
$probe = New-ProbeRepo -Name 'relocate'
Set-Content (Join-Path $probe 'elsewhere.go') 'var defaultClientSecret = "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"'
Commit-All -Dir $probe -Message relocate
if ((Invoke-Scan -Dir $probe) -ne 0) { Write-Host ' OK' } else {
    Write-Host ' FAIL (relocated credential was suppressed)'
    $failures += 'exempted credential was suppressed after relocation'
}
Remove-Item -Recurse -Force $probe -ErrorAction SilentlyContinue

Write-Host ''
if ($failures.Count -gt 0) {
    Write-Host "gitleaks config check FAILED ($($failures.Count)):" -ForegroundColor Red
    $failures | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    exit 1
}
Write-Host 'gitleaks config check passed.' -ForegroundColor Green
exit 0
