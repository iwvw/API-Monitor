/**
 * 用户设置管理服务（使用 SQLite 数据库）
 * 负责保存和加载用户的个性化设置（自定义CSS、模块配置等）
 */

const { UserSettings } = require('../db/models');
const dbService = require('../db/database');
const settingsRegistry = require('./settings-registry');

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
      'gemini-cli': true,
      qwen: true,
    };

    const channelEnabled = settings.channel_enabled || {
      'gemini-cli': true,
    };

    const channelModelPrefix = settings.channel_model_prefix || {
      'gemini-cli': '',
    };

    const order = settings.module_order || [
      'openai',
      'gemini-cli',
      'qwen',
      'paas',
      'dns',
      'self-h',
      'server',
    ];

    delete visibility.antigravity;
    delete channelEnabled.antigravity;
    delete channelModelPrefix.antigravity;
    const filteredOrder = order.filter(module => module !== 'antigravity');

    if (!('gemini-cli' in visibility)) {
      visibility['gemini-cli'] = true;
    }
    if (!('self-h' in visibility)) {
      visibility['self-h'] = false;
    }
    if (!('qwen' in visibility)) {
      visibility['qwen'] = true;
    }

    if (!('qwen' in channelEnabled)) {
      channelEnabled['qwen'] = true;
    }
    if (!('qwen' in channelModelPrefix)) {
      channelModelPrefix['qwen'] = '';
    }

    if (!filteredOrder.includes('qwen')) {
      const gcliIdx = filteredOrder.indexOf('gemini-cli');
      if (gcliIdx !== -1) {
        filteredOrder.splice(gcliIdx + 1, 0, 'qwen');
      } else {
        filteredOrder.push('qwen');
      }
    }
    if (!filteredOrder.includes('gemini-cli')) {
      filteredOrder.push('gemini-cli');
    }
    if (!filteredOrder.includes('self-h')) {
      // 插入到 server 之前，如果存在的话
      const serverIdx = filteredOrder.indexOf('server');
      if (serverIdx !== -1) {
        filteredOrder.splice(serverIdx, 0, 'self-h');
      } else {
        filteredOrder.push('self-h');
      }
    }

    return {
      customCss: settings.custom_css || '',
      themeMode: settings.theme_mode || undefined,
      pageWidthMode: settings.page_width_mode || undefined,
      koyebRefreshInterval: settings.koyeb_refresh_interval || 30000,
      flyRefreshInterval: settings.fly_refresh_interval || 30000,
      moduleVisibility: visibility,
      channelEnabled: channelEnabled,
      channelModelPrefix: channelModelPrefix,
      moduleOrder: filteredOrder,
      load_balancing_strategy: settings.load_balancing_strategy || 'random',
      serverIpDisplayMode: settings.server_ip_display_mode || 'normal',
      vibrationEnabled:
        settings.vibration_enabled !== undefined ? Boolean(settings.vibration_enabled) : true,
      navLayout: settings.main_tabs_layout || 'top',
      totpSettings: settings.totp_settings || {},
      agentDownloadUrl: settings.agent_download_url || '',
      publicApiUrl: settings.public_api_url || '',
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
    console.log('[UserSettings] 收到保存请求:', JSON.stringify(settings, null, 2));

    // 转换字段名，并确保即便字段不存在也能保留默认结构
    // 注意：必须检查 undefined，因为空字符串 "" 是有效值，不能用 || 运算符覆盖
    const dbSettings = {
      custom_css: settings.customCss !== undefined ? settings.customCss : settings.custom_css,
      theme_mode: settings.themeMode !== undefined ? settings.themeMode : settings.theme_mode,
      page_width_mode:
        settings.pageWidthMode !== undefined ? settings.pageWidthMode : settings.page_width_mode,
      koyeb_refresh_interval:
        settings.koyebRefreshInterval !== undefined
          ? settings.koyebRefreshInterval
          : settings.koyeb_refresh_interval,
      fly_refresh_interval:
        settings.flyRefreshInterval !== undefined
          ? settings.flyRefreshInterval
          : settings.fly_refresh_interval,
      module_visibility:
        settings.moduleVisibility !== undefined
          ? settings.moduleVisibility
          : settings.module_visibility,
      channel_enabled:
        settings.channelEnabled !== undefined ? settings.channelEnabled : settings.channel_enabled,
      channel_model_prefix:
        settings.channelModelPrefix !== undefined
          ? settings.channelModelPrefix
          : settings.channel_model_prefix,
      module_order:
        settings.moduleOrder !== undefined ? settings.moduleOrder : settings.module_order,
      load_balancing_strategy:
        settings.load_balancing_strategy || settings.load_balancing_strategy_form,
      server_ip_display_mode:
        settings.serverIpDisplayMode !== undefined
          ? settings.serverIpDisplayMode
          : settings.server_ip_display_mode,
      vibration_enabled:
        settings.vibrationEnabled !== undefined
          ? settings.vibrationEnabled
            ? 1
            : 0
          : settings.vibration_enabled,
      main_tabs_layout: settings.navLayout || settings.mainTabsLayout || settings.main_tabs_layout,
      totp_settings:
        settings.totpSettings !== undefined ? settings.totpSettings : settings.totp_settings,
      agent_download_url:
        settings.agentDownloadUrl !== undefined
          ? settings.agentDownloadUrl
          : settings.agent_download_url,
      public_api_url:
        settings.publicApiUrl !== undefined ? settings.publicApiUrl : settings.public_api_url,
    };

    console.log('[UserSettings] 转换后数据:', JSON.stringify(dbSettings, null, 2));

    // 直接交给 Model 层处理，Model 层会自动处理 JSON 序列化和数据库列更新
    const result = UserSettings.updateSettings(dbSettings);
    console.log('[UserSettings] 保存结果:', result);

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
    themeMode: 'auto',
    pageWidthMode: 'standard',
    moduleVisibility: {
      paas: true,
      dns: true,
      openai: true,
      server: true,
      'self-h': false,
      'gemini-cli': true,
    },
    channelEnabled: {
      'gemini-cli': true,
    },
    channelModelPrefix: {
      'gemini-cli': '',
    },
    moduleOrder: ['openai', 'gemini-cli', 'qwen', 'paas', 'dns', 'self-h', 'server'],
    load_balancing_strategy: 'random',
    serverIpDisplayMode: 'normal',
    navLayout: 'top',
    totpSettings: {
      hideCode: false,
      allowRevealCode: true,
      groupByPlatform: true,
      showPlatformHeaders: true,
      hidePlatformText: false,
      maskAccount: false,
      autoSave: true,
      lockInputMode: false,
      defaultInputMode: 'code',
    },
  };
}

/**
 * 更新部分设置
 */
function updateUserSettings(partialSettings) {
  const currentSettings = loadUserSettings();
  const updatedSettings = {
    ...currentSettings,
    ...partialSettings,
  };
  return saveUserSettings(updatedSettings);
}

settingsRegistry.register('system', {
  defaults: getDefaultSettings(),
  maskFields: ['publicApiUrl', 'agentDownloadUrl'],
});

settingsRegistry.register('totp', {
  defaults: getDefaultSettings().totpSettings,
  maskFields: [],
});

module.exports = {
  loadUserSettings,
  saveUserSettings,
  updateUserSettings,
  getDefaultSettings,
};
