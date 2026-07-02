import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const errors = [];
const warnings = [];

const sourceDirs = ['src/js', 'backend-go', 'agent-rust', '.github'];
const frontendDirs = ['src/js'];
const skipDirs = new Set(['.git', 'node_modules', 'dist', 'data', 'docs/archive']);

const legacyFiles = [
  'agent-rust/src/main.rs.backup',
  'agent-rust/src/main_new.rs',
  'agent-rust/src/main_old_protocol.rs',
  'src/test-socket.html',
  'API_MISMATCH_REPORT.md',
  'FRONTEND_BROWSER_DIAGNOSTICS.md',
  'FRONTEND_DISPLAY_DIAGNOSIS.md',
  'GO_BACKEND_MIGRATION_COMPLETE.md',
  'MIGRATION_CHECKLIST.md',
  'PROJECT_UPDATE_SUMMARY.md',
  'backend-go/API_COMPLETE_FIX_REPORT.md',
  'backend-go/API_FIX_COMPLETE_REPORT.md',
  'backend-go/FRONTEND_REALTIME_UPDATE_FIX.md',
  'backend-go/REALTIME_METRICS_FIX.md',
];

const retiredFrontendPatterns = [
  /\bMusicPage\b/i,
  /\/api\/music\b/i,
  /\/api\/openlist\b/i,
  /\bOpenList\b/i,
];

const allowedRetiredBackendFiles = [
  'backend-go/cmd/schema-audit/main.go',
  'backend-go/internal/settings/database_maintenance.go',
  'backend-go/internal/manifest/manifest.go',
  'backend-go/internal/manifest/manifest_test.go',
  'backend-go/internal/server/server.go',
  'backend-go/internal/server/server_test.go',
  'backend-go/internal/notification/service.go',
  'backend-go/internal/notification/service_test.go',
];

const allowedUnmanifested = [
  /^\/agent\/agent-[\w.-]+$/,
  /^\/v1$/,
  /^\/api$/,
  /^\/api\/server\/agent\/install$/,
];

function toPosix(file) {
  return file.split(path.sep).join('/');
}

function exists(rel) {
  return fs.existsSync(path.join(root, rel));
}

function walk(dir, out = []) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs)) return out;
  for (const entry of fs.readdirSync(abs, { withFileTypes: true })) {
    const rel = toPosix(path.join(dir, entry.name));
    if (entry.isDirectory()) {
      if (!skipDirs.has(rel) && !skipDirs.has(entry.name)) walk(rel, out);
      continue;
    }
    out.push(rel);
  }
  return out;
}

function read(rel) {
  return fs.readFileSync(path.join(root, rel), 'utf8');
}

function stripComments(content) {
  return content
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')
    .replace(/^\s*#.*$/gm, '');
}

function parseManifest() {
  const rel = 'backend-go/internal/manifest/manifest.go';
  if (!exists(rel)) {
    errors.push(`missing required manifest: ${rel}`);
    return [];
  }
  const content = read(rel);
  const routeRe = /\{Prefix:\s*"([^"]+)",\s*Module:\s*"([^"]+)",\s*Owner:\s*(Owner\w+)[\s\S]*?(?:MatchMode:\s*(Match\w+))?[\s\S]*?\}/g;
  const routes = [];
  let match;
  while ((match = routeRe.exec(content))) {
    routes.push({
      prefix: match[1],
      module: match[2],
      owner: match[3].replace(/^Owner/, '').toLowerCase(),
      matchMode: match[4] ? match[4].replace(/^Match/, '').toLowerCase() : 'prefix',
    });
  }
  if (routes.length === 0) errors.push('manifest has no parseable routes');
  return routes;
}

function splitRoute(value) {
  return value.replace(/[?#].*$/, '').replace(/\/+$/, '') || '/';
}

function matchesPattern(routePath, pattern) {
  const routeParts = splitRoute(routePath).split('/').filter(Boolean);
  const patternParts = splitRoute(pattern).split('/').filter(Boolean);
  if (routeParts.length !== patternParts.length) return false;
  return patternParts.every((part, index) => {
    return (part.startsWith('{') && part.endsWith('}') && routeParts[index]) || part === routeParts[index];
  });
}

function matchesRoute(routePath, route) {
  const cleanPath = splitRoute(routePath);
  if (route.matchMode === 'exact') return cleanPath === splitRoute(route.prefix);
  if (route.matchMode === 'pattern') return matchesPattern(cleanPath, route.prefix);
  const prefix = splitRoute(route.prefix);
  return cleanPath === prefix || cleanPath.startsWith(`${prefix}/`);
}

function bestRoute(routePath, routes) {
  let best = null;
  let score = -1;
  for (const route of routes) {
    if (!matchesRoute(routePath, route)) continue;
    const routeScore =
      route.prefix.length + (route.matchMode === 'exact' ? 2000 : route.matchMode === 'pattern' ? 1000 : 0);
    if (routeScore > score) {
      best = route;
      score = routeScore;
    }
  }
  return best;
}

function extractRouteLiterals(content) {
  const routes = new Set();
  const literalRe = /(['"`])([^'"`]*?(?:\/api(?:\/|$)|\/v1(?:\/|$)|\/socket\.io\/?|\/ws\/|\/health)[^'"`]*)\1/g;
  let match;
  while ((match = literalRe.exec(content))) {
    const literal = match[2];
    const normalizedLiteral = literal.replace(/\$\{[^}]+\}/g, '{id}');
    for (const found of normalizedLiteral.matchAll(/\/(?:api|v1|socket\.io|ws|health)(?:\/[A-Za-z0-9_.~:{}?=&%-]*)*/g)) {
      const raw = found[0];
      const cleaned = splitRoute(raw);
      if (/\/[^/{}]+\{id\}/.test(cleaned)) continue;
      if (cleaned.includes('{id}') && cleaned.replace(/\{id\}/g, '').endsWith('/')) continue;
      if (cleaned !== '/') routes.add(cleaned);
    }
  }
  return [...routes];
}

function checkLegacyFiles() {
  for (const rel of legacyFiles) {
    if (exists(rel)) errors.push(`legacy file should stay removed: ${rel}`);
  }
}

function checkRetiredModules() {
  if (exists('src/js/pages/MusicPage.jsx')) errors.push('retired Music page still exists: src/js/pages/MusicPage.jsx');
  for (const rel of walk('src/js')) {
    const content = stripComments(read(rel));
    for (const pattern of retiredFrontendPatterns) {
      if (pattern.test(content)) errors.push(`retired module reference in frontend: ${rel} matches ${pattern}`);
    }
  }
  for (const rel of walk('backend-go')) {
    if (allowedRetiredBackendFiles.includes(rel)) continue;
    const content = stripComments(read(rel));
    if (/\/api\/music\b|\/api\/openlist\b|\bOpenList\b|\bmusic-api\b|\bopenlist-api\b|\bMusicPage\b/i.test(content)) {
      errors.push(`retired module reference outside manifest/tests: ${rel}`);
    }
  }
}

function checkFrontendRoutes(routes) {
  const seen = new Map();
  for (const dir of frontendDirs) {
    for (const rel of walk(dir)) {
      if (!/\.(js|jsx|ts|tsx|mjs|cjs)$/.test(rel)) continue;
      for (const routePath of extractRouteLiterals(stripComments(read(rel)))) {
        if (allowedUnmanifested.some((pattern) => pattern.test(routePath))) continue;
        if (!seen.has(routePath)) seen.set(routePath, []);
        seen.get(routePath).push(rel);
      }
    }
  }

  for (const [routePath, files] of [...seen.entries()].sort()) {
    const route = bestRoute(routePath, routes);
    if (!route) {
      errors.push(`frontend route is not covered by Go manifest: ${routePath} (${files.slice(0, 3).join(', ')})`);
      continue;
    }
    if (route.owner === 'retired') {
      errors.push(
        `frontend route resolves to retired backend route: ${routePath} -> ${route.prefix} (${files
          .slice(0, 3)
          .join(', ')})`,
      );
    }
  }
}

function checkRouteImplementation(routes) {
  const serverGo = exists('backend-go/internal/server/server.go') ? read('backend-go/internal/server/server.go') : '';
  for (const route of routes) {
    if (route.owner !== 'go') continue;
    if (route.prefix.startsWith('/api/server/')) continue;
    if (route.matchMode === 'prefix' && serverGo.includes(`case "${route.prefix}"`)) continue;
    if (serverGo.includes(`"${route.prefix}"`)) continue;
    warnings.push(`Go manifest route may not be dispatched explicitly: ${route.prefix}`);
  }
}

function checkProductionFlow() {
  const dockerfile = exists('Dockerfile') ? read('Dockerfile') : '';
  const compose = exists('docker-compose.yml') ? read('docker-compose.yml') : '';
  const ci = exists('.github/workflows/ci-cd.yml') ? read('.github/workflows/ci-cd.yml') : '';

  if (!/\/health/.test(dockerfile)) errors.push('Dockerfile healthcheck must use /health');
  if (/ADMIN_PASSWORD\s*=\s*(?!\$\{)[^\n]+/.test(compose)) errors.push('docker-compose.yml must not hardcode ADMIN_PASSWORD');
  if (/JWT_SECRET\s*=\s*(?!\$\{)[^\n]+/.test(compose)) errors.push('docker-compose.yml must not hardcode JWT_SECRET');
  if (!/npm run governance:check/.test(ci)) warnings.push('CI does not run npm run governance:check yet');
}

function checkRequiredToolScripts() {
  const pkg = JSON.parse(read('package.json'));
  for (const [name, command] of Object.entries(pkg.scripts || {})) {
    for (const match of command.matchAll(/node\s+(tools\/[^\s]+)/g)) {
      if (!exists(match[1])) errors.push(`package script "${name}" references missing tool: ${match[1]}`);
    }
  }
}

function checkAgentEntrypoints() {
  for (const rel of walk('agent-rust/src')) {
    if (/main_(new|old)|backup/.test(rel)) errors.push(`legacy Rust agent entrypoint remains: ${rel}`);
  }
}

function main() {
  const routes = parseManifest();
  checkLegacyFiles();
  checkRetiredModules();
  checkFrontendRoutes(routes);
  checkRouteImplementation(routes);
  checkProductionFlow();
  checkRequiredToolScripts();
  checkAgentEntrypoints();

  if (warnings.length) {
    console.log('Three-side governance warnings:');
    for (const warning of warnings) console.log(`  - ${warning}`);
    console.log('');
  }

  if (errors.length) {
    console.error('Three-side governance check failed:');
    for (const error of errors) console.error(`  - ${error}`);
    process.exit(1);
  }

  console.log(`Three-side governance check passed (${routes.length} manifest routes).`);
}

main();
