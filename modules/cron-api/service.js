const cron = require('node-cron');
const { exec } = require('child_process');
const { CronTask, CronLog } = require('./models');
const axios = require('axios');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('Cron');

class CronService {
  constructor() {
    this.scheduledTasks = new Map(); // Map<taskId, cronJob>
    this.initialized = false;
  }

  initialize() {
    if (this.initialized) return;
    this.reloadAll();
    this.initialized = true;
  }

  reloadAll() {
    // Stop all existing
    this.scheduledTasks.forEach(job => job.stop());
    this.scheduledTasks.clear();

    const tasks = CronTask.findAll();
    tasks.forEach(task => {
      if (task.enabled) {
        this.scheduleTask(task);
      }
    });
    logger.info(`Loaded ${tasks.length} tasks, ${this.scheduledTasks.size} active.`);
  }

  scheduleTask(task) {
    if (!cron.validate(task.schedule)) {
      logger.error(`Invalid schedule for task ${task.id}: ${task.schedule}`);
      return;
    }

    const job = cron.schedule(task.schedule, () => {
      this.executeTask(task.id);
    });
    this.scheduledTasks.set(task.id, job);
  }

  reloadTask(taskId) {
    if (this.scheduledTasks.has(taskId)) {
      this.scheduledTasks.get(taskId).stop();
      this.scheduledTasks.delete(taskId);
    }

    const task = CronTask.findById(taskId);
    if (task && task.enabled) {
      this.scheduleTask(task);
    }
  }

  async executeTask(taskId) {
    const task = CronTask.findById(taskId);
    if (!task) return;

    const startTime = Math.floor(Date.now() / 1000);
    const log = CronLog.createLog({
      task_id: task.id,
      status: 'running',
      start_time: startTime,
    });

    logger.info(`Executing task ${task.name} (${task.id})`);

    try {
      let output = '';
      if (task.type === 'http') {
        // Simple HTTP GET for now
        const res = await axios.get(task.command, { timeout: 30000 });
        output = `Status: ${res.status}\nData: ${typeof res.data === 'object' ? JSON.stringify(res.data) : res.data}`;
      } else if (task.type === 'internal') {
        // 内部 API 调用：直接请求本地服务器
        // command 格式: GET /api/xxx 或 POST /api/xxx
        const parts = task.command.trim().split(/\s+/);
        const method = parts.length > 1 ? parts[0].toUpperCase() : 'GET';
        const path = parts.length > 1 ? parts[1] : parts[0];

        // 获取服务器端口
        const port = process.env.PORT || 3000;
        const url = `http://localhost:${port}${path.startsWith('/') ? path : '/' + path}`;

        const res = await axios({
          method: method,
          url: url,
          timeout: 60000,
          headers: { 'X-Internal-Cron': 'true' },
        });
        output = `Status: ${res.status}\nData: ${typeof res.data === 'object' ? JSON.stringify(res.data, null, 2) : res.data}`;
      } else {
        // Shell
        output = await new Promise((resolve, reject) => {
          exec(task.command, { timeout: 60000 * 5 }, (error, stdout, stderr) => {
            if (error) {
              reject({ error, stdout, stderr });
            } else {
              resolve(stdout + (stderr ? '\nStderr: ' + stderr : ''));
            }
          });
        });
      }

      const endTime = Math.floor(Date.now() / 1000);
      CronLog.updateLog(log.id, {
        status: 'success',
        output: output ? output.substring(0, 5000) : '(no output)',
        end_time: endTime,
        duration: endTime - startTime,
      });

      CronTask.updateTask(task.id, {
        last_run: endTime,
      });
    } catch (err) {
      const endTime = Math.floor(Date.now() / 1000);
      let errorOutput = '';
      if (err.error) {
        errorOutput = `Error: ${err.error.message}\nStdout: ${err.stdout}\nStderr: ${err.stderr}`;
      } else {
        errorOutput = err.message || String(err);
      }

      CronLog.updateLog(log.id, {
        status: 'failed',
        output: errorOutput.substring(0, 5000),
        end_time: endTime,
        duration: endTime - startTime,
      });

      CronTask.updateTask(task.id, {
        last_run: endTime,
      });
    }
  }
}

module.exports = new CronService();
