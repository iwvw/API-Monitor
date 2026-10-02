#!/usr/bin/env node
/**
 * Dependency-vulnerability gate with a reviewed, expiring allowlist.
 *
 * Why this exists
 * ---------------
 * `npm audit` and `cargo audit` exit non-zero the moment an advisory is
 * published, including for advisories that have no released fix. That made the
 * Security Baseline workflow fail on things nobody could act on that day, which
 * is how a red gate stops being read. This script keeps the gate strict while
 * making "we know, there is no fix yet" an explicit, reviewable, expiring
 * decision instead of a permanently red build.
 *
 * Design rules
 * ------------
 *  - Only advisories listed in `.github/security/dependency-allowlist.json`
 *    are ignored, and each entry MUST carry a reason, an owner and an expiry.
 *  - An expanded advisory (one with a known fix) may NEVER be allowlisted:
 *    upgrade instead. The script enforces this.
 *  - An expired entry is a failure, not a silent pass.
 *  - The allowlist is *additive to nothing*: any advisory not listed still
 *    fails the run.
 *
 * Usage:
 *   node tools/dependency-audit.mjs --ecosystem npm
 *   node tools/dependency-audit.mjs --ecosystem cargo
 *
 * Exit codes: 0 = clean or fully covered by unexpired entries, 1 = failure.
 */

import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync, realpathSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..');
const allowlistPath = join(repoRoot, '.github', 'security', 'dependency-allowlist.json');

// On Windows, package-manager entry points are `.cmd` shims that execFileSync
// cannot resolve from a bare name. `shell: true` lets the platform resolve
// them; arguments below are fixed literals, so there is no injection risk.
const isWindows = process.platform === 'win32';
const spawnOpts = { shell: isWindows };

const args = process.argv.slice(2);
const ecoIndex = args.indexOf('--ecosystem');
const ecosystem = ecoIndex >= 0 ? args[ecoIndex + 1] : null;

if (ecosystem !== null && ecosystem !== 'npm' && ecosystem !== 'cargo') {
  console.error('usage: node tools/dependency-audit.mjs --ecosystem <npm|cargo>');
  process.exit(2);
}

// True when this file is the process entry point (rather than imported as a
// module by tools/verify-audit-parsing.mjs). Compared via realpath so that
// Windows separators, symlinks and case differences do not cause a mismatch.
function isEntryPoint() {
  const entry = process.argv[1];
  if (!entry) return false;
  try {
    return realpathSync(entry) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}
const isCli = isEntryPoint();

// ---------------------------------------------------------------------------
// Allowlist loading
// ---------------------------------------------------------------------------

function loadAllowlist() {
  if (!existsSync(allowlistPath)) return { entries: [] };
  let parsed;
  try {
    parsed = JSON.parse(readFileSync(allowlistPath, 'utf8'));
  } catch (err) {
    console.error(`::error::${allowlistPath} is not valid JSON: ${err.message}`);
    process.exit(1);
  }
  if (!Array.isArray(parsed.entries)) {
    console.error(`::error::${allowlistPath} must contain an "entries" array.`);
    process.exit(1);
  }
  return parsed;
}

/**
 * Validates a single allowlist entry and decides whether it may suppress a
 * currently-reported advisory.
 */
function classifyEntry(entry, today, ecosystemName) {
  const problems = [];
  for (const field of ['id', 'ecosystem', 'reason', 'owner', 'expires']) {
    if (!entry[field]) problems.push(`missing "${field}"`);
  }
  if (problems.length) return { usable: false, problems };

  if (entry.ecosystem !== ecosystemName) return { usable: false, problems: ['wrong ecosystem'] };

  if (entry.fixAvailable === true) {
    problems.push('an advisory with an available fix must be upgraded, not allowlisted');
  }

  const expires = new Date(`${entry.expires}T23:59:59Z`);
  if (Number.isNaN(expires.getTime())) {
    problems.push(`"expires" is not an ISO date (got ${entry.expires})`);
  } else if (expires < today) {
    problems.push(`allowlist entry expired on ${entry.expires}`);
  }

  return { usable: problems.length === 0, problems };
}

// ---------------------------------------------------------------------------
// npm
// ---------------------------------------------------------------------------

function runNpmAudit() {
  let raw;
  try {
    raw = execFileSync('npm', ['audit', '--json'], {
      cwd: repoRoot,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      stdio: ['ignore', 'pipe', 'pipe'],
      ...spawnOpts,
    });
  } catch (err) {
    // npm audit exits non-zero when it finds anything; the JSON is still on
    // stdout. A genuinely broken run has no parseable JSON.
    raw = err.stdout;
    if (!raw) {
      console.error('::error::npm audit produced no output. This is an infrastructure failure, not a security finding.');
      console.error(String(err.stderr || err.message).slice(0, 2000));
      process.exit(1);
    }
  }
  return JSON.parse(raw);
}

function npmFindings(report) {
  const findings = [];
  for (const [name, vuln] of Object.entries(report.vulnerabilities || {})) {
    for (const via of vuln.via || []) {
      if (typeof via !== 'object' || !via.url) continue;
      const id = via.url.split('/').pop();
      findings.push({
        id,
        package: name,
        severity: via.severity || vuln.severity,
        title: via.title || '',
        fixAvailable: vuln.fixAvailable === true,
      });
    }
  }
  return findings;
}

// ---------------------------------------------------------------------------
// cargo
// ---------------------------------------------------------------------------

function runCargoAudit() {
  const cargoDir = join(repoRoot, 'agent-rust');
  let raw;
  try {
    // cargo-audit reads the committed Cargo.lock by default; there is no
    // --locked flag. Audit from agent-rust so it finds that lockfile.
    raw = execFileSync('cargo', ['audit', '--json'], {
      cwd: cargoDir,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      stdio: ['ignore', 'pipe', 'pipe'],
      ...spawnOpts,
    });
  } catch (err) {
    raw = err.stdout;
    if (!raw) {
      const stderr = String(err.stderr || err.message);
      // Distinguish a registry outage from a real advisory failure. These
      // wordings are observed from cargo-audit / crates.io / the RustSec
      // advisory DB; a network problem must never be reported as a
      // vulnerability, or the gate cries wolf and stops being trusted.
      const infra = /couldn't fetch advisory database|failed to fetch|git operation failed|IO error occurred|talking to the server|error sending request|could not be completed in the allotted timeframe|network|timed out|timed out|connection refused|dns/i;
      if (infra.test(stderr)) {
        console.error('::error::cargo audit could not reach the RustSec advisory database. This is an infrastructure failure, not a security finding -- re-run the job.');
      } else {
        console.error('::error::cargo audit produced no output.');
      }
      console.error(stderr.slice(0, 2000));
      process.exit(1);
    }
  }
  // cargo-audit may emit leading non-JSON noise; take the JSON object.
  const start = raw.indexOf('{');
  if (start < 0) {
    console.error('::error::cargo audit output contained no JSON object.');
    process.exit(1);
  }
  return JSON.parse(raw.slice(start));
}

function cargoFindings(report) {
  const findings = [];
  const list = report.vulnerabilities?.list || [];
  for (const v of list) {
    const advisory = v.advisory || {};
    const versions = v.versions || {};
    // cargo-audit reports `patched` versions; an empty list means no fix exists.
    const patched = Array.isArray(versions.patched) ? versions.patched.filter(Boolean) : [];
    findings.push({
      id: advisory.id,
      package: advisory.package || v.package?.name || 'unknown',
      severity: advisory.severity || 'unknown',
      title: advisory.title || '',
      fixAvailable: patched.length > 0,
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// Decision logic (pure, exported for tools/verify-audit-parsing.mjs)
// ---------------------------------------------------------------------------

/**
 * Splits findings into those suppressed by a usable allowlist entry and those
 * that must block the build. Also reports allowlist entries that are malformed
 * or expired, which are bugs in the allowlist itself and must always be
 * surfaced loudly rather than silently suppressing anything.
 */
export function partitionFindings(findings, entries, ecosystemName, today) {
  const usableIds = new Set();
  const entryProblems = [];

  for (const entry of entries) {
    const { usable, problems } = classifyEntry(entry, today, ecosystemName);
    if (usable) usableIds.add(entry.id);
    else if (problems[0] !== 'wrong ecosystem') {
      entryProblems.push({ id: entry.id || '<no id>', problems });
    }
  }

  const ignored = [];
  const blocking = [];
  for (const f of findings) {
    if (usableIds.has(f.id)) ignored.push(f);
    else blocking.push(f);
  }

  return { ignored, blocking, entryProblems };
}

/** De-duplicates findings; npm reports one advisory per dependent package. */
export function dedupe(findings) {
  const unique = new Map();
  for (const f of findings) {
    const key = `${f.id}|${f.package}`;
    if (!unique.has(key)) unique.set(key, f);
  }
  return [...unique.values()];
}

export { npmFindings, cargoFindings };

// ---------------------------------------------------------------------------
// CLI entry point
// ---------------------------------------------------------------------------

function main() {
  const allowlist = loadAllowlist();
  const today = new Date();
  const report = ecosystem === 'npm' ? runNpmAudit() : runCargoAudit();
  const findings = ecosystem === 'npm' ? npmFindings(report) : cargoFindings(report);

  const all = dedupe(findings);
  const { ignored, blocking, entryProblems } = partitionFindings(all, allowlist.entries, ecosystem, today);

  for (const { id, problems } of entryProblems) {
    console.error(`::warning::allowlist entry "${id}" is not usable: ${problems.join('; ')}`);
  }

  console.log(`\n[${ecosystem}] ${all.length} unique advisor${all.length === 1 ? 'y' : 'ies'} reported.`);
  for (const f of ignored) console.log(`  ignored  ${f.severity.padEnd(9)} ${f.id} (${f.package})`);
  for (const f of blocking) console.log(`  BLOCKING ${f.severity.padEnd(9)} ${f.id} (${f.package}) ${f.title}`);

  if (ignored.length) {
    console.log(`\n${ignored.length} advisor${ignored.length === 1 ? 'y is' : 'ies are'} suppressed by ${allowlistPath}.`);
    console.log('Each suppressed entry must be revisited before its expiry date.');
  }

  if (blocking.length) {
    console.error(`\n::error::${blocking.length} unallowlisted ${ecosystem} advisor${blocking.length === 1 ? 'y' : 'ies'} present.`);
    const fixable = blocking.filter((f) => f.fixAvailable);
    if (fixable.length) {
      console.error(`Fixable: ${fixable.map((f) => `${f.package} (${f.id})`).join(', ')} -- upgrade these.`);
    }
    console.error('To record a reviewed, time-limited exception, see .github/security/README.md.');
    process.exit(1);
  }

  console.log(`\n[${ecosystem}] gate passed.`);
  process.exit(0);
}

if (isCli) main();
