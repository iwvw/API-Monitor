const { randomUUID } = require('crypto');
const { createLogger } = require('../utils/logger');

const logger = createLogger('JobScheduler');
const jobs = new Map();

function normalizeJob(name, handler, options = {}) {
  if (!name || typeof name !== 'string') {
    throw new Error('Job name is required');
  }
  if (typeof handler !== 'function') {
    throw new Error(`Job ${name} handler must be a function`);
  }

  return {
    id: options.id || randomUUID(),
    name,
    handler,
    intervalMs: Number(options.intervalMs || options.interval || 0),
    jitterMs: Number(options.jitterMs || 0),
    running: false,
    timer: null,
    lastRunAt: null,
    nextRunAt: null,
    lastError: null,
  };
}

function scheduleNext(job) {
  if (!job.intervalMs) return;
  const jitter = job.jitterMs ? Math.floor(Math.random() * job.jitterMs) : 0;
  const delay = job.intervalMs + jitter;
  job.nextRunAt = new Date(Date.now() + delay).toISOString();
  job.timer = setTimeout(() => run(job.name), delay);
  if (job.timer.unref) job.timer.unref();
}

async function run(name, context = {}) {
  const job = jobs.get(name);
  if (!job) {
    throw new Error(`Job not registered: ${name}`);
  }
  if (job.running) {
    return { skipped: true, reason: 'running' };
  }

  job.running = true;
  job.lastRunAt = new Date().toISOString();
  job.lastError = null;

  try {
    const result = await job.handler(context);
    return { skipped: false, result };
  } catch (error) {
    job.lastError = error.message;
    logger.warn(`任务失败 ${name}: ${error.message}`);
    return { skipped: false, error };
  } finally {
    job.running = false;
    scheduleNext(job);
  }
}

function register(name, handler, options = {}) {
  if (jobs.has(name)) {
    stop(name);
  }
  const job = normalizeJob(name, handler, options);
  jobs.set(name, job);
  if (options.start !== false && job.intervalMs) {
    scheduleNext(job);
  }
  return job;
}

function stop(name) {
  const job = jobs.get(name);
  if (!job) return false;
  if (job.timer) clearTimeout(job.timer);
  job.timer = null;
  job.nextRunAt = null;
  return true;
}

function unregister(name) {
  stop(name);
  return jobs.delete(name);
}

function list() {
  return Array.from(jobs.values()).map(job => ({
    id: job.id,
    name: job.name,
    intervalMs: job.intervalMs,
    running: job.running,
    lastRunAt: job.lastRunAt,
    nextRunAt: job.nextRunAt,
    lastError: job.lastError,
  }));
}

module.exports = {
  register,
  run,
  stop,
  unregister,
  list,
};
