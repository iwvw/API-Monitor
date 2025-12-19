/**
 * 用户设置管理服务（使用 SQLite 数据库）
 * 负责保存和加载用户的个性化设置（自定义CSS、模块配置等）
 */

const { UserSettings } = require('../db/models');
const dbService = require('../db/database');

// 初始化数据库
dbService.initialize();

/**
 * 加载用户设置
 */
function loadUserSettings() {
  try {
    const settings = UserSettings.getSettings();

    const visibility = settings.module_visibility || {
      zeabur: true,
      dns: true,
      openai: true,
      server: true,
      antigravity: true,
      'gemini-cli': true
    };

    const channelEnabled = settings.channel_enabled || {
      antigravity: true,
      'gemini-cli': true
    };

    const channelModelPrefix = settings.channel_model_prefix || {
      antigravity: '',
      'gemini-cli': ''
    };

    const order = settings.module_order || ['zeabur', 'dns', 'openai', 'server', 'antigravity', 'gemini-cli'];

    // 确保 gemini-cli 在现有设置中
    if (!('gemini-cli' in visibility)) {
      visibility['gemini-cli'] = true;
    }
    if (!order.includes('gemini-cli')) {
      order.push('gemini-cli');
    }

    return {
      customCss: settings.custom_css || '',
      zeaburRefreshInterval: settings.zeabur_refresh_interval || 30000,
      moduleVisibility: visibility,
      channelEnabled: channelEnabled,
      channelModelPrefix: channelModelPrefix,
      moduleOrder: order,
      load_balancing_strategy: settings.load_balancing_strategy || 'random'
    };
  } catch (error) {
    console.error('加载用户设置失败:', error);
    return getDefaultSettings();
  }
}

/**
 * 保存用户设置
 */
function saveUserSettings(settings) {
  try {
    // 转换字段名
    const dbSettings = {
      custom_css: settings.customCss || settings.custom_css || '',
      zeabur_refresh_interval: settings.zeaburRefreshInterval || settings.zeabur_refresh_interval || 30000,
      module_visibility: settings.moduleVisibility || settings.module_visibility,
      channel_enabled: settings.channelEnabled || settings.channel_enabled,
      channel_model_prefix: settings.channelModelPrefix || settings.channel_model_prefix,
      module_order: settings.moduleOrder || settings.module_order,
      load_balancing_strategy: settings.load_balancing_strategy || settings.load_balancing_strategy_form || 'random'
    };

    // 确保 channel_model_prefix 是字符串，如果不是则进行 JSON.stringify
    if (dbSettings.channel_model_prefix && typeof dbSettings.channel_model_prefix !== 'string') {
      const originalPrefix = dbSettings.channel_model_prefix;
      dbSettings.channel_model_prefix = JSON.stringify(dbSettings.channel_model_prefix);
      console.log('[DEBUG] Stringified channel_model_prefix for saving:', originalPrefix, '->', dbSettings.channel_model_prefix);
    }

    console.log('[DEBUG] Saving User Settings:', JSON.stringify(dbSettings, null, 2));
    UserSettings.updateSettings(dbSettings);
    return { success: true };
  } catch (error) {
    console.error('保存用户设置失败:', error);
    return { success: false, error: error.message };
  }
}

/**
 * 获取默认设置
 */
function getDefaultSettings() {
  return {
    customCss: '',
    zeaburRefreshInterval: 30000,
    moduleVisibility: {
      zeabur: true,
      dns: true,
      openai: true,
      server: true,
      antigravity: true,
      'gemini-cli': true
    },
    channelEnabled: {
      antigravity: true,
      'gemini-cli': true
    },
    channelModelPrefix: {
      antigravity: '',
      'gemini-cli': ''
    },
    moduleOrder: ['zeabur', 'dns', 'openai', 'server', 'antigravity', 'gemini-cli'],
    load_balancing_strategy: 'random'
  };
}

/**
 * 更新部分设置
 */
function updateUserSettings(partialSettings) {
  const currentSettings = loadUserSettings();
  const updatedSettings = {
    ...currentSettings,
    ...partialSettings
  };
  return saveUserSettings(updatedSettings);
}

module.exports = {
  loadUserSettings,
  saveUserSettings,
  updateUserSettings,
  getDefaultSettings
};
