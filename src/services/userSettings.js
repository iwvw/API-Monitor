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

    // 转换字段名以保持向后兼容
    return {
      customCss: settings.custom_css || '',
      zeaburRefreshInterval: settings.zeabur_refresh_interval || 30000,
      moduleVisibility: settings.module_visibility || {
        zeabur: true,
        dns: true,
        openai: true,
        server: true,
        antigravity: true
      },
      moduleOrder: settings.module_order || ['zeabur', 'dns', 'openai', 'server', 'antigravity']
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
      module_order: settings.moduleOrder || settings.module_order
    };

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
      antigravity: true
    },
    moduleOrder: ['zeabur', 'dns', 'openai', 'server', 'antigravity']
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
