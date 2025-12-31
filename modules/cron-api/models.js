const BaseModel = require('../../src/db/models/BaseModel');

class CronTaskModel extends BaseModel {
  constructor() {
    super('cron_tasks');
  }

  createTask(taskData) {
    const data = {
      name: taskData.name,
      schedule: taskData.schedule,
      command: taskData.command,
      type: taskData.type || 'shell',
      enabled: taskData.enabled !== undefined ? taskData.enabled : 1,
      created_at: Math.floor(Date.now() / 1000),
    };
    const result = this.insert(data);
    return { ...data, id: result.lastInsertRowid };
  }

  updateTask(id, updates) {
    const allowedFields = [
      'name',
      'schedule',
      'command',
      'type',
      'enabled',
      'last_run',
      'next_run',
    ];
    const data = {};
    Object.keys(updates).forEach(key => {
      if (allowedFields.includes(key)) {
        data[key] = updates[key];
      }
    });
    this.update(id, data);
    return this.findById(id);
  }
}

class CronLogModel extends BaseModel {
  constructor() {
    super('cron_logs');
  }

  createLog(logData) {
    const data = {
      task_id: logData.task_id,
      status: logData.status || 'running',
      output: logData.output || '',
      start_time: logData.start_time || Math.floor(Date.now() / 1000),
      end_time: logData.end_time,
      duration: logData.duration,
    };
    const result = this.insert(data);
    return { ...data, id: result.lastInsertRowid };
  }

  updateLog(id, updates) {
    const allowedFields = ['status', 'output', 'end_time', 'duration'];
    const data = {};
    Object.keys(updates).forEach(key => {
      if (allowedFields.includes(key)) {
        data[key] = updates[key];
      }
    });
    this.update(id, data);
  }

  getLogsByTask(taskId, limit = 50) {
    const db = this.getDatabase();
    return db
      .prepare(`SELECT * FROM ${this.tableName} WHERE task_id = ? ORDER BY start_time DESC LIMIT ?`)
      .all(taskId, limit);
  }

  getAllLogs(limit = 100) {
    const db = this.getDatabase();
    // Join with tasks to get task name
    return db
      .prepare(
        `
            SELECT l.*, t.name as task_name 
            FROM ${this.tableName} l
            LEFT JOIN cron_tasks t ON l.task_id = t.id
            ORDER BY l.start_time DESC LIMIT ?
         `
      )
      .all(limit);
  }

  cleanLogs(daysToKeep) {
    const db = this.getDatabase();
    const timeThreshold = Math.floor(Date.now() / 1000) - daysToKeep * 86400;
    return db.prepare(`DELETE FROM ${this.tableName} WHERE start_time < ?`).run(timeThreshold);
  }

  clearAllLogs() {
    const db = this.getDatabase();
    return db.prepare(`DELETE FROM ${this.tableName}`).run();
  }
}

module.exports = {
  CronTask: new CronTaskModel(),
  CronLog: new CronLogModel(),
};
