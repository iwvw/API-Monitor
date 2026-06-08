const { EventEmitter } = require('events');
const { randomUUID } = require('crypto');
const { createLogger } = require('../utils/logger');

const logger = createLogger('ToolboxEventBus');
const emitter = new EventEmitter();
emitter.setMaxListeners(100);

function publish(type, payload = {}, options = {}) {
  const event = {
    id: options.id || randomUUID(),
    type,
    module: options.module || type.split('.')[0],
    payload,
    severity: options.severity || 'info',
    createdAt: new Date().toISOString(),
  };

  emitter.emit(type, event);
  emitter.emit('*', event);
  logger.debug(`事件发布: ${type}`);
  return event;
}

function subscribe(type, handler) {
  emitter.on(type, handler);
  return () => emitter.off(type, handler);
}

function once(type, handler) {
  emitter.once(type, handler);
}

module.exports = {
  publish,
  subscribe,
  once,
  emitter,
};
