import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import jsQR from 'jsqr';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Tabs } from '@cloudflare/kumo';
import useStore, { DEFAULT_TOTP_SETTINGS } from '../../store.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { buildTotpAccountPayload } from '../../modules/totpPayload.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { BRAND_COLOR_FALLBACK } from '../../components/ui/BrandIcon.jsx';
import { ResponsiveSearchInput, TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Plus } from '../../components/Icons.jsx';
import { TOTP_TABS } from './tabs.jsx';
import {
  buildBrandStyleOptions,
  buildCustomBrandStyleOptions,
  isSVGRepoIcon,
  mergeBrandStyleOptions,
  normalizeHexColor,
  normalizeRemoteBrandIconURL,
  resolveFormColor,
} from './utils.js';
import { GROUP_FILTER_ALL } from './constants.js';
import AccountsTab from './AccountsTab.jsx';
import GroupsTab from './GroupsTab.jsx';
import SettingsTab from './SettingsTab.jsx';
import AccountDialog from './AccountDialog.jsx';
import BrandStyleDialog from './BrandStyleDialog.jsx';
import GroupDialog from './GroupDialog.jsx';
import ExportDialog from './ExportDialog.jsx';

function TotpPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const triggerHaptic = useStore(state => state.triggerHaptic);
  const [totpCurrentTab, setTotpCurrentTab] = useState('accounts');
  const [totpAccounts, setTotpAccounts] = useState([]);
  const [totpGroups, setTotpGroups] = useState([]);
  const [totpCodes, setTotpCodes] = useState({});
  const [totpLoading, setTotpLoading] = useState(false);
  const [totpSearchQuery, setTotpSearchQuery] = useState('');
  const [totpFilterGroup, setTotpFilterGroup] = useState('');
  const [showExtensionGuide, setShowExtensionGuide] = useState(false);

  // 用户设置状态
  const [totpSettings, setTotpSettings] = useState({
    ...DEFAULT_TOTP_SETTINGS,
  });

  // Modal 状态
  const [showAccountModal, setShowAccountModal] = useState(false);
  const [accountModalMode, setAccountModalMode] = useState('add');
  const [editingAccountId, setEditingAccountId] = useState(null);
  const [accountForm, setAccountForm] = useState({
    otp_type: 'totp',
    issuer: '',
    account: '',
    secret: '',
    algorithm: 'SHA1',
    digits: 6,
    period: 30,
    counter: 0,
    group_id: '',
    icon: '',
    color: '',
  });
  const [accountModalError, setAccountModalError] = useState('');
  const [showAdvancedAccountSettings, setShowAdvancedAccountSettings] = useState(false);
  const [importUris, setImportUris] = useState('');
  const [accountModalSaving, setAccountModalSaving] = useState(false);
  const [brandDetecting, setBrandDetecting] = useState(false);
  const [brandStyleOptions, setBrandStyleOptions] = useState([]);
  const [showBrandStyleModal, setShowBrandStyleModal] = useState(false);
  const [customBrandIcons, setCustomBrandIcons] = useState([]);
  const [customBrandIconsLoading, setCustomBrandIconsLoading] = useState(false);
  const [customBrandIconUploading, setCustomBrandIconUploading] = useState(false);
  const [deletingCustomBrandIconId, setDeletingCustomBrandIconId] = useState('');
  const [accountAddTab, setAccountAddTab] = useState('scan');

  // QR 扫码状态
  const [isScanning, setIsScanning] = useState(false);
  const [qrParsing, setQrParsing] = useState(false);
  const [qrError, setQrError] = useState('');
  const scannerRef = useRef(null);
  const scannerStartTimerRef = useRef(null);
  const fileInputRef = useRef(null);
  const brandUploadInputRef = useRef(null);
  const editingAccountIdRef = useRef(null);

  // Group Modal 状态
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [groupModalMode, setGroupModalMode] = useState('add');
  const [editingGroupId, setEditingGroupId] = useState(null);
  const [groupForm, setGroupForm] = useState({ name: '', color: BRAND_COLOR_FALLBACK });

  // Export Modal 状态
  const [showExportModal, setShowExportModal] = useState(false);
  const [exportUris, setExportUris] = useState('');
  const [exportMeta, setExportMeta] = useState(null);

  // Local card reveal state
  const [revealedCodes, setRevealedCodes] = useState({});

  // 获取请求 Headers
  const getAuthHeaders = () => {
    return {
      'Content-Type': 'application/json',
    };
  };

  const getAuthOnlyHeaders = () => {
    return {};
  };

  const cacheDetectedBrandIcon = async item => {
    const sourceUrl = item?.sourceUrl;
    const icon = item?.icon;
    if (!sourceUrl || !isSVGRepoIcon(icon)) return;
    try {
      const remoteRes = await fetch(sourceUrl, { mode: 'cors', credentials: 'omit' });
      if (!remoteRes.ok) return;
      const svg = await remoteRes.text();
      if (!svg || !svg.toLowerCase().includes('<svg')) return;
      await fetch('/api/totp/icons/cache', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ icon, svg }),
      });
    } catch (_) {
      // Browser-side caching is best-effort because many icon hosts block CORS.
    }
  };

  // ==================== 数据接口交互 ====================
  const loadData = async () => {
    setTotpLoading(true);
    try {
      const headers = getAuthHeaders();
      const [accountsRes, groupsRes, settingsRes] = await Promise.all([
        fetch('/api/totp/accounts', { headers }),
        fetch('/api/totp/groups', { headers }),
        fetch('/api/settings', { headers }),
      ]);

      const accountsData = await accountsRes.json();
      const groupsData = await groupsRes.json();
      const settingsData = await settingsRes.json();

      if (accountsData.success) {
        setTotpAccounts(accountsData.data);
      }
      if (groupsData.success) {
        setTotpGroups(groupsData.data);
      }
      if (settingsData.success && settingsData.data?.totpSettings) {
        setTotpSettings(prev => ({ ...prev, ...settingsData.data.totpSettings }));
      }

      // 首次加载验证码
      await refreshCodes();
    } catch (e) {
      console.error(e);
      toast.error('加载 2FA 数据失败');
    } finally {
      setTotpLoading(false);
    }
  };

  const isRefreshingRef = useRef(false);

  const refreshCodes = async () => {
    if (isRefreshingRef.current) return;
    isRefreshingRef.current = true;
    try {
      const res = await fetch('/api/totp/codes', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setTotpCodes(data.data);
      }
    } catch (e) {
      console.error('刷新验证码失败:', e);
    } finally {
      isRefreshingRef.current = false;
    }
  };

  // 持久化保存设置
  const saveSettingsToServer = async newSettings => {
    try {
      const headers = getAuthHeaders();
      await fetch('/api/settings', {
        method: 'PATCH',
        headers,
        body: JSON.stringify({ totpSettings: newSettings }),
      });
    } catch (e) {
      console.error('保存设置失败:', e);
      toast.error('保存设置失败');
    }
  };

  const updateSetting = (key, value) => {
    const newSettings = { ...totpSettings, [key]: value };
    setTotpSettings(newSettings);
    saveSettingsToServer(newSettings);
  };

  // ==================== 倒计时逻辑 ====================
  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (!totpFilterGroup) return;
    const exists = totpGroups.some(group => String(group.id) === String(totpFilterGroup));
    if (!exists) {
      setTotpFilterGroup('');
    }
  }, [totpFilterGroup, totpGroups]);

  useEffect(() => {
    // 用真实流逝时间扣减剩余秒数：后台标签被浏览器节流时 tick 间隔
    // 可能远超 1s，按 tick 数-1 会让倒计时慢于真实时间（漂移）。
    let lastTickAt = Date.now();
    const timer = setInterval(() => {
      const now = Date.now();
      const elapsed = Math.max(1, Math.floor((now - lastTickAt) / 1000));
      lastTickAt = now;
      setTotpCodes(prevCodes => {
        const updated = {};
        let needRefresh = false;
        let changed = false;
        for (const id in prevCodes) {
          const item = prevCodes[id];
          if (item.remaining !== undefined && item.remaining > 0) {
            const nextRemaining = Math.max(0, item.remaining - elapsed);
            updated[id] = { ...item, remaining: nextRemaining };
            changed = true;
            if (nextRemaining <= 0) {
              needRefresh = true;
            }
          } else {
            updated[id] = item;
          }
        }

        if (needRefresh) {
          Promise.resolve().then(() => {
            refreshCodes();
          });
        }
        return changed ? updated : prevCodes;
      });
    }, 1000);

    return () => clearInterval(timer);
  }, []);

  // ==================== 过滤和分组运算 ====================
  const filteredAccounts = useMemo(() => {
    let list = [...totpAccounts];
    if (totpFilterGroup) {
      list = list.filter(a => String(a.group_id) === String(totpFilterGroup));
    }
    if (totpSearchQuery.trim()) {
      const q = totpSearchQuery.toLowerCase();
      list = list.filter(
        a =>
          (a.issuer || '').toLowerCase().includes(q) || (a.account || '').toLowerCase().includes(q)
      );
    }

    if (totpSettings.groupByPlatform) {
      list.sort((a, b) => (a.issuer || '').localeCompare(b.issuer || ''));
    }
    return list;
  }, [totpAccounts, totpFilterGroup, totpSearchQuery, totpSettings.groupByPlatform]);

  const platformCounts = useMemo(() => {
    const counts = {};
    totpAccounts.forEach(a => {
      const key = (a.issuer || '').toLowerCase();
      counts[key] = (counts[key] || 0) + 1;
    });
    return counts;
  }, [totpAccounts]);

  const groupAccountCounts = useMemo(() => {
    const counts = {};
    totpAccounts.forEach(a => {
      if (a.group_id) {
        counts[a.group_id] = (counts[a.group_id] || 0) + 1;
      }
    });
    return counts;
  }, [totpAccounts]);

  const groupFilterTabs = useMemo(
    () => [
      { value: GROUP_FILTER_ALL, label: '总' },
      ...totpGroups.map(group => ({
        value: String(group.id),
        label: group.name,
      })),
    ],
    [totpGroups]
  );
  const hasGroupTabs = totpGroups.length > 0;

  // ==================== 账号编辑与删除 ====================
  const handleOpenAddAccount = () => {
    const defaultMode = totpSettings.lockInputMode ? totpSettings.defaultInputMode : 'scan';

    setAccountAddTab(defaultMode === 'manual' ? 'manual' : 'scan');
    setAccountForm({
      otp_type: 'totp',
      issuer: '',
      account: '',
      secret: '',
      algorithm: 'SHA1',
      digits: 6,
      period: 30,
      counter: 0,
      group_id: '',
      icon: '',
      color: '',
    });
    setAccountModalMode('add');
    editingAccountIdRef.current = null;
    setAccountModalError('');
    setShowAdvancedAccountSettings(false);
    setImportUris('');
    setQrError('');
    setBrandStyleOptions([]);
    setCustomBrandIcons([]);
    setShowAccountModal(true);
  };

  const loadAccountSecret = useCallback(async accountId => {
    try {
      const res = await fetch(`/api/totp/accounts/${accountId}?showSecret=true`, {
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data.success || !data.data?.secret) {
        throw new Error(data.error || '获取密钥失败');
      }
      setAccountForm(prev =>
        editingAccountIdRef.current === accountId ? { ...prev, secret: data.data.secret } : prev
      );
    } catch (error) {
      console.error(error);
      toast.error(error.message || '获取密钥失败');
    }
  }, []);

  const handleOpenEditAccount = async account => {
    setAccountModalMode('edit');
    setAccountAddTab('manual');
    setEditingAccountId(account.id);
    editingAccountIdRef.current = account.id;
    setAccountForm({
      otp_type: account.otp_type || 'totp',
      issuer: account.issuer || '',
      account: account.account || '',
      secret: '••••••••••••••••',
      algorithm: account.algorithm || 'SHA1',
      digits: account.digits || 6,
      period: account.period || 30,
      counter: account.counter || 0,
      group_id: account.group_id || '',
      icon: account.icon || '',
      color: account.color || '',
    });
    setAccountModalError('');
    setShowAdvancedAccountSettings(false);
    setBrandStyleOptions([]);
    setCustomBrandIcons([]);
    setShowAccountModal(true);
    void loadAccountSecret(account.id);
  };

  const loadCustomBrandIcons = useCallback(async () => {
    setCustomBrandIconsLoading(true);
    try {
      const res = await fetch('/api/totp/icons/library', { headers: getAuthOnlyHeaders() });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || '加载图标库失败');
      }
      const list = Array.isArray(data.data) ? data.data : [];
      setCustomBrandIcons(list);
      return list;
    } catch (error) {
      toast.error(error.message || '加载图标库失败');
      return [];
    } finally {
      setCustomBrandIconsLoading(false);
    }
  }, []);

  const uploadCustomBrandIconAsset = useCallback(
    async (file, fallbackName = '') => {
      const formData = new FormData();
      formData.append('file', file);
      formData.append(
        'name',
        String(accountForm.issuer || fallbackName || file.name || '自定义图标').trim()
      );
      formData.append('issuer', String(accountForm.issuer || '').trim());
      formData.append(
        'color',
        normalizeHexColor(accountForm.color) || resolveFormColor(accountForm)
      );
      const res = await fetch('/api/totp/icons/library', {
        method: 'POST',
        headers: getAuthOnlyHeaders(),
        body: formData,
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || '上传图标失败');
      }
      const uploaded = data.data || {};
      const nextIcon = uploaded.icon || (uploaded.id ? `custom:${uploaded.id}` : '');
      setAccountForm(prev => ({
        ...prev,
        icon: nextIcon,
        color: uploaded.color || prev.color,
      }));
      const nextLibrary = await loadCustomBrandIcons();
      const customOptions = buildCustomBrandStyleOptions({
        issuer: accountForm.issuer,
        entries: nextLibrary,
        fallbackColor: resolveFormColor(accountForm),
      });
      setBrandStyleOptions(prev => mergeBrandStyleOptions(customOptions, prev));
      setShowBrandStyleModal(true);
      return uploaded;
    },
    [accountForm.color, accountForm.issuer, loadCustomBrandIcons]
  );

  const importCustomBrandIconFromURL = useCallback(
    async sourceURL => {
      const normalizedURL = normalizeRemoteBrandIconURL(sourceURL);
      if (!normalizedURL) {
        throw new Error('请粘贴有效的 http/https 图片链接');
      }
      const res = await fetch('/api/totp/icons/library/import-url', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          url: normalizedURL,
          name: String(accountForm.issuer || '').trim(),
          issuer: String(accountForm.issuer || '').trim(),
          color: normalizeHexColor(accountForm.color) || resolveFormColor(accountForm),
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || '下载图标失败');
      }
      const uploaded = data.data || {};
      const nextIcon = uploaded.icon || (uploaded.id ? `custom:${uploaded.id}` : '');
      setAccountForm(prev => ({
        ...prev,
        icon: nextIcon,
        color: uploaded.color || prev.color,
      }));
      const nextLibrary = await loadCustomBrandIcons();
      const customOptions = buildCustomBrandStyleOptions({
        issuer: accountForm.issuer,
        entries: nextLibrary,
        fallbackColor: resolveFormColor(accountForm),
      });
      setBrandStyleOptions(prev => mergeBrandStyleOptions(customOptions, prev));
      setShowBrandStyleModal(true);
      return uploaded;
    },
    [accountForm.color, accountForm.issuer, loadCustomBrandIcons]
  );

  const openBrandStylePicker = useCallback(
    async (baseOptions = null) => {
      const fallbackOptions =
        baseOptions ||
        buildBrandStyleOptions({
          issuer: accountForm.issuer,
          icon: accountForm.icon,
          color: resolveFormColor(accountForm),
          name: accountForm.issuer || accountForm.account || '品牌',
        });
      const library = await loadCustomBrandIcons();
      const customOptions = buildCustomBrandStyleOptions({
        issuer: accountForm.issuer,
        entries: library,
        fallbackColor: resolveFormColor(accountForm),
      });
      setBrandStyleOptions(mergeBrandStyleOptions(customOptions, fallbackOptions));
      setShowBrandStyleModal(true);
    },
    [
      accountForm.account,
      accountForm.icon,
      accountForm.issuer,
      accountForm.color,
      loadCustomBrandIcons,
    ]
  );

  const detectAccountBrandIcon = async () => {
    setBrandDetecting(true);
    try {
      const res = await fetch('/api/totp/icons/detect', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          issuer: accountForm.issuer,
          account: accountForm.account,
          query: accountForm.icon,
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || '检测失败');
      }
      if (!data.data?.matched) {
        await openBrandStylePicker(
          buildBrandStyleOptions({
            issuer: accountForm.issuer,
            icon: accountForm.icon,
            color: resolveFormColor(accountForm),
            name: accountForm.issuer || accountForm.account || '品牌',
          })
        );
        toast.info(data.data?.message || '未检测到远程图标，已提供系统图标样式', { isManual: true });
        return;
      }
      const baseOptions = buildBrandStyleOptions({
        issuer: accountForm.issuer,
        icon: data.data.icon,
        color: data.data.color,
        name: data.data.name,
        options: data.data.options,
      });
      const detectedItems = [
        data.data,
        ...(Array.isArray(data.data.options) ? data.data.options : []),
      ];
      detectedItems.forEach(item => {
        cacheDetectedBrandIcon(item);
      });
      await openBrandStylePicker(baseOptions);
    } catch (error) {
      toast.error(error.message || '检测图标失败');
    } finally {
      setBrandDetecting(false);
    }
  };

  const handleCustomBrandIconUpload = async event => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setCustomBrandIconUploading(true);
    try {
      await uploadCustomBrandIconAsset(file, file.name);
      toast.success('已上传并应用自定义图标');
    } catch (error) {
      toast.error(error.message || '上传图标失败');
    } finally {
      setCustomBrandIconUploading(false);
    }
  };

  const uploadCustomBrandIconFromClipboardItems = useCallback(async (items = []) => {
    for (const item of items) {
      if (!item) continue;
      if (item.kind === 'file') {
        const file = item.getAsFile?.();
        if (file) {
          const ext =
            file.type === 'image/svg+xml'
              ? 'svg'
              : file.type === 'image/png'
                ? 'png'
                : file.type === 'image/jpeg'
                  ? 'jpg'
                  : file.type === 'image/webp'
                    ? 'webp'
                    : file.type === 'image/gif'
                      ? 'gif'
                      : 'png';
          return {
            type: 'file',
            value: new File([file], file.name || `clipboard-icon.${ext}`, {
              type: file.type || 'image/png',
            }),
          };
        }
      }
      if (item.kind === 'string' && item.type === 'text/plain') {
        const text = await new Promise(resolve => item.getAsString(resolve));
        if (typeof text === 'string' && text.toLowerCase().includes('<svg')) {
          return {
            type: 'file',
            value: new File([text], 'clipboard-icon.svg', { type: 'image/svg+xml' }),
          };
        }
        const sourceURL = normalizeRemoteBrandIconURL(text);
        if (sourceURL) {
          return { type: 'url', value: sourceURL };
        }
      }
    }
    return null;
  }, []);

  const handleBrandLibraryPaste = useCallback(
    async event => {
      const items = Array.from(event.clipboardData?.items || []);
      if (items.length === 0) return;
      event.preventDefault();
      setCustomBrandIconUploading(true);
      try {
        const asset = await uploadCustomBrandIconFromClipboardItems(items);
        if (!asset) {
          throw new Error('剪贴板里没有可用的图片、SVG 图标或图片链接');
        }
        if (asset.type === 'url') {
          await importCustomBrandIconFromURL(asset.value);
          toast.success('已从链接下载并应用图标');
        } else {
          await uploadCustomBrandIconAsset(asset.value, 'clipboard-icon');
          toast.success('已从剪贴板粘贴并应用图标');
        }
      } catch (error) {
        toast.error(error.message || '粘贴图标失败');
      } finally {
        setCustomBrandIconUploading(false);
      }
    },
    [
      importCustomBrandIconFromURL,
      uploadCustomBrandIconAsset,
      uploadCustomBrandIconFromClipboardItems,
    ]
  );

  const handlePasteBrandIconFromClipboard = useCallback(async () => {
    if (!navigator.clipboard?.read) {
      toast.info('当前环境不支持直接读取剪贴板，请在下方区域按 Ctrl+V 粘贴', { isManual: true });
      return;
    }
    setCustomBrandIconUploading(true);
    try {
      const clipboardItems = await navigator.clipboard.read();
      let uploaded = false;
      let clipboardText = '';
      for (const clipboardItem of clipboardItems) {
        const types = clipboardItem.types || [];
        const imageType = types.find(type =>
          ['image/svg+xml', 'image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(type)
        );
        if (imageType) {
          const blob = await clipboardItem.getType(imageType);
          const ext =
            imageType === 'image/svg+xml'
              ? 'svg'
              : imageType === 'image/png'
                ? 'png'
                : imageType === 'image/jpeg'
                  ? 'jpg'
                  : imageType === 'image/webp'
                    ? 'webp'
                    : 'gif';
          const file = new File([blob], `clipboard-icon.${ext}`, { type: imageType });
          await uploadCustomBrandIconAsset(file, 'clipboard-icon');
          uploaded = true;
          break;
        }
        if (!clipboardText && types.includes('text/plain')) {
          const textBlob = await clipboardItem.getType('text/plain');
          clipboardText = await textBlob.text();
        }
      }
      if (!uploaded && clipboardText.toLowerCase().includes('<svg')) {
        const file = new File([clipboardText], 'clipboard-icon.svg', { type: 'image/svg+xml' });
        await uploadCustomBrandIconAsset(file, 'clipboard-icon');
        uploaded = true;
      }
      let importedFromURL = false;
      if (!uploaded) {
        const sourceURL = normalizeRemoteBrandIconURL(clipboardText);
        if (sourceURL) {
          await importCustomBrandIconFromURL(sourceURL);
          uploaded = true;
          importedFromURL = true;
        }
      }
      if (!uploaded) {
        throw new Error('剪贴板中没有可上传的图标或图片链接');
      }
      toast.success(importedFromURL ? '已从链接下载并应用图标' : '已从剪贴板粘贴并应用图标');
    } catch (error) {
      toast.error(error.message || '读取剪贴板失败');
    } finally {
      setCustomBrandIconUploading(false);
    }
  }, [importCustomBrandIconFromURL, uploadCustomBrandIconAsset]);

  const applyBrandStyleOption = option => {
    setAccountForm(prev => ({
      ...prev,
      icon: option.icon || '',
      color: option.color || prev.color,
    }));
    setShowBrandStyleModal(false);
    toast.success(`已选择${option.label}`);
  };

  const deleteCustomBrandIcon = async option => {
    if (!option?.customId) return;
    if (!confirmPress(`custom-icon-${option.customId}`, `删除自定义图标「${option.label}」`)) return;
    setDeletingCustomBrandIconId(option.customId);
    try {
      const res = await fetch(`/api/totp/icons/library/${encodeURIComponent(option.customId)}`, {
        method: 'DELETE',
        headers: getAuthOnlyHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data.success) {
        throw new Error(data.error || '删除图标失败');
      }
      setCustomBrandIcons(prev => prev.filter(item => item.id !== option.customId));
      setBrandStyleOptions(prev => prev.filter(item => item.customId !== option.customId));
      setTotpAccounts(prev =>
        prev.map(account => (account.icon === option.icon ? { ...account, icon: '' } : account))
      );
      setAccountForm(prev => (prev.icon === option.icon ? { ...prev, icon: '' } : prev));
      toast.success('自定义图标已删除');
    } catch (error) {
      toast.error(error.message || '删除图标失败');
    } finally {
      setDeletingCustomBrandIconId('');
    }
  };

  const handleSaveAccount = async () => {
    setAccountModalError('');

    if (!accountForm.issuer.trim()) {
      setAccountModalError('请输入发行商名称');
      return;
    }
    if (accountModalMode === 'add' && !accountForm.secret.trim()) {
      setAccountModalError('请输入密钥');
      return;
    }

    setAccountModalSaving(true);
    try {
      const payload = buildTotpAccountPayload(accountForm, {
        includeSecret: accountModalMode === 'add',
      });

      const url =
        accountModalMode === 'add'
          ? '/api/totp/accounts'
          : `/api/totp/accounts/${editingAccountId}`;

      const res = await fetch(url, {
        method: accountModalMode === 'add' ? 'POST' : 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });

      const result = await res.json();
      if (result.success) {
        toast.success(accountModalMode === 'add' ? '账号添加成功' : '账号更新成功');
        setShowAccountModal(false);
        await loadData();
      } else {
        setAccountModalError(result.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      setAccountModalError('保存失败');
    } finally {
      setAccountModalSaving(false);
    }
  };

  const handleDeleteAccount = async account => {
    if (!confirmPress(`totp-account-${account.id}`, `删除「${account.issuer}」的账号`)) {
      return;
    }

    try {
      const res = await fetch(`/api/totp/accounts/${account.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('账号已删除');
        await loadData();
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除失败');
    }
  };

  const incrementHotp = async account => {
    try {
      const res = await fetch(`/api/totp/accounts/${account.id}/increment`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        setTotpCodes(prev => ({
          ...prev,
          [account.id]: {
            ...prev[account.id],
            code: data.data.code,
            counter: data.data.counter,
          },
        }));
        toast.success('HOTP 计数器已递增');
      }
    } catch (e) {
      console.error(e);
      toast.error('递增失败');
    }
  };

  // ==================== 扫码与导入解析 ====================
  // 扫码库按需加载：仅在用户点击扫码时注入，避免所有访客都下载 ~380KB 第三方脚本
  const loadHtml5Qrcode = () =>
    new Promise((resolve, reject) => {
      if (window.Html5Qrcode) {
        resolve();
        return;
      }
      const existing = document.querySelector('script[data-html5-qrcode]');
      if (existing) {
        existing.addEventListener('load', resolve);
        existing.addEventListener('error', reject);
        return;
      }
      const script = document.createElement('script');
      script.src = 'https://unpkg.com/html5-qrcode@2.3.8/html5-qrcode.min.js';
      script.async = true;
      script.dataset.html5Qrcode = 'true';
      script.addEventListener('load', resolve);
      script.addEventListener('error', reject);
      document.head.appendChild(script);
    });

  const startQrScan = async () => {
    try {
      await loadHtml5Qrcode();
    } catch {
      toast.error('扫码库加载失败');
      return;
    }
    if (!window.Html5Qrcode) {
      toast.error('扫码库加载失败');
      return;
    }
    if (
      !window.isSecureContext &&
      location.hostname !== 'localhost' &&
      location.hostname !== '127.0.0.1'
    ) {
      setQrError('摄像头功能仅支持 HTTPS 环境。如果是移动端访问，请确认服务器域名已开启 SSL。');
      toast.warning('环境不受支持');
      return;
    }

    setQrError('');

    // 在用户显式点击手势（User Gesture）中，率先触发原生的 getUserMedia 授权请求
    if (navigator.mediaDevices && typeof navigator.mediaDevices.getUserMedia === 'function') {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: 'environment' } },
        });
        if (stream && stream.getTracks) {
          stream.getTracks().forEach(track => track.stop());
        }
      } catch (permErr) {
        console.warn(
          'Initial getUserMedia with environment facingMode failed, retrying with simple video constraint',
          permErr
        );
        try {
          const fallbackStream = await navigator.mediaDevices.getUserMedia({ video: true });
          if (fallbackStream && fallbackStream.getTracks) {
            fallbackStream.getTracks().forEach(track => track.stop());
          }
        } catch (permErr2) {
          console.error('Camera permission request denied', permErr2);
          let friendlyMsg = '未获得摄像头访问权限';
          if (permErr2.name === 'NotAllowedError' || permErr2.name === 'PermissionDeniedError') {
            friendlyMsg =
              '未获得摄像头访问权限，请在手机系统或浏览器地址栏安全图标中开启摄像头权限';
          } else if (permErr2.name === 'NotFoundError') {
            friendlyMsg = '未发现可用的摄像头';
          } else if (permErr2.name === 'NotReadableError') {
            friendlyMsg = '摄像头已被其他应用占用';
          }
          setQrError(
            `${friendlyMsg} (${permErr2.message || permErr2.name || 'Permission denied'})`
          );
          toast.error(friendlyMsg);
          return;
        }
      }
    }

    setIsScanning(true);

    scannerStartTimerRef.current = setTimeout(async () => {
      scannerStartTimerRef.current = null;
      try {
        const html5QrCode = new window.Html5Qrcode('qr-reader');
        scannerRef.current = html5QrCode;

        const config = {
          fps: 15,
          aspectRatio: 1,
          qrbox: { width: 250, height: 250 },
        };

        const successCallback = async decodedText => {
          if (decodedText.startsWith('otpauth://')) {
            triggerHaptic('success');
            await stopQrScan();

            if (totpSettings.autoSave) {
              await importUrisDirectly(decodedText);
            } else {
              setImportUris(prev => (prev ? prev + '\n' + decodedText : decodedText));
              toast.success('扫码成功');
            }
          }
        };

        try {
          await html5QrCode.start({ facingMode: 'environment' }, config, successCallback, () => {});
        } catch (err) {
          try {
            await html5QrCode.start({ facingMode: 'user' }, config, successCallback, () => {});
          } catch (err2) {
            const devices = await window.Html5Qrcode.getCameras();
            if (devices && devices.length > 0) {
              await html5QrCode.start(devices[0].id, config, successCallback, () => {});
            } else {
              throw new Error('未检测到任何摄像头设备', { cause: err2 });
            }
          }
        }
      } catch (err) {
        console.error(err);
        let friendlyMsg = '启动摄像头失败';
        if (err.name === 'NotAllowedError') friendlyMsg = '未获得摄像头访问权限';
        else if (err.name === 'NotFoundError') friendlyMsg = '未发现可用的摄像头';
        else if (err.name === 'NotReadableError') friendlyMsg = '摄像头已被占用或故障';
        setQrError(`${friendlyMsg}: ${err.message || '未知错误'}`);
        setIsScanning(false);
      }
    }, 50);
  };

  const stopQrScan = async () => {
    if (scannerStartTimerRef.current) {
      clearTimeout(scannerStartTimerRef.current);
      scannerStartTimerRef.current = null;
    }
    if (scannerRef.current) {
      try {
        await scannerRef.current.stop();
      } catch (err) {
        console.error(err);
      }
      scannerRef.current = null;
    }
    setIsScanning(false);
  };

  useEffect(() => {
    return () => {
      if (scannerStartTimerRef.current) {
        clearTimeout(scannerStartTimerRef.current);
        scannerStartTimerRef.current = null;
      }
      if (scannerRef.current) {
        try {
          void scannerRef.current.stop();
        } catch (err) {
          console.error(err);
        }
        scannerRef.current = null;
      }
    };
  }, []);

  const parseQrImage = async blob => {
    try {
      setQrParsing(true);
      setQrError('');

      const img = new Image();
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');

      await new Promise((resolve, reject) => {
        img.onload = resolve;
        img.onerror = reject;
        img.src = URL.createObjectURL(blob);
      });

      canvas.width = img.width;
      canvas.height = img.height;
      ctx.drawImage(img, 0, 0);

      const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
      const code = jsQR(imageData.data, imageData.width, imageData.height);

      if (code && code.data.startsWith('otpauth://')) {
        const uri = code.data;
        if (totpSettings.autoSave) {
          await importUrisDirectly(uri);
        } else {
          setImportUris(prev => (prev ? prev + '\n' + uri : uri));
          toast.success('二维码解析成功');
        }
      } else {
        setQrError('无法识别二维码或不是有效的 OTP URI');
      }
      URL.revokeObjectURL(img.src);
    } catch (e) {
      console.error(e);
      setQrError('二维码解析失败');
    } finally {
      setQrParsing(false);
    }
  };

  const handleQrPaste = async e => {
    const items = e.clipboardData?.items;
    if (!items) return;

    for (const item of items) {
      if (item.type.startsWith('image/')) {
        e.preventDefault();
        const blob = item.getAsFile();
        await parseQrImage(blob);
        return;
      }
    }
  };

  const handleQrUpload = async e => {
    const file = e.target.files?.[0];
    if (!file) return;
    await parseQrImage(file);
    e.target.value = ''; // Reset input
  };

  const importUrisDirectly = async urisText => {
    const uris = urisText
      .split('\n')
      .map(line => line.trim())
      .filter(line => line.startsWith('otpauth://'));
    const backupPayload = uris.length === 0 ? urisText.trim() : '';

    if (uris.length === 0 && !backupPayload) {
      toast.warning('没有找到有效的 URI 或加密备份');
      return;
    }

    try {
      const importBody = uris.length > 0 ? { uris } : { backup: backupPayload };
      const previewRes = await fetch('/api/totp/import/preview', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(importBody),
      });
      const previewData = await previewRes.json();
      if (!previewRes.ok || !previewData.success) {
        throw new Error(previewData.error || '导入预览失败');
      }
      const preview = previewData.data;
      if (preview.errors?.length) {
        toast.warning(`导入预览发现 ${preview.errors.length} 个错误`);
      }
      if (preview.duplicates > 0) {
        toast.warning(`导入预览发现 ${preview.duplicates} 个重复项`);
      }

      const res = await fetch('/api/totp/import', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(importBody),
      });
      const data = await res.json();
      if (data.success) {
        toast.success(`导入成功: 新增 ${data.data.success} 个账号`);
        setShowAccountModal(false);
        await loadData();
      } else {
        toast.error(data.error || '导入失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('导入失败');
    }
  };

  // ==================== 分组管理 ====================
  const handleOpenAddGroup = () => {
    setGroupModalMode('add');
    setGroupForm({ name: '', color: BRAND_COLOR_FALLBACK });
    setShowGroupModal(true);
  };

  const handleOpenEditGroup = group => {
    setGroupModalMode('edit');
    setEditingGroupId(group.id);
    setGroupForm({ name: group.name, color: group.color || BRAND_COLOR_FALLBACK });
    setShowGroupModal(true);
  };

  const handleSaveGroup = async () => {
    if (!groupForm.name.trim()) {
      toast.warning('请输入分组名称');
      return;
    }

    try {
      const url =
        groupModalMode === 'add' ? '/api/totp/groups' : `/api/totp/groups/${editingGroupId}`;

      const res = await fetch(url, {
        method: groupModalMode === 'add' ? 'POST' : 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(groupForm),
      });

      const data = await res.json();
      if (data.success) {
        toast.success(groupModalMode === 'add' ? '分组创建成功' : '分组更新成功');
        setShowGroupModal(false);
        await loadData();
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('保存失败');
    }
  };

  const handleDeleteGroup = async group => {
    if (!confirmPress(`totp-group-${group.id}`, `删除分组「${group.name}」`)) {
      return;
    }

    try {
      const res = await fetch(`/api/totp/groups/${group.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        toast.success('分组已删除');
        await loadData();
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除失败');
    }
  };

  // ==================== 导出导入数据 ====================
  const handleExportAccounts = async () => {
    if (totpAccounts.length === 0) {
      toast.warning('没有可导出的账号');
      return;
    }
    try {
      const res = await fetch('/api/totp/export', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        if (data.format === 'encrypted-backup') {
          setExportUris(data.data.payload);
          setExportMeta(data.data);
        } else {
          setExportUris(Array.isArray(data.data) ? data.data.join('\n') : String(data.data || ''));
          setExportMeta({ format: data.format || 'uri' });
        }
        setShowExportModal(true);
        toast.success('已生成导出数据');
      } else {
        toast.error(data.error || '导出失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('导出失败');
    }
  };

  const copyExportedUris = async () => {
    if (!exportUris) return;
    try {
      await navigator.clipboard.writeText(exportUris);
      toast.success('导出数据已复制到剪贴板');
    } catch (e) {
      toast.error('复制失败');
    }
  };

  const copyCodeToClipboard = async account => {
    const code = totpCodes[account.id]?.code;
    if (!code) return;

    try {
      await navigator.clipboard.writeText(code);
      toast.success(`验证码已复制: ${code}`);
    } catch (e) {
      toast.error('复制失败');
    }
  };

  // 同步配置到浏览器扩展
  const syncConfigToExtension = async () => {
    try {
      const response = await fetch('/api/auth/plugin-pairings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ name: `浏览器插件 ${new Date().toLocaleDateString('zh-CN')}` }),
      });
      const result = await response.json();
      const pairing = result.data || result;
      if (!response.ok || !pairing.code) {
        throw new Error(result.error || '生成插件配对码失败');
      }
      await navigator.clipboard.writeText(pairing.code);
      toast.success('一次性配对码已复制，10 分钟内到插件设置页兑换');
    } catch (error) {
      toast.error(error.message || '生成插件配对码失败');
    }
  };

  // Helper formats code displaying
  const formatTotpCode = (account, code) => {
    const digits = account.digits || 6;
    const isRevealed = revealedCodes[account.id] || false;

    if (totpSettings.hideCode && !isRevealed) {
      if (digits === 8) return '•••• ••••';
      return '••• •••';
    }

    if (!code) {
      if (digits === 8) return '0000 0000';
      return '000 000';
    }

    const cleanCode = code.replace(/\s/g, '');
    if (cleanCode.length === 6) {
      return cleanCode.slice(0, 3) + ' ' + cleanCode.slice(3);
    }
    if (cleanCode.length === 8) {
      return cleanCode.slice(0, 4) + ' ' + cleanCode.slice(4);
    }
    return cleanCode;
  };

  const getTotpCodeParts = (account, code) => {
    const formatted = formatTotpCode(account, code);
    const parts = formatted.split(' ');
    return parts.length > 1 ? parts : [formatted];
  };

  const handleCardMouseEnter = accountId => {
    if (totpSettings.allowRevealCode) {
      setRevealedCodes(prev => ({ ...prev, [accountId]: true }));
    }
  };

  const handleCardMouseLeave = accountId => {
    if (totpSettings.allowRevealCode) {
      setRevealedCodes(prev => ({ ...prev, [accountId]: false }));
    }
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={totpCurrentTab}
          onValueChange={setTotpCurrentTab}
          tabs={TOTP_TABS}
        />

        {totpCurrentTab === 'accounts' && (
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {hasGroupTabs && (
              <Tabs
                {...TOOL_TABS_PROPS}
                value={totpFilterGroup || GROUP_FILTER_ALL}
                onValueChange={value => {
                  const nextValue = String(value);
                  setTotpFilterGroup(nextValue === GROUP_FILTER_ALL ? '' : nextValue);
                }}
                tabs={groupFilterTabs}
                className="min-w-0 max-w-full flex-1 cq-md:flex-none"
              />
            )}

            <ResponsiveSearchInput
              value={totpSearchQuery}
              onChange={e => setTotpSearchQuery(e.target.value)}
              placeholder="搜索账号..."
              ariaLabel="搜索 TOTP 账号"
              className="cq-md:max-w-48"
            />

            <TabBarOverflowActions
              items={[
                {
                  key: 'add-account',
                  label: '添加账号',
                  icon: <Plus className="w-4 h-4" />,
                  onClick: handleOpenAddAccount,
                  variant: 'primary',
                },
              ]}
            />
          </div>
        )}

        {totpCurrentTab === 'groups' && (
          <TabBarOverflowActions
            items={[
              {
                key: 'add-group',
                label: '新建分组',
                icon: <Plus className="w-4 h-4" />,
                onClick: handleOpenAddGroup,
                variant: 'primary',
              },
            ]}
          />
        )}
      </div>

      {/* ==================== 1. 验证码卡片列表 ==================== */}
      {totpCurrentTab === 'accounts' && (
        <AccountsTab
          totpLoading={totpLoading}
          filteredAccounts={filteredAccounts}
          totpSearchQuery={totpSearchQuery}
          handleOpenAddAccount={handleOpenAddAccount}
          totpSettings={totpSettings}
          totpCodes={totpCodes}
          platformCounts={platformCounts}
          isArmed={isArmed}
          handleCardMouseEnter={handleCardMouseEnter}
          handleCardMouseLeave={handleCardMouseLeave}
          copyCodeToClipboard={copyCodeToClipboard}
          handleOpenEditAccount={handleOpenEditAccount}
          handleDeleteAccount={handleDeleteAccount}
          incrementHotp={incrementHotp}
          getTotpCodeParts={getTotpCodeParts}
        />
      )}

      {/* ==================== 2. 分组列表 ==================== */}
      {totpCurrentTab === 'groups' && (
        <GroupsTab
          totpGroups={totpGroups}
          handleOpenAddGroup={handleOpenAddGroup}
          groupAccountCounts={groupAccountCounts}
          isArmed={isArmed}
          handleOpenEditGroup={handleOpenEditGroup}
          handleDeleteGroup={handleDeleteGroup}
        />
      )}

      {/* ==================== 3. 选项设置 ==================== */}
      {totpCurrentTab === 'settings' && (
        <SettingsTab
          totpSettings={totpSettings}
          updateSetting={updateSetting}
          importUrisDirectly={importUrisDirectly}
          handleExportAccounts={handleExportAccounts}
          refreshCodes={refreshCodes}
          showExtensionGuide={showExtensionGuide}
          setShowExtensionGuide={setShowExtensionGuide}
          syncConfigToExtension={syncConfigToExtension}
        />
      )}

      {/* ==================== 模态框 1: 账号添加/修改 ==================== */}
      <AccountDialog
        showAccountModal={showAccountModal}
        setShowAccountModal={setShowAccountModal}
        stopQrScan={stopQrScan}
        accountModalMode={accountModalMode}
        accountAddTab={accountAddTab}
        setAccountAddTab={setAccountAddTab}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        isScanning={isScanning}
        startQrScan={startQrScan}
        fileInputRef={fileInputRef}
        handleQrUpload={handleQrUpload}
        qrParsing={qrParsing}
        qrError={qrError}
        handleQrPaste={handleQrPaste}
        importUris={importUris}
        setImportUris={setImportUris}
        totpGroups={totpGroups}
        brandDetecting={brandDetecting}
        detectAccountBrandIcon={detectAccountBrandIcon}
        openBrandStylePicker={openBrandStylePicker}
        showAdvancedAccountSettings={showAdvancedAccountSettings}
        setShowAdvancedAccountSettings={setShowAdvancedAccountSettings}
        accountModalError={accountModalError}
        importUrisDirectly={importUrisDirectly}
        handleSaveAccount={handleSaveAccount}
        accountModalSaving={accountModalSaving}
      />

      {/* ==================== 模态框: 品牌图标样式选择 ==================== */}
      <BrandStyleDialog
        showBrandStyleModal={showBrandStyleModal}
        setShowBrandStyleModal={setShowBrandStyleModal}
        brandUploadInputRef={brandUploadInputRef}
        handleCustomBrandIconUpload={handleCustomBrandIconUpload}
        customBrandIconUploading={customBrandIconUploading}
        handlePasteBrandIconFromClipboard={handlePasteBrandIconFromClipboard}
        handleBrandLibraryPaste={handleBrandLibraryPaste}
        customBrandIconsLoading={customBrandIconsLoading}
        brandStyleOptions={brandStyleOptions}
        resolveFormColor={resolveFormColor}
        accountForm={accountForm}
        applyBrandStyleOption={applyBrandStyleOption}
        isArmed={isArmed}
        deletingCustomBrandIconId={deletingCustomBrandIconId}
        deleteCustomBrandIcon={deleteCustomBrandIcon}
      />

      {/* ==================== 模态框 2: 新建/编辑分组 ==================== */}
      <GroupDialog
        showGroupModal={showGroupModal}
        setShowGroupModal={setShowGroupModal}
        groupModalMode={groupModalMode}
        groupForm={groupForm}
        setGroupForm={setGroupForm}
        handleSaveGroup={handleSaveGroup}
      />

      {/* ==================== 模态框 3: 备份导出账号 ==================== */}
      <ExportDialog
        showExportModal={showExportModal}
        setShowExportModal={setShowExportModal}
        exportUris={exportUris}
        exportMeta={exportMeta}
        copyExportedUris={copyExportedUris}
      />
    </div>
  );
}

export default TotpPage;
