// Verifies the dependency gate's parsing and allowlist-decision logic.
//
// These are the paths that decide whether an advisory blocks a build. They are
// exercised here against synthetic reports so they can be checked without
// network access to the npm registry or the RustSec advisory database.
//
// Run: node tools/verify-audit-parsing.mjs

import {
  npmFindings,
  cargoFindings,
  dedupe,
  partitionFindings,
} from './dependency-audit.mjs';

let failures = 0;
function check(label, condition, detail = '') {
  if (condition) {
    console.log(`  ok   ${label}`);
  } else {
    console.log(`  FAIL ${label}${detail ? ` -- ${detail}` : ''}`);
    failures++;
  }
}

// ---------------------------------------------------------------------------
console.log('\ncargo report parsing');
// ---------------------------------------------------------------------------

const cargoWithFix = cargoFindings({
  vulnerabilities: {
    list: [
      {
        advisory: { id: 'RUSTSEC-2024-0001', package: 'foo', severity: 'high', title: 'Bad' },
        versions: { patched: ['>=1.2.3'] },
      },
    ],
  },
});
check('advisory with patched versions is fixable', cargoWithFix[0]?.fixAvailable === true);
check('advisory id is parsed', cargoWithFix[0]?.id === 'RUSTSEC-2024-0001');

const cargoNoFix = cargoFindings({
  vulnerabilities: {
    list: [
      {
        advisory: { id: 'RUSTSEC-2024-0002', package: 'bar', severity: 'medium', title: 'Unfixed' },
        versions: { patched: [] },
      },
    ],
  },
});
check('advisory with no patched versions is not fixable', cargoNoFix[0]?.fixAvailable === false);

// cargo-audit may report `patched: [null]` for an unfixed advisory.
const cargoNullPatched = cargoFindings({
  vulnerabilities: {
    list: [
      {
        advisory: { id: 'RUSTSEC-2024-0003', package: 'baz', severity: 'low', title: 'Null' },
        versions: { patched: [null] },
      },
    ],
  },
});
check('null patched entry counts as unfixed', cargoNullPatched[0]?.fixAvailable === false);

check('clean cargo report yields no findings', cargoFindings({ vulnerabilities: { list: [] } }).length === 0);
check('missing vulnerabilities key is tolerated', cargoFindings({}).length === 0);

// ---------------------------------------------------------------------------
console.log('\nnpm report parsing');
// ---------------------------------------------------------------------------

const npmReport = {
  vulnerabilities: {
    lodash: {
      severity: 'high',
      fixAvailable: true,
      via: [
        {
          url: 'https://github.com/advisories/GHSA-35jh-r3h4-6jhm',
          severity: 'high',
          title: 'Command Injection in lodash',
        },
      ],
    },
  },
};
const npmParsed = npmFindings(npmReport);
check('advisory id is extracted from the GHSA url', npmParsed[0]?.id === 'GHSA-35jh-r3h4-6jhm');
check('fixAvailable propagates', npmParsed[0]?.fixAvailable === true);

// A package vulnerable only transitively has string `via` entries; those must
// not crash the parser or invent advisory ids.
const npmTransitive = {
  vulnerabilities: {
    parent: { severity: 'high', fixAvailable: false, via: ['child'] },
  },
};
check('transitive-only `via` yields no findings', npmFindings(npmTransitive).length === 0);

// ---------------------------------------------------------------------------
console.log('\ndeduplication');
// ---------------------------------------------------------------------------

const dupes = dedupe([
  { id: 'A', package: 'p' },
  { id: 'A', package: 'p' },
  { id: 'A', package: 'q' },
]);
check('same id+package collapses, different package kept', dupes.length === 2);

// ---------------------------------------------------------------------------
console.log('\nallowlist decisions');
// ---------------------------------------------------------------------------

const today = new Date('2026-06-01T00:00:00Z');
const findings = [
  { id: 'ALLOWED-1', package: 'p', severity: 'high', fixAvailable: false, title: 't' },
  { id: 'BLOCKED-1', package: 'q', severity: 'high', fixAvailable: true, title: 't' },
];
const base = { ecosystem: 'npm', reason: 'r', owner: '@me' };

function run(entries) {
  return partitionFindings(findings, entries, 'npm', today);
}

let r = run([{ id: 'ALLOWED-1', expires: '2026-12-31', ...base }]);
check('valid unexpired entry suppresses exactly its advisory', r.ignored.length === 1 && r.ignored[0].id === 'ALLOWED-1');
check('unlisted advisory still blocks', r.blocking.length === 1 && r.blocking[0].id === 'BLOCKED-1');
check('no entry problems for a valid entry', r.entryProblems.length === 0);

r = run([{ id: 'ALLOWED-1', expires: '2020-01-01', ...base }]);
check('expired entry does not suppress', r.ignored.length === 0 && r.blocking.length === 2);
check('expired entry is reported as a problem', r.entryProblems.some((p) => p.problems.join(' ').includes('expired')));

r = run([{ id: 'ALLOWED-1', expires: '2026-12-31', ...base, fixAvailable: true }]);
check('entry marked fixAvailable is rejected', r.ignored.length === 0);

r = run([{ id: 'ALLOWED-1', ecosystem: 'cargo', expires: '2026-12-31', reason: 'r', owner: '@me' }]);
check('wrong-ecosystem entry does not suppress', r.ignored.length === 0);
check('wrong-ecosystem entry is not warned about at all', r.entryProblems.length === 0);

r = run([{ id: 'ALLOWED-1', ecosystem: 'npm' }]);
check('entry missing required fields is rejected', r.ignored.length === 0);
check('missing fields are named in the problem', r.entryProblems[0].problems.some((p) => p.includes('reason')));

r = run([{ id: 'ALLOWED-1', expires: 'not-a-date', ...base }]);
check('unparseable expiry is rejected', r.ignored.length === 0);

r = run([]);
check('empty allowlist blocks everything', r.blocking.length === 2);

// Expiry boundary: an entry expiring today is still valid (end-of-day).
r = run([{ id: 'ALLOWED-1', expires: '2026-06-01', ...base }]);
check('entry expiring today is still valid', r.ignored.length === 1);

r = run([{ id: 'ALLOWED-1', expires: '2026-05-31', ...base }]);
check('entry expiring yesterday is invalid', r.ignored.length === 0);

// ---------------------------------------------------------------------------
console.log('');
if (failures > 0) {
  console.error(`verify-audit-parsing FAILED: ${failures} check(s) failed.`);
  process.exit(1);
}
console.log('verify-audit-parsing passed.');
process.exit(0);
