import http from 'node:http';
import process from 'node:process';

const baseUrl = process.env.API_MONITOR_BASE_URL || 'http://127.0.0.1:3000';

function request(path) {
  return new Promise((resolve, reject) => {
    const req = http.get(`${baseUrl}${path}`, (res) => {
      res.resume();
      res.on('end', () => resolve(res.statusCode));
    });
    req.on('error', reject);
    req.setTimeout(5000, () => {
      req.destroy(new Error(`timeout requesting ${path}`));
    });
  });
}

const health = await request('/health');
if (health !== 200) {
  console.error(`Go backend smoke failed: /health returned ${health}`);
  process.exit(1);
}

console.log(`Go backend smoke passed: ${baseUrl}/health`);
