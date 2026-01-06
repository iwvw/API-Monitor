/**
 * DNS 管理模块
 * 负责 DNS 管理相关功能
 */

import { store } from '../store.js';
import { toast } from './toast.js';

let monacoEditorInstance = null;

/**
 * 动态加载 Monaco Editor
 */
async function loadMonaco() {
  if (window.monaco) return;

  return new Promise((resolve, reject) => {
    const loaderScript = document.createElement('script');
    loaderScript.src =
      'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs/loader.min.js';
    loaderScript.onload = () => {
      window.require.config({
        paths: { vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs' },
      });
      window.require(['vs/editor/editor.main'], () => {
        resolve();
      });
    };
    loaderScript.onerror = reject;
    document.body.appendChild(loaderScript);
  });
}

// 缓存 key 常量（定义在模块级别，避免 Vue 警告）
const DNS_ACCOUNTS_CACHE_KEY = 'dns_accounts_cache';

export const cloudflareMethods = {
  // 从本地缓存加载账号数据（立即显示）
  loadFromDnsAccountsCache() {
    try {
      const cached = localStorage.getItem(DNS_ACCOUNTS_CACHE_KEY);
      if (cached) {
        const data = JSON.parse(cached);
        if (data && Array.isArray(data.accounts)) {
          store.dnsAccounts = data.accounts.map(acc => ({
            ...acc,
            showToken: false,
            apiToken: null,
          }));
          // 如果没有选中账号且有账号列表，自动选择第一个
          if (!store.dnsSelectedAccountId && store.dnsAccounts.length > 0) {
            store.dnsSelectedAccountId = store.dnsAccounts[0].id;
            // 触发 zones 加载
            this.loadDnsZones();
          }
          return true;
        }
      }
    } catch (e) {
      console.warn('加载 DNS 账号缓存失败:', e);
    }
    return false;
  },

  // 保存账号数据到本地缓存
  saveToDnsAccountsCache(accounts) {
    try {
      localStorage.setItem(
        DNS_ACCOUNTS_CACHE_KEY,
        JSON.stringify({
          accounts,
          timestamp: Date.now(),
        })
      );
    } catch (e) {
      console.warn('保存 DNS 账号缓存失败:', e);
    }
  },

  switchToDns() {
    store.mainActiveTab = 'dns';
    if (store.dnsAccounts.length === 0) {
      // 优先加载缓存
      this.loadFromDnsAccountsCache();
      // 后台刷新最新数据
      this.loadDnsAccounts(true);
    }
    this.loadDnsTemplates();
  },

  /**
   * 打开外部链接
   */
  openExternalLink(url) {
    if (!url) return;

    let targetUrl = url;
    // 如果没有 http:// 或 https:// 开头，且看起来像域名
    if (!/^https?:\/\//i.test(targetUrl)) {
      targetUrl = 'https://' + targetUrl;
    }

    window.open(targetUrl, '_blank');
  },

  /**
   * 加载 Dns 账号
   */
  async loadDnsAccounts(silent = false) {
    try {
      const response = await fetch('/api/cloudflare/accounts', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      // API 直接返回账号数组
      if (Array.isArray(data)) {
        // 为每个账号添加 showToken 属性
        store.dnsAccounts = data.map(acc => ({
          ...acc,
          showToken: false,
          apiToken: null,
        }));

        // 保存到本地缓存
        this.saveToDnsAccountsCache(data);

        // 如果没有选中账号且有账号列表，自动选择第一个
        if (!store.dnsSelectedAccountId && store.dnsAccounts.length > 0) {
          this.selectDnsAccount(store.dnsAccounts[0]);
        }
      } else if (data.error) {
        console.error('加载 CF 账号失败:', data.error);
      }
    } catch (error) {
      console.error('加载 CF 账号失败:', error);
    }
  },

  async toggleDnsTokenVisibility(account) {
    try {
      if (account.showToken) {
        // 隐藏 token
        account.showToken = false;
        account.apiToken = null;
      } else {
        // 显示 token - 从主机获取
        const response = await fetch(`/api/cloudflare/accounts/${account.id}/token`, {
          headers: store.getAuthHeaders(),
        });
        const data = await response.json();
        if (data.success && data.apiToken) {
          account.apiToken = data.apiToken;
          account.showToken = true;
        } else {
          this.showDnsToast('获取 Token 失败', 'error');
        }
      }
    } catch (error) {
      this.showDnsToast('操作失败: ' + error.message, 'error');
    }
  },

  selectDnsAccount(acc) {
    store.dnsSelectedAccountId = acc.id;
    this.loadDnsZones();
  },

  openAddDnsAccountModal() {
    this.dnsAccountForm = { name: '', apiToken: '', email: '' };
    this.dnsAccountFormError = '';
    this.dnsAccountFormSuccess = '';
    this.showAddDnsAccountModal = true;
  },

  editDnsAccount(account) {
    this.dnsEditingAccount = account;
    this.dnsEditAccountForm = {
      name: account.name,
      apiToken: '', // 不显示原 Token
      email: account.email || '',
    };
    this.dnsEditAccountFormError = '';
    this.dnsEditAccountFormSuccess = '';
    this.showEditDnsAccountModal = true;
  },

  async deleteDnsAccount(account) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除账号 "${account.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/cloudflare/accounts/${account.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      // API 返回 { success: true } 或 { error: '...' }
      if (response.ok && data.success) {
        this.showDnsToast('账号已删除', 'success');
        await this.loadDnsAccounts();
        if (this.dnsSelectedAccountId === account.id) {
          this.dnsSelectedAccountId = '';
          this.dnsZones = [];
          this.dnsRecords = [];
        }
      } else {
        this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showDnsToast('删除失败: ' + error.message, 'error');
    }
  },

  async loadDnsZones() {
    if (!store.dnsSelectedAccountId) {
      store.dnsZones = [];
      return;
    }

    const cacheKey = `dns_zones_${store.dnsSelectedAccountId}`;

    // 优先从缓存加载，实现即时显示
    try {
      const cached = localStorage.getItem(cacheKey);
      if (cached) {
        const cachedData = JSON.parse(cached);
        if (cachedData.zones && cachedData.zones.length > 0) {
          store.dnsZones = cachedData.zones;
          // 不自动选中域名，用户需要手动选择
        }
      }
    } catch (e) {
      console.warn('[DNS] 缓存读取失败:', e);
    }

    // 后台静默刷新
    store.dnsLoadingZones = true;

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones`, {
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      // API 返回 { zones: [...], pagination: {...} } 或 { error: '...' }
      if (data.zones) {
        store.dnsZones = data.zones;
        // 保存到缓存
        try {
          localStorage.setItem(
            cacheKey,
            JSON.stringify({ zones: data.zones, timestamp: Date.now() })
          );
        } catch (e) {
          console.warn('[DNS] 缓存保存失败:', e);
        }
      } else if (data.error) {
        console.error('加载域名失败:', data.error);
      }
    } catch (error) {
      console.error('加载域名失败:', error);
    } finally {
      store.dnsLoadingZones = false;
    }
  },

  selectDnsZone(zone) {
    store.dnsSelectedZoneId = zone.id;
    store.dnsSelectedZoneName = zone.name;
    // 兼容两种命名方式: nameServers (驼峰) 和 name_servers (下划线)
    store.dnsSelectedZoneNameServers = zone.nameServers || zone.name_servers || [];
    console.log(
      '[DNS] 已选择域名:',
      zone.name,
      '| NS记录数量:',
      store.dnsSelectedZoneNameServers.length,
      '| NS数据:',
      store.dnsSelectedZoneNameServers
    );
    // 保存选中的 zone 到缓存以便下次恢复
    if (store.dnsSelectedAccountId) {
      try {
        localStorage.setItem(`dns_last_zone_${store.dnsSelectedAccountId}`, zone.id);
      } catch (e) {
        // 忽略存储错误
      }
    }
    this.loadDnsRecords();
    this.loadZoneSsl(); // 加载SSL信息
    // this.loadZoneAnalytics(); // 加载Analytics数据 - 暂时禁用
  },

  toggleNsPopover() {
    store.showNsPopover = !store.showNsPopover;
  },

  /**
   * 清除Zone的所有缓存
   */
  async purgeZoneCache() {
    if (!store.dnsSelectedAccountId || !store.dnsSelectedZoneId) {
      toast.error('请先选择域名');
      return;
    }

    const confirmed = await store.showConfirm({
      title: '确认清除缓存',
      message: `确定要清除域名 "${store.dnsSelectedZoneName}" 的所有缓存吗？此操作将清空CDN缓存，可能会短暂影响性能。`,
      icon: 'fa-broom',
      confirmText: '清除缓存',
      confirmClass: 'btn-warning',
    });

    if (!confirmed) return;

    store.dnsPurgingCache = true;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/purge`,
        {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({ purge_everything: true }),
        }
      );

      const data = await response.json();

      if (response.ok && data.success) {
        toast.success(`✅ 缓存已清除！域名: ${store.dnsSelectedZoneName}`);
      } else {
        toast.error('清除缓存失败: ' + (data.error || data.message || '未知错误'));
      }
    } catch (error) {
      toast.error('清除缓存失败: ' + error.message);
    } finally {
      store.dnsPurgingCache = false;
    }
  },

  /**
   * 加载域名的SSL/TLS信息
   */
  async loadZoneSsl() {
    if (!store.dnsSelectedAccountId || !store.dnsSelectedZoneId) return;

    store.dnsLoadingSsl = true;
    store.dnsSelectedZoneSsl = null;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/ssl`,
        { headers: store.getAuthHeaders() }
      );

      const data = await response.json();
      if (data.success) {
        store.dnsSelectedZoneSsl = data.ssl;
        console.log('[DNS] SSL信息已加载:', data.ssl);
      } else {
        toast.error('加载SSL信息失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      console.error('[DNS] 加载SSL信息失败:', error);
      toast.error('加载SSL信息失败: ' + error.message);
    } finally {
      store.dnsLoadingSsl = false;
    }
  },

  /**
   * 更新SSL模式
   */
  async updateSslMode(mode) {
    if (!store.dnsSelectedAccountId || !store.dnsSelectedZoneId) return;

    const modeNames = {
      off: 'Off (关闭)',
      flexible: 'Flexible (灵活)',
      full: 'Full (完全)',
      strict: 'Full (strict)',
    };

    const confirmed = await store.showConfirm({
      title: '确认修改SSL模式',
      message: `确定要将SSL模式修改为 "${modeNames[mode]}" 吗？\n\n修改后可能需要几分钟生效。`,
      icon: 'fa-lock',
      confirmText: '确认修改',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    store.dnsLoadingSsl = true;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/ssl`,
        {
          method: 'PATCH',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({ mode }),
        }
      );

      const data = await response.json();
      if (data.success) {
        store.dnsSelectedZoneSsl.mode = data.ssl.mode;
        toast.success(`✅ SSL模式已更新为: ${modeNames[mode]}`);
      } else {
        toast.error('更新SSL模式失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      console.error('[DNS] 更新SSL模式失败:', error);
      toast.error('更新SSL模式失败: ' + error.message);
    } finally {
      store.dnsLoadingSsl = false;
    }
  },

  /**
   * 加载Analytics数据
   */
  async loadZoneAnalytics(timeRange = '24h') {
    if (!store.dnsSelectedAccountId || !store.dnsSelectedZoneId) return;

    store.dnsLoadingAnalytics = true;
    store.dnsAnalyticsTimeRange = timeRange;

    try {
      console.log('[DNS] 开始加载Analytics:', {
        accountId: store.dnsSelectedAccountId,
        zoneId: store.dnsSelectedZoneId,
        timeRange,
      });

      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/analytics?timeRange=${timeRange}`,
        { headers: store.getAuthHeaders() }
      );

      console.log('[DNS] Analytics响应状态:', response.status);

      const data = await response.json();

      console.log('[DNS] Analytics返回数据:', data);

      if (data.success) {
        store.dnsSelectedZoneAnalytics = data.analytics;
        console.log('[DNS] Analytics已加载:', data.analytics);

        // 如果数据全是0，给出提示
        if (data.analytics.requests === 0 && data.analytics.bandwidth === 0) {
          console.warn('[DNS] Analytics数据为0，可能原因：');
          console.warn('1. 域名在此时间段内没有流量');
          console.warn('2. Analytics数据还未生成（新域名）');
          console.warn('3. API权限问题');
        }
      } else {
        console.error('[DNS] Analytics加载失败:', data.error);
        toast.error('加载Analytics失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      console.error('[DNS] Analytics请求异常:', error);
      console.error('[DNS] 错误详情:', error.message, error.stack);
      // 静默失败，不影响其他功能
      store.dnsSelectedZoneAnalytics = null;
    } finally {
      store.dnsLoadingAnalytics = false;
    }
  },

  /**
   * 格式化数字（简化显示）
   */
  formatNumber(num) {
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
    return num.toString();
  },

  /**
   * 格式化字节
   */
  formatBytes(bytes) {
    if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(2) + ' GB';
    if (bytes >= 1048576) return (bytes / 1048576).toFixed(2) + ' MB';
    if (bytes >= 1024) return (bytes / 1024).toFixed(2) + ' KB';
    return bytes + ' B';
  },

  async loadDnsRecords() {
    if (!store.dnsSelectedAccountId || !store.dnsSelectedZoneId) return;

    store.dnsLoadingRecords = true;
    store.dnsRecords = [];

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records`,
        { headers: store.getAuthHeaders() }
      );

      const data = await response.json();
      // API 返回 { records: [...], pagination: {...} } 或 { error: '...' }
      if (data.records) {
        store.dnsRecords = data.records;
        toast.success('解析记录已更新');
      } else if (data.error) {
        this.showDnsToast('加载记录失败: ' + data.error, 'error');
      }
    } catch (error) {
      this.showDnsToast('加载记录失败: ' + error.message, 'error');
    } finally {
      store.dnsLoadingRecords = false;
    }
  },

  openAddDnsRecordModal() {
    this.dnsEditingRecord = null;
    this.dnsRecordForm = { type: 'A', name: '', content: '', ttl: 1, proxied: false, priority: 10 };
    this.dnsRecordFormError = '';
    this.showDnsRecordModal = true;
  },

  editDnsRecord(record) {
    this.dnsEditingRecord = record;
    this.dnsRecordForm = {
      type: record.type,
      name: this.formatDnsRecordName(record.name),
      content: record.content,
      ttl: record.ttl,
      proxied: record.proxied || false,
      priority: record.priority || 10,
    };
    this.dnsRecordFormError = '';
    this.showDnsRecordModal = true;
  },

  async saveDnsRecord() {
    if (!this.dnsRecordForm.name || !this.dnsRecordForm.content) {
      this.dnsRecordFormError = '请填写名称和内容';
      return;
    }

    this.dnsSavingRecord = true;
    this.dnsRecordFormError = '';

    try {
      const url = this.dnsEditingRecord
        ? `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records/${this.dnsEditingRecord.id}`
        : `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records`;

      const response = await fetch(url, {
        method: this.dnsEditingRecord ? 'PUT' : 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.dnsRecordForm),
      });

      const data = await response.json();
      // API 返回 { success: true, record: {...} } 或 { error: '...' }
      if (response.ok && (data.success || data.record)) {
        this.showDnsToast(this.dnsEditingRecord ? '记录已更新' : '记录已添加', 'success');
        this.showDnsRecordModal = false;
        await this.loadDnsRecords();
      } else {
        this.dnsRecordFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.dnsRecordFormError = '保存失败: ' + error.message;
    } finally {
      this.dnsSavingRecord = false;
    }
  },

  async deleteDnsRecord(record) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除记录 "${record.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records/${record.id}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      const data = await response.json();
      // API 返回 { success: true } 或 { error: '...' }
      if (response.ok && data.success) {
        this.showDnsToast('记录已删除', 'success');
        await this.loadDnsRecords();
      } else {
        this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showDnsToast('删除失败: ' + error.message, 'error');
    }
  },

  formatDnsRecordName(name) {
    if (!name || !this.dnsSelectedZoneName) return name;
    const suffix = '.' + this.dnsSelectedZoneName;
    if (name === this.dnsSelectedZoneName) return '@';
    if (name.endsWith(suffix)) {
      return name.slice(0, -suffix.length);
    }
    return name;
  },

  toggleSelectAllDnsRecords(event) {
    if (event.target.checked) {
      store.dnsSelectedRecords = store.dnsRecords.map(r => r.id);
    } else {
      store.dnsSelectedRecords = [];
    }
  },

  async batchDeleteDnsRecords() {
    if (store.dnsSelectedRecords.length === 0) return;

    const confirmed = await store.showConfirm({
      title: '批量删除确认',
      message: `确定要删除选中的 ${store.dnsSelectedRecords.length} 条 DNS 记录吗？此操作不可恢复！`,
      icon: 'fa-exclamation-triangle',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    const selectedCount = store.dnsSelectedRecords.length;
    let successCount = 0;
    let failedCount = 0;

    // 显示删除进度
    toast.info(`正在删除 ${selectedCount} 条记录...`);

    for (const recordId of store.dnsSelectedRecords) {
      try {
        const response = await fetch(
          `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records/${recordId}`,
          {
            method: 'DELETE',
            headers: store.getAuthHeaders(),
          }
        );

        const data = await response.json();
        if (response.ok && data.success) {
          successCount++;
        } else {
          failedCount++;
        }
      } catch (error) {
        failedCount++;
      }
    }

    // 清空选择
    store.dnsSelectedRecords = [];

    // 刷新记录列表
    await this.loadDnsRecords();

    // 显示结果
    if (failedCount === 0) {
      toast.success(`✅ 成功删除 ${successCount} 条记录`);
    } else {
      toast.error(`⚠️ 删除完成：成功 ${successCount} 条，失败 ${failedCount} 条`);
    }
  },

  async dnsQuickSwitch() {
    if (!this.dnsQuickSwitchName || !this.dnsQuickSwitchContent) {
      toast.error('请填写记录名称 and 新内容');
      return;
    }

    this.dnsSwitching = true;

    try {
      // 使用正确的 API 端点 /switch
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/switch`,
        {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            type: this.dnsQuickSwitchType,
            name: this.dnsQuickSwitchName,
            newContent: this.dnsQuickSwitchContent,
          }),
        }
      );

      const data = await response.json();
      // API 返回 { success: true, updated: n, records: [...] } 或 { error: '...' }
      if (response.ok && data.success) {
        toast.success(`切换成功！更新了 ${data.updated || 0} 条记录`);
        this.dnsQuickSwitchName = '';
        this.dnsQuickSwitchContent = '';
        await this.loadDnsRecords();
      } else {
        toast.error('切换失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('切换失败: ' + error.message);
    } finally {
      this.dnsSwitching = false;
    }
  },

  async loadDnsTemplates() {
    try {
      const response = await fetch('/api/cloudflare/templates', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      // API 直接返回模板数组
      if (Array.isArray(data)) {
        store.dnsTemplates = data;
      } else if (data.error) {
        console.error('加载模板失败:', data.error);
      }
    } catch (error) {
      console.error('加载模板失败:', error);
    }
  },

  openAddDnsTemplateModal() {
    this.dnsEditingTemplate = null;
    this.dnsTemplateForm = {
      name: '',
      type: 'A',
      content: '',
      ttl: 1,
      proxied: false,
      description: '',
    };
    this.dnsTemplateFormError = '';
    this.showDnsTemplateModal = true;
  },

  editDnsTemplate(template) {
    this.dnsEditingTemplate = template;
    this.dnsTemplateForm = { ...template };
    this.dnsTemplateFormError = '';
    this.showDnsTemplateModal = true;
  },

  async saveDnsTemplate() {
    if (!this.dnsTemplateForm.name || !this.dnsTemplateForm.content) {
      this.dnsTemplateFormError = '请填写模板名称和内容';
      return;
    }

    this.dnsSavingTemplate = true;
    this.dnsTemplateFormError = '';

    try {
      const url = this.dnsEditingTemplate
        ? `/api/cloudflare/templates/${this.dnsEditingTemplate.id}`
        : '/api/cloudflare/templates';

      const response = await fetch(url, {
        method: this.dnsEditingTemplate ? 'PUT' : 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.dnsTemplateForm),
      });

      const data = await response.json();
      // API 返回 { success: true, template: {...} } 或 { error: '...' }
      if (response.ok && (data.success || data.template)) {
        this.showDnsToast(this.dnsEditingTemplate ? '模板已更新' : '模板已添加', 'success');
        this.showDnsTemplateModal = false;
        await this.loadDnsTemplates();
      } else {
        this.dnsTemplateFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.dnsTemplateFormError = '保存失败: ' + error.message;
    } finally {
      this.dnsSavingTemplate = false;
    }
  },

  async deleteDnsTemplate(template) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除模板 "${template.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/cloudflare/templates/${template.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      // API 返回 { success: true } 或 { error: '...' }
      if (response.ok && data.success) {
        this.showDnsToast('模板已删除', 'success');
        await this.loadDnsTemplates();
      } else {
        this.showDnsToast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showDnsToast('删除失败: ' + error.message, 'error');
    }
  },

  async addDnsAccount() {
    if (!this.dnsAccountForm.name || !this.dnsAccountForm.apiToken) {
      this.dnsAccountFormError = '请填写账号名称和 API Token';
      return;
    }

    this.dnsSavingAccount = true;
    this.dnsAccountFormError = '';
    this.dnsAccountFormSuccess = '';

    try {
      const response = await fetch('/api/cloudflare/accounts', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.dnsAccountForm),
      });

      const data = await response.json();
      // API 返回 { success: true, account: {...} } 或 { error: '...' }
      if (response.ok && (data.success || data.account)) {
        this.dnsAccountFormSuccess = '账号添加成功！';
        await this.loadDnsAccounts();
        setTimeout(() => {
          this.showAddDnsAccountModal = false;
        }, 1000);
      } else {
        this.dnsAccountFormError = data.error || '添加失败';
      }
    } catch (error) {
      this.dnsAccountFormError = '网络错误: ' + error.message;
    } finally {
      this.dnsSavingAccount = false;
    }
  },

  async verifyDnsAccount(account) {
    try {
      const response = await fetch(`/api/cloudflare/accounts/${account.id}/verify`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      // API 返回 { valid: true/false, ... }
      if (data.valid) {
        this.showDnsToast('Token 验证成功！', 'success');
      } else {
        this.showDnsToast('Token 验证失败: ' + (data.error || '无效的 Token'), 'error');
      }
    } catch (error) {
      this.showDnsToast('验证失败: ' + error.message, 'error');
    }
  },

  async updateDnsAccount() {
    if (!this.dnsEditAccountForm.name) {
      this.dnsEditAccountFormError = '请填写账号名称';
      return;
    }

    this.dnsSavingAccount = true;
    this.dnsEditAccountFormError = '';
    this.dnsEditAccountFormSuccess = '';

    try {
      const updateData = {
        name: this.dnsEditAccountForm.name,
        email: this.dnsEditAccountForm.email,
      };

      // 如果填写了新的 Token，则包含在更新数据中
      if (this.dnsEditAccountForm.apiToken) {
        updateData.apiToken = this.dnsEditAccountForm.apiToken;
      }

      const response = await fetch(`/api/cloudflare/accounts/${this.dnsEditingAccount.id}`, {
        method: 'PUT',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(updateData),
      });

      const data = await response.json();
      if (response.ok && data.success) {
        this.dnsEditAccountFormSuccess = '账号更新成功！';
        setTimeout(() => {
          this.showEditDnsAccountModal = false;
          this.loadDnsAccounts();
        }, 1000);
      } else {
        this.dnsEditAccountFormError = data.error || '更新失败';
      }
    } catch (error) {
      this.dnsEditAccountFormError = '更新失败: ' + error.message;
    } finally {
      this.dnsSavingAccount = false;
    }
  },

  formatDnsDate(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleDateString('zh-CN');
  },

  canDnsBeProxied(type) {
    return ['A', 'AAAA', 'CNAME'].includes(type);
  },

  startEditDnsName(record) {
    // 取消其他正在编辑的记录
    this.dnsRecords.forEach(r => {
      if (r.isEditingName) {
        r.isEditingName = false;
      }
      if (r.isEditingContent) {
        r.isEditingContent = false;
      }
    });

    record.isEditingName = true;
    record.editingName = this.formatDnsRecordName(record.name);

    // 等待 DOM 更新后聚焦输入框
    this.$nextTick(() => {
      const inputs = document.querySelectorAll('.inline-edit-input');
      if (inputs.length > 0) {
        inputs[inputs.length - 1].focus();
        inputs[inputs.length - 1].select();
      }
    });
  },

  cancelEditDnsName(record) {
    record.isEditingName = false;
    record.editingName = '';
  },

  async saveDnsName(record) {
    // 如果不在编辑状态，直接返回
    if (!record.isEditingName) {
      return;
    }

    const originalName = this.formatDnsRecordName(record.name);

    // 如果名称没有变化，取消编辑
    if (record.editingName === originalName) {
      this.cancelEditDnsName(record);
      return;
    }

    // 验证名称不为空
    if (!record.editingName || record.editingName.trim() === '') {
      this.showDnsToast('名称不能为空', 'error');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records/${record.id}`,
        {
          method: 'PUT',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            type: record.type,
            name: record.editingName.trim(),
            content: record.content,
            ttl: record.ttl,
            proxied: record.proxied || false,
          }),
        }
      );

      const data = await response.json();
      if (response.ok && (data.success || data.record)) {
        // 重新加载记录以获取更新后的完整名称
        this.cancelEditDnsName(record);
        this.showDnsToast('记录名称已更新', 'success');
        await this.loadDnsRecords();
      } else {
        this.showDnsToast('保存失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showDnsToast('保存失败: ' + error.message, 'error');
    }
  },

  startEditDnsContent(record) {
    // 取消其他正在编辑的记录
    this.dnsRecords.forEach(r => {
      if (r.isEditingContent) {
        r.isEditingContent = false;
      }
    });

    record.isEditingContent = true;
    record.editingContent = record.content;

    // 等待 DOM 更新后聚焦输入框
    this.$nextTick(() => {
      const inputs = document.querySelectorAll('.inline-edit-input');
      if (inputs.length > 0) {
        inputs[inputs.length - 1].focus();
        inputs[inputs.length - 1].select();
      }
    });
  },

  cancelEditDnsContent(record) {
    record.isEditingContent = false;
    record.editingContent = '';
  },

  async saveDnsContent(record) {
    // 如果不在编辑状态，直接返回
    if (!record.isEditingContent) {
      return;
    }

    // 如果内容没有变化，取消编辑
    if (record.editingContent === record.content) {
      this.cancelEditDnsContent(record);
      return;
    }

    // 验证内容不为空
    if (!record.editingContent || record.editingContent.trim() === '') {
      this.showDnsToast('内容不能为空', 'error');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/records/${record.id}`,
        {
          method: 'PUT',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            type: record.type,
            name: this.formatDnsRecordName(record.name),
            content: record.editingContent.trim(),
            ttl: record.ttl,
            proxied: record.proxied || false,
          }),
        }
      );

      const data = await response.json();
      if (response.ok && (data.success || data.record)) {
        // 更新本地记录
        record.content = record.editingContent.trim();
        this.cancelEditDnsContent(record);
        this.showDnsToast('记录已更新', 'success');
      } else {
        this.showDnsToast('保存失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showDnsToast('保存失败: ' + error.message, 'error');
    }
  },

  // 导出 DNS 记录
  async exportDnsRecords() {
    try {
      if (!store.dnsSelectedZoneId) {
        toast.error('请先选择一个域名');
        return;
      }

      if (store.dnsRecords.length === 0) {
        toast.warning('没有可导出的 DNS 记录');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        zoneName: store.dnsSelectedZoneName,
        zoneId: store.dnsSelectedZoneId,
        records: store.dnsRecords.map(record => ({
          type: record.type,
          name: this.formatDnsRecordName(record.name),
          content: record.content,
          ttl: record.ttl,
          proxied: record.proxied || false,
          priority: record.priority,
        })),
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `dns-${this.dnsSelectedZoneName}-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showDnsToast('DNS 记录导出成功', 'success');
    } catch (error) {
      this.showDnsToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入 DNS 记录
  async importDnsRecords() {
    if (!this.dnsSelectedZoneId) {
      this.showDnsToast('请先选择一个域名', 'error');
      return;
    }

    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入 DNS 记录将添加到当前域名中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);

          // 验证数据格式
          if (!importedData.version || !importedData.records) {
            this.showDnsToast('无效的备份文件格式', 'error');
            return;
          }

          // 询问是否确认导入
          const confirm2 = await this.showConfirm({
            title: '确认导入',
            message: `将导入 ${importedData.records.length} 条 DNS 记录到域名 ${this.dnsSelectedZoneName}，确定继续吗？`,
            icon: 'fa-info-circle',
            confirmText: '开始导入',
            confirmClass: 'btn-primary',
          });

          if (!confirm2) return;

          // 导入记录
          let successCount = 0;
          let failedCount = 0;

          this.showDnsToast(`正在导入 ${importedData.records.length} 条记录...`, 'success');

          for (const record of importedData.records) {
            try {
              const response = await fetch(
                `/api/cloudflare/accounts/${this.dnsSelectedAccountId}/zones/${this.dnsSelectedZoneId}/records`,
                {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(record),
                }
              );

              const data = await response.json();
              if (response.ok && (data.success || data.record)) {
                successCount++;
              } else {
                failedCount++;
              }
            } catch (error) {
              failedCount++;
            }
          }

          // 刷新记录列表
          await this.loadDnsRecords();

          // 显示结果
          if (failedCount === 0) {
            this.showDnsToast(`✅ 成功导入 ${successCount} 条记录`, 'success');
          } else {
            this.showDnsToast(
              `⚠️ 导入完成：成功 ${successCount} 条，失败 ${failedCount} 条`,
              'error'
            );
          }
        } catch (error) {
          this.showDnsToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // 导出 DNS 账号
  async exportDnsAccounts() {
    try {
      if (this.dnsAccounts.length === 0) {
        this.showDnsToast('没有可导出的账号', 'warning');
        return;
      }

      // 从主机获取包含 API Token 的完整账号数据
      const response = await fetch('/api/cloudflare/accounts/export', {
        headers: this.getAuthHeaders(),
      });

      const data = await response.json();
      if (!response.ok || !data.success) {
        this.showDnsToast('导出失败: ' + (data.error || '未知错误'), 'error');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        accounts: data.accounts,
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `dns-accounts-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showDnsToast('DNS 账号导出成功', 'success');
    } catch (error) {
      this.showDnsToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入 DNS 账号
  async importDnsAccounts() {
    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入 DNS 账号将添加到现有账号列表中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);

          // 验证数据格式
          if (!importedData.version || !importedData.accounts) {
            this.showDnsToast('无效的备份文件格式', 'error');
            return;
          }

          // 询问是否确认导入
          const confirm2 = await this.showConfirm({
            title: '确认导入',
            message: `将导入 ${importedData.accounts.length} 个 DNS 账号，确定继续吗？`,
            icon: 'fa-info-circle',
            confirmText: '开始导入',
            confirmClass: 'btn-primary',
          });

          if (!confirm2) return;

          // 导入账号
          let successCount = 0;
          let failedCount = 0;
          let skippedCount = 0;

          this.showDnsToast(`正在导入 ${importedData.accounts.length} 个账号...`, 'success');

          for (const account of importedData.accounts) {
            try {
              // 检查账号是否已存在
              const exists = this.dnsAccounts.some(acc => acc.name === account.name);
              if (exists) {
                skippedCount++;
                continue;
              }

              const response = await fetch('/api/cloudflare/accounts', {
                method: 'POST',
                headers: this.getAuthHeaders(),
                body: JSON.stringify(account),
              });

              const data = await response.json();
              if (response.ok && (data.success || data.account)) {
                successCount++;
              } else {
                failedCount++;
              }
            } catch (error) {
              failedCount++;
            }
          }

          // 刷新账号列表
          await this.loadDnsAccounts();

          // 显示结果
          let message = `✅ 成功导入 ${successCount} 个账号`;
          if (skippedCount > 0) {
            message += `，跳过 ${skippedCount} 个重复账号`;
          }
          if (failedCount > 0) {
            message = `⚠️ 导入完成：成功 ${successCount} 个，跳过 ${skippedCount} 个，失败 ${failedCount} 个`;
          }
          this.showDnsToast(message, failedCount > 0 ? 'error' : 'success');
        } catch (error) {
          this.showDnsToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // ==================== Workers 管理 ====================

  /**
   * 加载 Workers 列表
   */
  async loadWorkers() {
    if (!store.dnsSelectedAccountId) {
      toast.warning('请先选择一个账号');
      return;
    }

    // 只在无缓存数据时显示 loading，刷新时静默加载
    const isFirstLoad = this.workers.length === 0;
    if (isFirstLoad) {
      this.workersLoading = true;
    }

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      if (response.ok) {
        this.workers = data.workers || [];
        this.workersSubdomain = data.subdomain || null;
        this.workersCfAccountId = data.cfAccountId || null; // 保存 CF 账号 ID
      } else {
        toast.error(data.error || '加载 Workers 失败');
      }
    } catch (error) {
      toast.error('加载 Workers 失败: ' + error.message);
    } finally {
      this.workersLoading = false;
    }
  },

  /**
   * 格式化 Worker 日期
   */
  formatWorkerDate(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  },

  async deleteWorker(scriptName) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 Worker "${scriptName}" 吗？此操作不可恢复。`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(scriptName)}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );
      const data = await response.json();

      if (response.ok) {
        toast.success('Worker 已删除');
        if (this.selectedWorker?.name === scriptName) {
          this.selectedWorker = null;
          this.workerEditorContent = '';
        }
        await this.loadWorkers();
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  /**
   * 开始编辑 Worker 名称
   */
  startEditWorkerName(worker) {
    worker.editingName = worker.name;
    worker.isEditing = true;
    this.$nextTick(() => {
      const inputs = this.$refs.workerNameInput;
      if (inputs) {
        const input = Array.isArray(inputs) ? inputs.find(el => el) : inputs;
        if (input) {
          input.focus();
          input.select();
        }
      }
    });
  },

  /**
   * 保存 Worker 名称
   */
  async saveWorkerName(worker) {
    const newName = worker.editingName?.trim();
    if (!newName || newName === worker.name) {
      worker.isEditing = false;
      return;
    }

    // 验证命名规则
    if (!/^[a-z0-9-]+$/.test(newName)) {
      toast.error('Worker 名称只能包含小写字母、数字和连字符');
      worker.editingName = worker.name;
      worker.isEditing = false;
      return;
    }

    // 检查是否与其他 Worker 重名
    const hasDuplicate = this.workers.some(w => w.id !== worker.id && w.name === newName);
    if (hasDuplicate) {
      toast.error(`Worker 名称 "${newName}" 已存在,请使用其他名称`);
      worker.editingName = worker.name;
      worker.isEditing = false;
      return;
    }

    toast.warning('Cloudflare Workers 不支持重命名,请手动创建新 Worker 并删除旧 Worker');
    worker.editingName = worker.name;
    worker.isEditing = false;
  },

  /**
   * 取消编辑 Worker 名称
   */
  cancelEditWorkerName(worker) {
    worker.editingName = worker.name;
    worker.isEditing = false;
  },
  /**
   * 打开新建 Worker 模态框
   */
  async openNewWorkerModal() {
    store.isEditingWorker = false;
    this.newWorkerName = '';
    const defaultScript = `// 新建 Worker
export default {
  async fetch(request, env, ctx) {
    return new Response('Hello World!');
  },
};`;
    this.newWorkerScript = defaultScript;
    this.showNewWorkerModal = true;

    // 初始化 Monaco Editor
    this.$nextTick(async () => {
      try {
        await loadMonaco();
        const container = document.getElementById('monaco-editor-container');
        if (container) {
          if (monacoEditorInstance) {
            monacoEditorInstance.dispose();
          }
          monacoEditorInstance = monaco.editor.create(container, {
            value: defaultScript,
            language: 'javascript',
            theme: 'vs-dark',
            automaticLayout: true,
            minimap: { enabled: true },
            fontSize: 13,
            scrollBeyondLastLine: false,
          });
        }
      } catch (e) {
        console.error('Monaco Editor 加载失败:', e);
        toast.error('编辑器加载失败，请检查网络');
      }
    });
  },

  /**
   * 打开编辑 Worker 模态框
   */
  async openEditWorkerModal(worker) {
    store.isEditingWorker = true;
    this.newWorkerName = worker.name;
    this.showNewWorkerModal = true;
    this.workersLoading = true;

    try {
      // 获取脚本内容
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(worker.name)}`,
        {
          headers: store.getAuthHeaders(),
        }
      );
      const data = await response.json();

      if (response.ok && data.success) {
        const script = data.worker.script;

        // 初始化/更新 Monaco Editor (带重试机制)
        this.$nextTick(async () => {
          try {
            await loadMonaco();

            let attempts = 0;
            const initEditor = () => {
              const container = document.getElementById('monaco-editor-container');
              if (container) {
                // 清理旧实例
                if (monacoEditorInstance) {
                  try {
                    monacoEditorInstance.dispose();
                  } catch (e) {
                    console.warn('Dispose error:', e);
                  }
                  monacoEditorInstance = null;
                }

                // 确保容器为空
                container.innerHTML = '';

                try {
                  monacoEditorInstance = monaco.editor.create(container, {
                    value: script,
                    language: 'javascript',
                    theme: 'vs-dark',
                    automaticLayout: true,
                    minimap: { enabled: true },
                    fontSize: 13,
                    scrollBeyondLastLine: false,
                    readOnly: false,
                  });
                } catch (createError) {
                  console.error('Create editor error:', createError);
                  toast.error('创建编辑器实例失败');
                }
              } else if (attempts < 20) {
                // 如果容器未找到，等待 50ms 后重试（最多 1秒）
                attempts++;
                setTimeout(initEditor, 50);
              } else {
                console.error('Monaco container not found after 20 attempts');
                toast.error('编辑器容器加载超时');
              }
            };

            initEditor();
          } catch (e) {
            console.error('Monaco Editor 加载失败:', e);
            toast.error('编辑器资源加载失败');
          }
        });
      } else {
        toast.error(data.error || '获取脚本内容失败');
        this.showNewWorkerModal = false;
      }
    } catch (error) {
      toast.error('请求脚本失败: ' + error.message);
      this.showNewWorkerModal = false;
    } finally {
      this.workersLoading = false;
    }
  },

  /**
   * 保存新 Worker
   */
  async saveNewWorker() {
    const name = this.newWorkerName?.trim();
    // 从编辑器获取内容
    const script = monacoEditorInstance ? monacoEditorInstance.getValue() : this.newWorkerScript;

    if (!name) {
      toast.error('请输入 Worker 名称');
      return;
    }
    if (!script) {
      toast.error('请输入脚本内容');
      return;
    }

    // 验证名称格式
    if (!/^[a-z0-9-]+$/.test(name)) {
      toast.error('Worker 名称只能包含小写字母、数字和连字符');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(name)}`,
        {
          method: 'PUT',
          headers: {
            ...store.getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ script }),
        }
      );
      const data = await response.json();

      if (response.ok) {
        toast.success('Worker 已创建，正在打开 Cloudflare 编辑器...');
        this.showNewWorkerModal = false;
        await this.loadWorkers();
        // 自动打开 Cloudflare 编辑器
        if (this.workersCfAccountId) {
          this.openExternalLink(
            `https://dash.cloudflare.com/${this.workersCfAccountId}/workers/services/edit/${name}/production`
          );
        }
      } else {
        toast.error(data.error || '创建失败');
      }
    } catch (error) {
      toast.error('创建失败: ' + error.message);
    }
  },

  // ==================== Pages 管理 ====================

  /**
   * 加载 Pages 项目列表
   */
  async loadPages() {
    if (!store.dnsSelectedAccountId) {
      return;
    }

    // 只在无缓存数据时显示 loading，刷新时静默加载
    const isFirstLoad = this.pagesProjects.length === 0;
    if (isFirstLoad) {
      this.pagesLoading = true;
    }

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      if (response.ok) {
        this.pagesProjects = data.projects || [];
      } else {
        toast.error(data.error || '加载 Pages 失败');
      }
    } catch (error) {
      toast.error('加载 Pages 失败: ' + error.message);
    } finally {
      this.pagesLoading = false;
    }
  },

  /**
   * 删除 Pages 项目
   */
  async deletePagesProject(project) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 Pages 项目 "${project.name}" 吗？此操作不可恢复，所有部署和自定义域名都将被删除。`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages/${encodeURIComponent(project.name)}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('项目已删除');
        await this.loadPages();
      } else {
        const data = await response.json();
        toast.error(data.error || '删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  /**
   * 查看 Pages 部署历史
   */
  async viewPagesDeployments(project) {
    this.selectedPagesProject = project;
    this.showPagesDeploymentsModal = true;
    this.pagesDeploymentsLoading = true;
    this.pagesDeployments = [];

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages/${encodeURIComponent(project.name)}/deployments`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok) {
        this.pagesDeployments = data.deployments || [];
      } else {
        toast.error(data.error || '加载部署历史失败');
      }
    } catch (error) {
      toast.error('加载部署历史失败: ' + error.message);
    } finally {
      this.pagesDeploymentsLoading = false;
    }
  },

  /**
   * 关闭 Pages 部署历史模态框
   */
  closePagesDeploymentsModal() {
    this.showPagesDeploymentsModal = false;
    this.selectedPagesProject = null;
    this.pagesDeployments = [];
  },

  /**
   * 格式化部署状态
   */
  formatDeploymentStatus(status) {
    const map = {
      success: '成功',
      failure: '失败',
      active: '活跃',
      canceled: '已取消',
      queued: '排队中',
      building: '构建中',
    };
    return map[status] || status;
  },

  /**
   * 获取状态对应的 CSS 类
   */
  getDeploymentStatusClass(status) {
    if (status === 'success' || status === 'active') return 'ag-status-online';
    if (status === 'failure') return 'ag-status-offline';
    return 'ag-status-unknown';
  },

  // ==================== Worker 路由管理 ====================

  /**
   * 打开 Worker 路由管理模态框
   */
  async openWorkerRoutesModal(worker) {
    this.selectedWorkerForRoutes = worker;
    this.showWorkerRoutesModal = true;
    this.workerRoutesLoading = true;
    this.workerRoutes = [];

    // 需要先选择一个域名才能获取路由
    if (!store.dnsSelectedZoneId) {
      toast.warning('请先在 DNS 记录标签页选择一个域名，然后再管理路由');
      this.showWorkerRoutesModal = false;
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/workers/routes`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok) {
        this.workerRoutes = data.routes || [];
      } else {
        toast.error(data.error || '加载路由失败');
      }
    } catch (error) {
      toast.error('加载路由失败: ' + error.message);
    } finally {
      this.workerRoutesLoading = false;
    }
  },

  /**
   * 关闭 Worker 路由模态框
   */
  closeWorkerRoutesModal() {
    this.showWorkerRoutesModal = false;
    this.selectedWorkerForRoutes = null;
    this.workerRoutes = [];
  },

  /**
   * 删除 Worker 路由
   */
  async deleteWorkerRoute(route) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除路由 "${route.pattern}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/workers/routes/${route.id}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('路由已删除');
        // 重新加载路由列表
        await this.openWorkerRoutesModal(this.selectedWorkerForRoutes);
      } else {
        const data = await response.json();
        toast.error(data.error || '删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  /**
   * 创建新的 Worker 路由
   */
  async createWorkerRoute() {
    const pattern = this.newRoutePattern?.trim();
    const script = this.newRouteScript?.trim();

    if (!pattern) {
      toast.error('请输入路由模式，例如：example.com/*');
      return;
    }

    if (!store.dnsSelectedZoneId) {
      toast.error('请先在 DNS 记录标签页选择一个域名');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}/workers/routes`,
        {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({
            pattern: pattern,
            script: script || undefined, // 空字符串时不传
          }),
        }
      );
      const data = await response.json();

      if (response.ok) {
        toast.success('路由已创建');
        this.newRoutePattern = '';
        this.newRouteScript = '';
        // 重新加载路由列表
        await this.openWorkerRoutesModal(this.selectedWorkerForRoutes);
      } else {
        toast.error(data.error || '创建失败');
      }
    } catch (error) {
      toast.error('创建失败: ' + error.message);
    }
  },

  // ==================== Pages 自定义域名管理 ====================

  /**
   * 打开 Pages 自定义域名管理模态框
   */
  async openPagesDomainsModal(project) {
    this.selectedPagesProjectForDomains = project;
    this.showPagesDomainsModal = true;
    this.pagesDomainsLoading = true;
    this.pagesDomains = [];
    this.newPagesDomain = '';

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages/${encodeURIComponent(project.name)}/domains`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok) {
        this.pagesDomains = data.domains || [];
      } else {
        toast.error(data.error || '加载域名失败');
      }
    } catch (error) {
      toast.error('加载域名失败: ' + error.message);
    } finally {
      this.pagesDomainsLoading = false;
    }
  },

  /**
   * 关闭 Pages 域名模态框
   */
  closePagesDomainsModal() {
    this.showPagesDomainsModal = false;
    this.selectedPagesProjectForDomains = null;
    this.pagesDomains = [];
    this.newPagesDomain = '';
  },

  /**
   * 添加 Pages 自定义域名
   */
  async addPagesDomain() {
    const domain = this.newPagesDomain?.trim();
    if (!domain) {
      toast.error('请输入域名');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages/${encodeURIComponent(this.selectedPagesProjectForDomains.name)}/domains`,
        {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({ domain }),
        }
      );
      const data = await response.json();

      if (response.ok) {
        toast.success('域名已添加');
        this.newPagesDomain = '';
        // 重新加载域名列表
        await this.openPagesDomainsModal(this.selectedPagesProjectForDomains);
      } else {
        toast.error(data.error || '添加失败');
      }
    } catch (error) {
      toast.error('添加失败: ' + error.message);
    }
  },

  /**
   * 删除 Pages 自定义域名
   */
  async deletePagesDomain(domain) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除域名 "${domain.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/pages/${encodeURIComponent(this.selectedPagesProjectForDomains.name)}/domains/${encodeURIComponent(domain.name)}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('域名已删除');
        await this.openPagesDomainsModal(this.selectedPagesProjectForDomains);
      } else {
        const data = await response.json();
        toast.error(data.error || '删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  /**
   * 获取域名状态显示
   */
  getDomainStatusText(status) {
    const map = {
      active: '已激活',
      pending: '待验证',
      initializing: '初始化中',
      error: '错误',
    };
    return map[status] || status;
  },

  /**
   * 获取域名状态的 CSS 类
   */
  getDomainStatusClass(status) {
    if (status === 'active') return 'ag-status-online';
    if (status === 'error') return 'ag-status-offline';
    return 'ag-status-unknown';
  },

  // ==================== Workers 自定义域名管理 ====================

  /**
   * 打开 Workers 自定义域名管理模态框
   */
  async openWorkerDomainsModal(worker) {
    this.selectedWorkerForDomains = worker;
    this.showWorkerDomainsModal = true;
    this.workerDomainsLoading = true;
    this.workerDomains = [];
    this.newWorkerDomain = '';

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(worker.name)}/domains`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok) {
        this.workerDomains = data.domains || [];
      } else {
        toast.error(data.error || '加载域名失败');
      }
    } catch (error) {
      toast.error('加载域名失败: ' + error.message);
    } finally {
      this.workerDomainsLoading = false;
    }
  },

  /**
   * 关闭 Workers 域名模态框
   */
  closeWorkerDomainsModal() {
    this.showWorkerDomainsModal = false;
    this.selectedWorkerForDomains = null;
    this.workerDomains = [];
    this.newWorkerDomain = '';
  },

  /**
   * 添加 Workers 自定义域名
   */
  async addWorkerDomain() {
    const hostname = this.newWorkerDomain?.trim();
    if (!hostname) {
      toast.error('请输入域名');
      return;
    }

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(this.selectedWorkerForDomains.name)}/domains`,
        {
          method: 'POST',
          headers: store.getAuthHeaders(),
          body: JSON.stringify({ hostname }),
        }
      );
      const data = await response.json();

      if (response.ok) {
        toast.success('域名已添加');
        this.newWorkerDomain = '';
        // 重新加载域名列表
        await this.openWorkerDomainsModal(this.selectedWorkerForDomains);
      } else {
        toast.error(data.error || '添加失败');
      }
    } catch (error) {
      toast.error('添加失败: ' + error.message);
    }
  },

  /**
   * 删除 Workers 自定义域名
   */
  async deleteWorkerDomain(domain) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除域名 "${domain.hostname}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/workers/${encodeURIComponent(this.selectedWorkerForDomains.name)}/domains/${domain.id}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('域名已删除');
        await this.openWorkerDomainsModal(this.selectedWorkerForDomains);
      } else {
        const data = await response.json();
        toast.error(data.error || '删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  // ==================== Zone (域名) 管理 ====================

  /**
   * 打开添加域名模态框
   */
  openAddZoneModal() {
    this.zoneForm = {
      name: '',
      jumpStart: false,
    };
    this.zoneFormError = '';
    this.showAddZoneModal = true;
  },

  /**
   * 保存域名(添加新域名到 Cloudflare)
   */
  async saveZone() {
    const name = this.zoneForm.name?.trim();
    if (!name) {
      this.zoneFormError = '请输入域名';
      return;
    }

    // 简单的域名格式验证
    if (!/^[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9]?\.[a-zA-Z]{2,}$/.test(name)) {
      this.zoneFormError = '请输入有效的域名格式,如 example.com';
      return;
    }

    this.dnsSaving = true;
    this.zoneFormError = '';

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones`, {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          name,
          jumpStart: this.zoneForm.jumpStart,
        }),
      });

      const data = await response.json();

      if (response.ok && data.success) {
        toast.success(`域名 "${name}" 已添加到 Cloudflare`);
        this.showAddZoneModal = false;
        // 重新加载域名列表
        await this.loadDnsZones();
      } else {
        this.zoneFormError = data.error || '添加域名失败';
        toast.error(this.zoneFormError);
      }
    } catch (error) {
      this.zoneFormError = '添加域名失败: ' + error.message;
      toast.error(this.zoneFormError);
    } finally {
      this.dnsSaving = false;
    }
  },

  /**
   * 删除域名
   */
  async deleteZone() {
    if (!store.dnsSelectedZoneId) {
      toast.error('请先选择要删除的域名');
      return;
    }

    // 获取当前选中的域名信息
    const selectedZone = store.dnsZones.find(z => z.id === store.dnsSelectedZoneId);
    if (!selectedZone) {
      toast.error('未找到选中的域名');
      return;
    }

    const confirmed = await store.showConfirm({
      title: '确认删除域名',
      message: `确定要从 Cloudflare 删除域名 "${selectedZone.name}" 吗?\n\n⚠️ 此操作不可恢复,将删除该域名下的所有 DNS 记录、Workers 路由等配置!`,
      icon: 'fa-exclamation-triangle',
      confirmText: '确认删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/zones/${store.dnsSelectedZoneId}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      const data = await response.json();

      if (response.ok && data.success) {
        toast.success(`域名 "${selectedZone.name}" 已从 Cloudflare 删除`);
        // 清除选中的域名
        store.dnsSelectedZoneId = null;
        store.dnsRecords = [];
        // 重新加载域名列表
        await this.loadDnsZones();
      } else {
        toast.error(data.error || '删除域名失败');
      }
    } catch (error) {
      toast.error('删除域名失败: ' + error.message);
    }
  },

  /**
   * 格式化 SSL 模式显示
   */
  formatSslMode(mode) {
    const modes = {
      off: '关闭',
      flexible: '灵活',
      full: '完全',
      strict: '严格',
    };
    return modes[mode] || mode || '加载中...';
  },

  // ==================== Tunnel 管理 ====================

  /**
   * 加载 Tunnel 列表
   */
  async loadTunnels() {
    if (!store.dnsSelectedAccountId) {
      return;
    }

    // 只在无缓存数据时显示 loading
    const isFirstLoad = !store.tunnels || store.tunnels.length === 0;
    if (isFirstLoad) {
      store.tunnelsLoading = true;
    }

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      if (response.ok && data.success) {
        store.tunnels = data.tunnels || [];
      } else {
        toast.error(data.error || '加载 Tunnel 列表失败');
      }
    } catch (error) {
      toast.error('加载 Tunnel 列表失败: ' + error.message);
    } finally {
      store.tunnelsLoading = false;
    }
  },

  /**
   * 打开创建 Tunnel 模态框
   */
  openCreateTunnelModal() {
    store.newTunnelName = '';
    store.showCreateTunnelModal = true;
  },

  /**
   * 创建新 Tunnel
   */
  async createTunnel() {
    const name = store.newTunnelName?.trim();
    if (!name) {
      toast.error('请输入 Tunnel 名称');
      return;
    }

    // 验证名称格式
    if (!/^[a-zA-Z0-9-_]+$/.test(name)) {
      toast.error('Tunnel 名称只能包含字母、数字、连字符和下划线');
      return;
    }

    store.tunnelSaving = true;

    try {
      const response = await fetch(`/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels`, {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name }),
      });
      const data = await response.json();

      if (response.ok && data.success) {
        toast.success(`Tunnel "${name}" 创建成功`);
        store.showCreateTunnelModal = false;
        await this.loadTunnels();
      } else {
        toast.error(data.error || '创建 Tunnel 失败');
      }
    } catch (error) {
      toast.error('创建 Tunnel 失败: ' + error.message);
    } finally {
      store.tunnelSaving = false;
    }
  },

  /**
   * 删除 Tunnel
   */
  async deleteTunnel(tunnel) {
    const confirmed = await store.showConfirm({
      title: '确认删除 Tunnel',
      message: `确定要删除 Tunnel "${tunnel.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${tunnel.id}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success(`Tunnel "${tunnel.name}" 已删除`);
        await this.loadTunnels();
      } else {
        const data = await response.json();
        toast.error(data.error || '删除 Tunnel 失败');
      }
    } catch (error) {
      toast.error('删除 Tunnel 失败: ' + error.message);
    }
  },

  /**
   * 获取 Tunnel Token
   */
  async getTunnelToken(tunnel) {
    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${tunnel.id}/token`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok && data.success) {
        store.selectedTunnelToken = data.token;
        store.selectedTunnelForToken = tunnel;
        store.showTunnelTokenModal = true;
      } else {
        toast.error(data.error || '获取 Tunnel Token 失败');
      }
    } catch (error) {
      toast.error('获取 Tunnel Token 失败: ' + error.message);
    }
  },

  /**
   * 复制 Tunnel Token
   */
  async copyTunnelToken() {
    try {
      await navigator.clipboard.writeText(store.selectedTunnelToken);
      toast.success('Token 已复制到剪贴板');
    } catch (error) {
      toast.error('复制失败: ' + error.message);
    }
  },

  /**
   * 复制 cloudflared 运行命令
   */
  async copyCloudflaredCommand() {
    const command = `cloudflared tunnel run --token ${store.selectedTunnelToken}`;
    try {
      await navigator.clipboard.writeText(command);
      toast.success('命令已复制到剪贴板');
    } catch (error) {
      toast.error('复制失败: ' + error.message);
    }
  },

  /**
   * 关闭 Token 模态框
   */
  closeTunnelTokenModal() {
    store.showTunnelTokenModal = false;
    store.selectedTunnelToken = '';
    store.selectedTunnelForToken = null;
  },

  /**
   * 打开 Tunnel 配置模态框
   */
  async openTunnelConfigModal(tunnel) {
    store.selectedTunnelForConfig = tunnel;
    store.showTunnelConfigModal = true;
    store.tunnelConfigLoading = true;
    store.tunnelConfig = { ingress: [] };

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${tunnel.id}/configuration`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok && data.success) {
        store.tunnelConfig = data.config || { ingress: [] };
      } else {
        toast.error(data.error || '获取 Tunnel 配置失败');
      }
    } catch (error) {
      toast.error('获取 Tunnel 配置失败: ' + error.message);
    } finally {
      store.tunnelConfigLoading = false;
    }
  },

  /**
   * 关闭 Tunnel 配置模态框
   */
  closeTunnelConfigModal() {
    store.showTunnelConfigModal = false;
    store.selectedTunnelForConfig = null;
    store.tunnelConfig = { ingress: [] };
  },

  /**
   * 添加 Ingress 规则
   */
  addIngressRule() {
    if (!store.tunnelConfig.ingress) {
      store.tunnelConfig.ingress = [];
    }
    // 在倒数第二个位置插入（保持 catch-all 在最后）
    const insertIndex = Math.max(0, store.tunnelConfig.ingress.length - 1);
    store.tunnelConfig.ingress.splice(insertIndex, 0, {
      hostname: '',
      service: 'http://localhost:8080',
      path: '',
    });
  },

  /**
   * 删除 Ingress 规则
   */
  removeIngressRule(index) {
    store.tunnelConfig.ingress.splice(index, 1);
  },

  /**
   * 保存 Tunnel 配置
   */
  async saveTunnelConfig() {
    if (!store.selectedTunnelForConfig) return;

    // 验证配置
    const ingress = store.tunnelConfig.ingress || [];
    if (ingress.length === 0) {
      toast.error('至少需要一条 Ingress 规则');
      return;
    }

    // 确保最后一条是 catch-all 规则
    const lastRule = ingress[ingress.length - 1];
    if (lastRule.hostname && lastRule.hostname !== '') {
      // 添加 catch-all 规则
      ingress.push({ service: 'http_status:404' });
    }

    store.tunnelConfigSaving = true;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${store.selectedTunnelForConfig.id}/configuration`,
        {
          method: 'PUT',
          headers: {
            ...store.getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ config: store.tunnelConfig }),
        }
      );
      const data = await response.json();

      if (response.ok && data.success) {
        toast.success('Tunnel 配置已保存');
        this.closeTunnelConfigModal();
      } else {
        toast.error(data.error || '保存 Tunnel 配置失败');
      }
    } catch (error) {
      toast.error('保存 Tunnel 配置失败: ' + error.message);
    } finally {
      store.tunnelConfigSaving = false;
    }
  },

  /**
   * 打开 Tunnel 连接状态模态框
   */
  async openTunnelConnectionsModal(tunnel) {
    store.selectedTunnelForConnections = tunnel;
    store.showTunnelConnectionsModal = true;
    store.tunnelConnectionsLoading = true;
    store.tunnelConnections = [];

    try {
      // 如果 tunnel 对象中已有 connections 数据，直接使用
      if (tunnel.connections && tunnel.connections.length > 0) {
        // Tunnel 列表 API 返回的 connections 结构可能不同，需要适配
        store.tunnelConnections = tunnel.connections.map(c => ({
          id: c.id || c.uuid,
          clientId: c.client_id || c.id,
          clientVersion: c.client_version,
          arch: c.arch,
          connectedAt: c.opened_at || c.run_at,
          coloName: c.colo_name,
        }));
        store.tunnelConnectionsLoading = false;
        return;
      }

      // 否则调用 API 获取
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${tunnel.id}/connections`,
        { headers: store.getAuthHeaders() }
      );
      const data = await response.json();

      if (response.ok && data.success) {
        store.tunnelConnections = data.connections || [];
      } else {
        toast.error(data.error || '获取 Tunnel 连接失败');
      }
    } catch (error) {
      toast.error('获取 Tunnel 连接失败: ' + error.message);
    } finally {
      store.tunnelConnectionsLoading = false;
    }
  },

  /**
   * 关闭 Tunnel 连接模态框
   */
  closeTunnelConnectionsModal() {
    store.showTunnelConnectionsModal = false;
    store.selectedTunnelForConnections = null;
    store.tunnelConnections = [];
  },

  /**
   * 刷新 Tunnel 连接
   */
  async refreshTunnelConnections() {
    if (store.selectedTunnelForConnections) {
      await this.openTunnelConnectionsModal(store.selectedTunnelForConnections);
    }
  },

  /**
   * 清理 Tunnel 所有连接
   */
  async cleanupTunnelConnections(tunnel) {
    const confirmed = await store.showConfirm({
      title: '确认清理连接',
      message: `确定要清理 Tunnel "${tunnel.name}" 的所有连接吗？\n\n建议在更换 Token 后执行此操作。`,
      icon: 'fa-trash',
      confirmText: '清理',
      confirmClass: 'btn-warning',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/cloudflare/accounts/${store.dnsSelectedAccountId}/tunnels/${tunnel.id}/connections`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('连接已清理');
        // 刷新连接列表
        if (store.showTunnelConnectionsModal) {
          await this.openTunnelConnectionsModal(tunnel);
        }
        // 刷新 Tunnel 列表以更新状态
        await this.loadTunnels();
      } else {
        const data = await response.json();
        toast.error(data.error || '清理连接失败');
      }
    } catch (error) {
      toast.error('清理连接失败: ' + error.message);
    }
  },

  /**
   * 获取 Tunnel 状态显示文本
   */
  getTunnelStatusText(status) {
    const map = {
      inactive: '未连接',
      active: '已连接',
      down: '离线',
      degraded: '降级',
    };
    return map[status] || status || '未知';
  },

  /**
   * 获取 Tunnel 状态 CSS 类
   */
  getTunnelStatusClass(status) {
    if (status === 'active' || status === 'healthy') return 'tunnel-status-active';
    if (status === 'inactive' || status === 'down') return 'tunnel-status-inactive';
    return 'tunnel-status-unknown';
  },

  /**
   * 格式化 Tunnel 日期
   */
  formatTunnelDate(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  },
};
