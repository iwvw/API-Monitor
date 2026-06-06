/**
 * Local system routes.
 */

const express = require('express');
const os = require('os');
const path = require('path');
const { execFile } = require('child_process');

const router = express.Router();

const DISK_USAGE_CACHE_TTL_MS = 10_000;

let lastCpuSnapshot = null;
let diskUsageCache = null;

function clampPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.max(0, Math.min(100, number));
}

function readCpuSnapshot() {
  const totals = os.cpus().reduce(
    (acc, cpu) => {
      const times = cpu.times || {};
      const idle = Number(times.idle) || 0;
      const total = Object.values(times).reduce((sum, item) => sum + (Number(item) || 0), 0);
      return {
        idle: acc.idle + idle,
        total: acc.total + total,
      };
    },
    { idle: 0, total: 0 }
  );

  return {
    ...totals,
    at: Date.now(),
  };
}

function calculateCpuUsage(previous, current) {
  const idleDelta = current.idle - previous.idle;
  const totalDelta = current.total - previous.total;
  if (totalDelta <= 0) return 0;
  return clampPercent((1 - idleDelta / totalDelta) * 100);
}

function wait(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function getCpuUsage() {
  const current = readCpuSnapshot();

  if (lastCpuSnapshot) {
    const usage = calculateCpuUsage(lastCpuSnapshot, current);
    lastCpuSnapshot = current;
    return usage;
  }

  await wait(120);
  const sampled = readCpuSnapshot();
  const usage = calculateCpuUsage(current, sampled);
  lastCpuSnapshot = sampled;
  return usage;
}

function execFileAsync(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    execFile(command, args, { timeout: 2500, windowsHide: true, ...options }, (error, stdout) => {
      if (error) {
        reject(error);
        return;
      }

      resolve(stdout);
    });
  });
}

function getRootPath() {
  return path.parse(process.cwd()).root || '/';
}

async function getWindowsDiskUsage(rootPath) {
  const drive = rootPath.replace(/\\+$/, '');
  const safeDrive = /^[a-z]:$/i.test(drive) ? drive.toUpperCase() : 'C:';
  const command = [
    `$disk = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='${safeDrive}'";`,
    'if ($disk) {',
    '[pscustomobject]@{ Size=[int64]$disk.Size; Free=[int64]$disk.FreeSpace } | ConvertTo-Json -Compress',
    '}',
  ].join(' ');

  const stdout = await execFileAsync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', command]);
  const data = JSON.parse(stdout.trim() || '{}');
  const total = Number(data.Size) || 0;
  const free = Number(data.Free) || 0;
  const used = Math.max(0, total - free);

  return {
    root: safeDrive,
    total,
    used,
    free,
    usage: total > 0 ? clampPercent((used / total) * 100) : 0,
  };
}

async function getUnixDiskUsage(rootPath) {
  const stdout = await execFileAsync('df', ['-kP', rootPath]);
  const line = stdout.trim().split('\n')[1] || '';
  const parts = line.trim().split(/\s+/);
  const total = (Number(parts[1]) || 0) * 1024;
  const used = (Number(parts[2]) || 0) * 1024;
  const free = (Number(parts[3]) || 0) * 1024;

  return {
    root: parts[5] || rootPath,
    total,
    used,
    free,
    usage: total > 0 ? clampPercent((used / total) * 100) : 0,
  };
}

async function getDiskUsage() {
  const rootPath = getRootPath();
  const cached = diskUsageCache;

  if (cached && cached.rootPath === rootPath && Date.now() - cached.updatedAt < DISK_USAGE_CACHE_TTL_MS) {
    return cached.data;
  }

  try {
    const data = process.platform === 'win32'
      ? await getWindowsDiskUsage(rootPath)
      : await getUnixDiskUsage(rootPath);

    diskUsageCache = {
      rootPath,
      data,
      updatedAt: Date.now(),
    };

    return data;
  } catch (error) {
    const data = {
      root: rootPath,
      total: 0,
      used: 0,
      free: 0,
      usage: 0,
      error: error.message,
    };

    diskUsageCache = {
      rootPath,
      data,
      updatedAt: Date.now(),
    };

    return data;
  }
}

router.get('/host-metrics', async (req, res) => {
  try {
    const [cpuUsage, disk] = await Promise.all([getCpuUsage(), getDiskUsage()]);
    const totalMemory = os.totalmem();
    const freeMemory = os.freemem();
    const usedMemory = Math.max(0, totalMemory - freeMemory);
    const loadAverage = os.loadavg();
    const cpus = os.cpus();

    res.json({
      success: true,
      data: {
        hostname: os.hostname(),
        platform: process.platform,
        platformLabel: `${os.type()} ${os.release()}`,
        uptime: os.uptime(),
        cpu: {
          usage: cpuUsage,
          cores: cpus.length,
          model: cpus[0]?.model || '',
          loadAverage,
        },
        memory: {
          total: totalMemory,
          used: usedMemory,
          free: freeMemory,
          usage: totalMemory > 0 ? clampPercent((usedMemory / totalMemory) * 100) : 0,
        },
        disk,
        process: {
          uptime: process.uptime(),
          memoryRss: process.memoryUsage().rss,
        },
        timestamp: new Date().toISOString(),
      },
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
    });
  }
});

module.exports = router;
