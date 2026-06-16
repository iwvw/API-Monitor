import fs from 'node:fs';
import process from 'node:process';

const checks = [
  ['backend-go/internal/manifest/manifest.go', 'Go route manifest'],
  ['backend-go/internal/server/server.go', 'Go HTTP router'],
  ['agent-rust/src/main.rs', 'Rust agent entrypoint'],
  ['src/js/App.jsx', 'Frontend app entry'],
];

let failed = false;
for (const [file, label] of checks) {
  if (fs.existsSync(file)) {
    console.log(`ok   ${label}: ${file}`);
  } else {
    failed = true;
    console.error(`fail ${label}: ${file}`);
  }
}

process.exit(failed ? 1 : 0);
