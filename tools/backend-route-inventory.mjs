import fs from 'node:fs';

const manifest = fs.readFileSync('backend-go/internal/manifest/manifest.go', 'utf8');
const routes = [...manifest.matchAll(/\{Prefix:\s*"([^"]+)",\s*Module:\s*"([^"]+)",\s*Owner:\s*(Owner\w+)/g)].map(
  ([, prefix, module, owner]) => ({ prefix, module, owner: owner.replace(/^Owner/, '').toLowerCase() }),
);

for (const route of routes) {
  console.log(`${route.owner.padEnd(7)} ${route.prefix.padEnd(56)} ${route.module}`);
}
