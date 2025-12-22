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
      paas: true,
      dns: true,
      openai: true,
      server: true,
      'self-h': false,
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

    const order = settings.module_order || ['openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'self-h', 'server'];

    // 确保 gemini-cli 和 self-h 在现有设置中
    if (!('gemini-cli' in visibility)) {
      visibility['gemini-cli'] = true;
    }
    if (!('self-h' in visibility)) {
      visibility['self-h'] = false;
    }
    
    if (!order.includes('gemini-cli')) {
      order.push('gemini-cli');
    }
    if (!order.includes('self-h')) {
      // 插入到 server 之前，如果存在的话
      const serverIdx = order.indexOf('server');
      if (serverIdx !== -1) {
        order.splice(serverIdx, 0, 'self-h');
      } else {
        order.push('self-h');
      }
    }

    return {
      customCss: settings.custom_css || '',
      zeaburRefreshInterval: settings.zeabur_refresh_interval || 30000,
      moduleVisibility: visibility,
      channelEnabled: channelEnabled,
      channelModelPrefix: channelModelPrefix,
      moduleOrder: order,
      load_balancing_strategy: settings.load_balancing_strategy || 'random',
      serverIpDisplayMode: settings.server_ip_display_mode || 'normal',
      navLayout: settings.main_tabs_layout || 'top'
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
      load_balancing_strategy: settings.load_balancing_strategy || settings.load_balancing_strategy_form || 'random',
      server_ip_display_mode: settings.serverIpDisplayMode || settings.server_ip_display_mode || 'normal',
      main_tabs_layout: settings.navLayout || settings.mainTabsLayout || settings.main_tabs_layout || 'top'
    };

    // 确保 channel_model_prefix 是字符串，如果不是则进行 JSON.stringify
    if (dbSettings.channel_model_prefix && typeof dbSettings.channel_model_prefix !== 'string') {
      const originalPrefix = dbSettings.channel_model_prefix;
      dbSettings.channel_model_prefix = JSON.stringify(dbSettings.channel_model_prefix);

    }


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
      paas: true,
      dns: true,
      openai: true,
      server: true,
      'self-h': false,
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
    moduleOrder: ['openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'self-h', 'server'],
    load_balancing_strategy: 'random',
    serverIpDisplayMode: 'normal',
    navLayout: 'top'
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
