import process from 'node:process';

console.log('Backend performance baseline placeholder: run against a live service before release benchmarking.');
console.log(`API_MONITOR_BASE_URL=${process.env.API_MONITOR_BASE_URL || 'http://127.0.0.1:3000'}`);
