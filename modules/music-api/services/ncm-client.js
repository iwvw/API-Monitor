const { createLogger } = require('../../../src/utils/logger');
const authService = require('./auth-service');

const logger = createLogger('NcmClient');

class NcmClient {
  constructor() {
    this.api = null;
  }

  load() {
    if (this.api) return this.api;
    try {
      this.api = require('@neteasecloudmusicapienhanced/api');
      logger.success('NCM API loaded from npm package');
      return this.api;
    } catch (error) {
      logger.error('Failed to load NCM API:', error.message);
      logger.warn('Please install: npm install @neteasecloudmusicapienhanced/api');
      return null;
    }
  }

  hasMethod(moduleName) {
    const api = this.load();
    return !!api && typeof api[moduleName] === 'function';
  }

  async invoke(moduleName, params = {}, reqCookieHeader = '') {
    const api = this.load();
    if (!api || typeof api[moduleName] !== 'function') {
      const error = new Error(`API method ${moduleName} not found`);
      error.status = 404;
      throw error;
    }

    const result = await api[moduleName]({
      ...params,
      cookie: authService.getEffectiveCookie(reqCookieHeader),
    });

    if (result.cookie && Array.isArray(result.cookie)) {
      authService.mergeResponseCookies(result.cookie);
    }

    return result;
  }

  getHealth(options = {}) {
    const api = options.load === true ? this.load() : this.api;
    return {
      modulesLoaded: !!api,
      moduleCount: api ? Object.keys(api).filter(key => typeof api[key] === 'function').length : 0,
    };
  }
}

module.exports = new NcmClient();
