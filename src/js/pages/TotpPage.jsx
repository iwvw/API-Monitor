import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import jsQR from 'jsqr';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import {
  Key,
  FolderOpen,
  Settings,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  History,
  Shield,
  Bot
} from '../components/Icons.jsx';

// ==================== 品牌图标与颜色配置 ====================
const ISSUER_COLORS = {
  github: '#6e40c9',
  gitlab: '#fc6d26',
  bitbucket: '#0052cc',
  discord: '#5865f2',
  twitter: '#1da1f2',
  'x.com': '#000000',
  facebook: '#1877f2',
  instagram: '#e4405f',
  linkedin: '#0a66c2',
  reddit: '#ff4500',
  telegram: '#26a5e4',
  whatsapp: '#25d366',
  slack: '#4a154b',
  twitch: '#9146ff',
  microsoft: '#00a4ef',
  google: '#4285f4',
  amazon: '#ff9900',
  apple: '#000000',
  meta: '#1877f2',
  cloudflare: '#f38020',
  vultr: '#007bfc',
  digitalocean: '#0080ff',
  linode: '#00a95c',
  heroku: '#430098',
  vercel: '#000000',
  netlify: '#00c7b7',
  railway: '#0b0d0e',
  render: '#46e3b7',
  dropbox: '#0061ff',
  steam: '#1b2838',
  epic: '#2f2d2e',
  playstation: '#003791',
  xbox: '#107c10',
  nintendo: '#e60012',
  blizzard: '#00ceff',
  paypal: '#003087',
  stripe: '#635bff',
  coinbase: '#0052ff',
  binance: '#f0b90b',
  tencent: '#1296db',
  huawei: '#cf0a2c',
  aliyun: '#ff6a00',
  alibaba: '#ff6a00',
};

const SIMPLE_ICONS = {
  github: 'si si-github',
  gitlab: 'si si-gitlab',
  bitbucket: 'si si-bitbucket',
  discord: 'si si-discord',
  twitter: 'si si-twitter',
  'x.com': 'si si-x',
  facebook: 'si si-facebook',
  instagram: 'si si-instagram',
  linkedin: 'si si-linkedin',
  reddit: 'si si-reddit',
  telegram: 'si si-telegram',
  whatsapp: 'si si-whatsapp',
  slack: 'si si-slack',
  twitch: 'si si-twitch',
  microsoft: 'fab fa-microsoft',
  google: 'si si-google',
  amazon: 'si si-amazon',
  apple: 'si si-apple',
  meta: 'si si-meta',
  cloudflare: 'si si-cloudflare',
  aws: 'si si-amazonaws',
  digitalocean: 'si si-digitalocean',
  vultr: 'si si-vultr',
  linode: 'si si-linode',
  heroku: 'si si-heroku',
  vercel: 'si si-vercel',
  netlify: 'si si-netlify',
  railway: 'si si-railway',
  render: 'si si-render',
  dropbox: 'si si-dropbox',
  drive: 'si si-googledrive',
  onedrive: 'si si-microsoftonedrive',
  backblaze: 'si si-backblaze',
  steam: 'si si-steam',
  epic: 'si si-epicgames',
  playstation: 'si si-playstation',
  xbox: 'si si-xbox',
  nintendo: 'si si-nintendo',
  blizzard: 'si si-blizzard',
  ubisoft: 'si si-ubisoft',
  paypal: 'si si-paypal',
  stripe: 'si si-stripe',
  coinbase: 'si si-coinbase',
  binance: 'si si-binance',
  npm: 'si si-npm',
  docker: 'si si-docker',
  wordpress: 'si si-wordpress',
  jira: 'si si-jira',
  trello: 'si si-trello',
  figma: 'si si-figma',
  notion: 'si si-notion',
  tencent: 'fab fa-qq',
  huawei: 'si si-huawei',
  aliyun: 'si si-alibabacloud',
  alibaba: 'si si-alibaba',
  baidu: 'si si-baidu',
  weixin: 'si si-wechat',
  wechat: 'si si-wechat',
  weibo: 'si si-sinaweibo',
  qq: 'fab fa-qq',
  bytedance: 'si si-bytedance',
  douyin: 'si si-tiktok',
  bilibili: 'si si-bilibili',
  spaceship: 'si si-spaceship',
  godaddy: 'si si-godaddy',
  namecheap: 'si si-namecheap',
  porkbun: 'si si-porkbun',
  bitwarden: 'si si-bitwarden',
  '1password': 'si si-1password',
  lastpass: 'si si-lastpass',
  proton: 'si si-proton',
  shopify: 'si si-shopify',
  spotify: 'si si-spotify',
  adobe: 'si si-adobe',
  zoom: 'si si-zoom',
  okta: 'si si-okta',
  auth0: 'si si-auth0',
  hetzner: 'si si-hetzner',
  ovh: 'si si-ovh',
  oracle: 'si si-oracle',
  sentry: 'si si-sentry',
  cloudways: 'si si-cloudways',
};

const FA_ICONS = {
  microsoft: 'fab fa-microsoft',
  github: 'fab fa-github',
  google: 'fab fa-google',
  amazon: 'fab fa-amazon',
  apple: 'fab fa-apple',
  dropbox: 'fab fa-dropbox',
  steam: 'fab fa-steam',
  playstation: 'fab fa-playstation',
  xbox: 'fab fa-xbox',
  paypal: 'fab fa-paypal',
  stripe: 'fab fa-stripe',
  docker: 'fab fa-docker',
  npm: 'fab fa-npm',
  discord: 'fab fa-discord',
  twitter: 'fab fa-twitter',
  facebook: 'fab fa-facebook',
  instagram: 'fab fa-instagram',
  linkedin: 'fab fa-linkedin',
  reddit: 'fab fa-reddit',
  telegram: 'fab fa-telegram',
  whatsapp: 'fab fa-whatsapp',
  slack: 'fab fa-slack',
  twitch: 'fab fa-twitch',
  spotify: 'fab fa-spotify',
  adobe: 'fab fa-adobe',
  cloudflare: 'fas fa-cloud',
  vultr: 'fas fa-server',
  digitalocean: 'fab fa-digital-ocean',
  linux: 'fab fa-linux',
  ubuntu: 'fab fa-ubuntu',
  windows: 'fab fa-windows',
  email: 'fas fa-envelope',
  mail: 'fas fa-envelope',
  bank: 'fas fa-university',
  crypto: 'fas fa-coins',
  vpn: 'fas fa-shield-alt',
  server: 'fas fa-server',
  hosting: 'fas fa-server',
  domain: 'fas fa-globe',
  ssh: 'fas fa-terminal',
  database: 'fas fa-database',
  storage: 'fas fa-hdd',
  cloud: 'fas fa-cloud',
  game: 'fas fa-gamepad',
  shop: 'fas fa-shopping-cart',
  store: 'fas fa-store',
  finance: 'fas fa-chart-line',
  trading: 'fas fa-chart-bar',
  exchange: 'fas fa-exchange-alt',
  wallet: 'fas fa-wallet',
  social: 'fas fa-users',
  chat: 'fas fa-comments',
  video: 'fas fa-video',
  music: 'fas fa-music',
  photo: 'fas fa-camera',
  code: 'fas fa-code',
  dev: 'fas fa-laptop-code',
};

const getIssuerColor = (issuer) => {
  const key = issuer?.toLowerCase() || '';
  for (const [name, color] of Object.entries(ISSUER_COLORS)) {
    if (key.includes(name)) return color;
  }
  return '#8b5cf6'; // Default purple
};

const getIssuerIcon = (issuer) => {
  const key = issuer?.toLowerCase() || '';
  for (const [name, icon] of Object.entries(SIMPLE_ICONS)) {
    if (key.includes(name)) return icon;
  }
  for (const [name, icon] of Object.entries(FA_ICONS)) {
    if (key.includes(name)) return icon;
  }
  return 'fas fa-shield-alt';
};

const maskEmail = (email) => {
  if (!email) return '';
  if (!email.includes('@')) return email;
  const [local, domain] = email.split('@');
  if (local.length <= 3) return local[0] + '***@' + domain;
  return local.slice(0, 2) + '***' + local.slice(-1) + '@' + domain;
};

// ==================== TotpPage 组件 ====================
function TotpPage() {
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
    maskAccount: false,
    hideCode: false,
    allowRevealCode: true,
    groupByPlatform: false,
    showPlatformHeaders: true,
    hidePlatformText: false,
    autoSave: false,
    lockInputMode: false,
    defaultInputMode: 'scan',
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
    color: '',
  });
  const [accountModalError, setAccountModalError] = useState('');
  const [totpShowSecret, setTotpShowSecret] = useState(false);
  const [importUris, setImportUris] = useState('');
  const [accountModalSaving, setAccountModalSaving] = useState(false);
  const [accountAddTab, setAccountAddTab] = useState('scan');

  // QR 扫码状态
  const [isScanning, setIsScanning] = useState(false);
  const [qrParsing, setQrParsing] = useState(false);
  const [qrError, setQrError] = useState('');
  const scannerRef = useRef(null);
  const fileInputRef = useRef(null);

  // Group Modal 状态
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [groupModalMode, setGroupModalMode] = useState('add');
  const [editingGroupId, setEditingGroupId] = useState(null);
  const [groupForm, setGroupForm] = useState({ name: '', color: '#8b5cf6' });

  // Export Modal 状态
  const [showExportModal, setShowExportModal] = useState(false);
  const [exportUris, setExportUris] = useState('');

  // Local card reveal state
  const [revealedCodes, setRevealedCodes] = useState({});

  // 获取请求 Headers
  const getAuthHeaders = () => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
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
        setTotpSettings((prev) => ({ ...prev, ...settingsData.data.totpSettings }));
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
  const saveSettingsToServer = async (newSettings) => {
    try {
      const headers = getAuthHeaders();
      const settingsRes = await fetch('/api/settings', { headers });
      const settingsResult = await settingsRes.json();
      
      let currentSettings = {};
      if (settingsResult.success && settingsResult.data) {
        currentSettings = settingsResult.data;
      }

      const payload = {
        ...currentSettings,
        totpSettings: newSettings,
      };

      await fetch('/api/settings', {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
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
    const timer = setInterval(() => {
      setTotpCodes((prevCodes) => {
        const updated = {};
        let needRefresh = false;
        let changed = false;
        for (const id in prevCodes) {
          const item = prevCodes[id];
          if (item.remaining !== undefined && item.remaining > 0) {
            const nextRemaining = item.remaining - 1;
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
      list = list.filter((a) => String(a.group_id) === String(totpFilterGroup));
    }
    if (totpSearchQuery.trim()) {
      const q = totpSearchQuery.toLowerCase();
      list = list.filter(
        (a) =>
          (a.issuer || '').toLowerCase().includes(q) ||
          (a.account || '').toLowerCase().includes(q)
      );
    }

    if (totpSettings.groupByPlatform) {
      list.sort((a, b) => (a.issuer || '').localeCompare(b.issuer || ''));
    }
    return list;
  }, [totpAccounts, totpFilterGroup, totpSearchQuery, totpSettings.groupByPlatform]);

  const platformCounts = useMemo(() => {
    const counts = {};
    totpAccounts.forEach((a) => {
      const key = (a.issuer || '').toLowerCase();
      counts[key] = (counts[key] || 0) + 1;
    });
    return counts;
  }, [totpAccounts]);

  const groupAccountCounts = useMemo(() => {
    const counts = {};
    totpAccounts.forEach((a) => {
      if (a.group_id) {
        counts[a.group_id] = (counts[a.group_id] || 0) + 1;
      }
    });
    return counts;
  }, [totpAccounts]);

  // ==================== 账号编辑与删除 ====================
  const handleOpenAddAccount = () => {
    const defaultMode = totpSettings.lockInputMode
      ? totpSettings.defaultInputMode
      : 'scan';

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
      color: '',
    });
    setAccountModalMode('add');
    setAccountModalError('');
    setTotpShowSecret(false);
    setImportUris('');
    setQrError('');
    setShowAccountModal(true);
  };

  const handleOpenEditAccount = async (account) => {
    setAccountModalMode('edit');
    setAccountAddTab('manual');
    setEditingAccountId(account.id);
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
      color: account.color || '',
    });
    setAccountModalError('');
    setTotpShowSecret(false);
    setShowAccountModal(true);
  };

  const toggleSecretVisibility = async () => {
    if (!totpShowSecret && accountModalMode === 'edit' && accountForm.secret.includes('•••')) {
      try {
        const res = await fetch(`/api/totp/accounts/${editingAccountId}?showSecret=true`, {
          headers: getAuthHeaders(),
        });
        const data = await res.json();
        if (data.success && data.data.secret) {
          setAccountForm((prev) => ({ ...prev, secret: data.data.secret }));
        } else {
          toast.error('获取密钥失败');
        }
      } catch (e) {
        console.error(e);
        toast.error('获取密钥失败');
      }
    }
    setTotpShowSecret(!totpShowSecret);
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
      const payload = {
        otp_type: accountForm.otp_type,
        issuer: accountForm.issuer.trim(),
        account: accountForm.account.trim(),
        algorithm: accountForm.algorithm,
        digits: Number(accountForm.digits),
        period: Number(accountForm.period),
        counter: Number(accountForm.counter),
        group_id: accountForm.group_id ? Number(accountForm.group_id) : null,
        color: accountForm.color || null,
      };

      if (accountModalMode === 'add') {
        payload.secret = accountForm.secret.replace(/\s/g, '');
      }

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

  const handleDeleteAccount = async (account) => {
    if (!confirm(`确定要删除 "${account.issuer}" 的账号吗？`)) {
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

  const incrementHotp = async (account) => {
    try {
      const res = await fetch(`/api/totp/accounts/${account.id}/increment`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (data.success) {
        setTotpCodes((prev) => ({
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
  const startQrScan = async () => {
    if (!window.Html5Qrcode) {
      toast.error('扫码库加载失败');
      return;
    }
    if (!window.isSecureContext && location.hostname !== 'localhost' && location.hostname !== '127.0.0.1') {
      setQrError('摄像头功能仅支持 HTTPS 环境。如果是移动端访问，请确认服务器域名已开启 SSL。');
      toast.warning('环境不受支持');
      return;
    }

    setIsScanning(true);
    setQrError('');

    setTimeout(async () => {
      try {
        const html5QrCode = new window.Html5Qrcode('qr-reader');
        scannerRef.current = html5QrCode;

        const config = {
          fps: 15,
          qrbox: { width: 250, height: 250 },
        };

        const successCallback = async (decodedText) => {
          if (decodedText.startsWith('otpauth://')) {
            if (window.navigator && window.navigator.vibrate) {
              window.navigator.vibrate(100);
            }
            await stopQrScan();

            if (totpSettings.autoSave) {
              await importUrisDirectly(decodedText);
            } else {
              setImportUris((prev) => (prev ? prev + '\n' + decodedText : decodedText));
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
              throw new Error('未检测到任何摄像头设备');
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
    }, 100);
  };

  const stopQrScan = async () => {
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

  const parseQrImage = async (blob) => {
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
          setImportUris((prev) => (prev ? prev + '\n' + uri : uri));
          toast.success('二维码解析成功');
        }
      } else {
        setQrError('无法识别二维码或二维码不是有效的 OTP URI');
      }
      URL.revokeObjectURL(img.src);
    } catch (e) {
      console.error(e);
      setQrError('二维码解析失败');
    } finally {
      setQrParsing(false);
    }
  };

  const handleQrPaste = async (e) => {
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

  const handleQrUpload = async (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    await parseQrImage(file);
    e.target.value = ''; // Reset input
  };

  const importUrisDirectly = async (urisText) => {
    const uris = urisText
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line.startsWith('otpauth://'));

    if (uris.length === 0) {
      toast.warning('没有找到有效的 URI');
      return;
    }

    try {
      const res = await fetch('/api/totp/import', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ uris }),
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
    setGroupForm({ name: '', color: '#8b5cf6' });
    setShowGroupModal(true);
  };

  const handleOpenEditGroup = (group) => {
    setGroupModalMode('edit');
    setEditingGroupId(group.id);
    setGroupForm({ name: group.name, color: group.color || '#8b5cf6' });
    setShowGroupModal(true);
  };

  const handleSaveGroup = async () => {
    if (!groupForm.name.trim()) {
      toast.warning('请输入分组名称');
      return;
    }

    try {
      const url =
        groupModalMode === 'add'
          ? '/api/totp/groups'
          : `/api/totp/groups/${editingGroupId}`;

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

  const handleDeleteGroup = async (group) => {
    if (!confirm(`确定要删除分组 "${group.name}" 吗？分组内的账号不会被删除。`)) {
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
        setExportUris(data.data.join('\n'));
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

  const copyCodeToClipboard = async (account) => {
    const code = totpCodes[account.id]?.code;
    if (!code) return;

    try {
      await navigator.clipboard.writeText(code);
      toast.success(`验证码已复制: ${code}`);
    } catch (e) {
      // Fallback
      const textarea = document.createElement('textarea');
      textarea.value = code;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      toast.success(`验证码已复制: ${code}`);
    }
  };

  // 同步配置到浏览器扩展
  const syncConfigToExtension = () => {
    const password = localStorage.getItem('admin_password') || '';
    const serverUrl = window.location.origin;

    window.postMessage(
      {
        type: 'API_MONITOR_SYNC_CONFIG',
        serverUrl: serverUrl,
        password: password,
      },
      '*'
    );

    const successHandler = (e) => {
      if (e.data && e.data.type === 'API_MONITOR_SYNC_SUCCESS') {
        toast.success('配置已成功同步到浏览器插件！');
        window.removeEventListener('message', successHandler);
      }
    };
    window.addEventListener('message', successHandler);
    setTimeout(() => {
      window.removeEventListener('message', successHandler);
    }, 3000);
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

  const handleCardMouseEnter = (accountId) => {
    if (totpSettings.allowRevealCode) {
      setRevealedCodes((prev) => ({ ...prev, [accountId]: true }));
    }
  };

  const handleCardMouseLeave = (accountId) => {
    if (totpSettings.allowRevealCode) {
      setRevealedCodes((prev) => ({ ...prev, [accountId]: false }));
    }
  };

  return (
    <div className="space-y-6">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className="flex flex-col md:flex-row justify-between items-start md:items-center border-b border-kumo-line pb-4 gap-4">
        <div className="flex border border-kumo-line rounded-lg p-0.5 bg-kumo-recessed select-none">
          <button
            onClick={() => setTotpCurrentTab('accounts')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              totpCurrentTab === 'accounts'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Key className="w-3.5 h-3.5" />
            <span>验证码</span>
          </button>
          <button
            onClick={() => setTotpCurrentTab('groups')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              totpCurrentTab === 'groups'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <FolderOpen className="w-3.5 h-3.5" />
            <span>分组</span>
          </button>
          <button
            onClick={() => setTotpCurrentTab('settings')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              totpCurrentTab === 'settings'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Settings className="w-3.5 h-3.5" />
            <span>设置</span>
          </button>
        </div>

        {totpCurrentTab === 'accounts' && (
          <div className="flex items-center gap-2 w-full md:w-auto">
            <select
              value={totpFilterGroup}
              onChange={(e) => setTotpFilterGroup(e.target.value)}
              className="bg-kumo-base text-kumo-strong border border-kumo-line rounded-md text-xs px-2.5 py-1.5 focus:outline-none focus:ring-1 focus:ring-kumo-brand"
            >
              <option value="">全部分组</option>
              {totpGroups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>

            <div className="relative flex-1 md:w-48">
              <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-kumo-subtle">
                <Search className="w-3.5 h-3.5" />
              </span>
              <input
                type="text"
                placeholder="搜索账号..."
                value={totpSearchQuery}
                onChange={(e) => setTotpSearchQuery(e.target.value)}
                className="w-full bg-kumo-base text-kumo-strong border border-kumo-line rounded-md text-xs pl-8 pr-3 py-1.5 focus:outline-none focus:ring-1 focus:ring-kumo-brand"
              />
            </div>

            <Button variant="primary" icon={<Plus className="w-4 h-4" />} onClick={handleOpenAddAccount}>
              添加账号
            </Button>
          </div>
        )}

        {totpCurrentTab === 'groups' && (
          <Button variant="primary" icon={<Plus className="w-4 h-4" />} onClick={handleOpenAddGroup}>
            新建分组
          </Button>
        )}
      </div>

      {/* ==================== 1. 验证码卡片列表 ==================== */}
      {totpCurrentTab === 'accounts' && (
        <div className="quick-fade-in">
          {totpLoading ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle">
              <RefreshCw className="w-8 h-8 animate-spin text-kumo-brand mb-4" />
              <span>数据加载中...</span>
            </div>
          ) : filteredAccounts.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle border border-dashed border-kumo-line rounded-xl bg-kumo-recessed/10">
              <Shield className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">
                {totpSearchQuery ? '没有找到匹配的账号' : '暂无 2FA 账号，快来添加第一个吧'}
              </div>
              {!totpSearchQuery && (
                <Button variant="primary" className="mt-4" onClick={handleOpenAddAccount}>
                  添加第一个账号
                </Button>
              )}
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
              {filteredAccounts.map((account, index) => {
                const isFirstOfPlatform =
                  index === 0 ||
                  (account.issuer || '').toLowerCase() !==
                    (filteredAccounts[index - 1].issuer || '').toLowerCase();

                const issuerColor = account.color || getIssuerColor(account.issuer);
                const codeDetail = totpCodes[account.id] || {};
                const remaining = codeDetail.remaining ?? 30;
                const period = account.period || 30;
                const ratio = Math.max(0, Math.min(100, (remaining / period) * 100));
                
                // Show platform header if settings enable it
                const showHeader =
                  totpSettings.groupByPlatform &&
                  totpSettings.showPlatformHeaders &&
                  isFirstOfPlatform;

                return (
                  <React.Fragment key={account.id}>
                    {showHeader && (
                      <div className="col-span-full border-b border-kumo-line pb-1.5 mt-4 flex items-center justify-between">
                        {!totpSettings.hidePlatformText ? (
                          <div className="flex items-center gap-2">
                            <span
                              className="w-5 h-5 flex items-center justify-center rounded text-sm bg-kumo-recessed"
                              style={{ color: getIssuerColor(account.issuer) }}
                            >
                              <i className={getIssuerIcon(account.issuer)} />
                            </span>
                            <span className="text-sm font-bold text-kumo-strong">
                              {account.issuer || '未知平台'}
                            </span>
                            <span className="text-[10px] text-kumo-subtle bg-kumo-recessed px-1.5 py-0.5 rounded border border-kumo-line ml-1 font-medium">
                              {platformCounts[(account.issuer || '').toLowerCase()]} 个账号
                            </span>
                          </div>
                        ) : (
                          <div className="flex items-center gap-2">
                            <span className="w-2.5 h-2.5 rounded-full" style={{ background: getIssuerColor(account.issuer) }} />
                            <span className="text-xs text-kumo-subtle font-medium">
                              {platformCounts[(account.issuer || '').toLowerCase()]} 个账号
                            </span>
                          </div>
                        )}
                      </div>
                    )}

                    <div
                      onMouseEnter={() => handleCardMouseEnter(account.id)}
                      onMouseLeave={() => handleCardMouseLeave(account.id)}
                      onClick={() => copyCodeToClipboard(account)}
                      style={{ '--card-accent': issuerColor }}
                      className="group/card relative flex flex-col justify-between bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all min-h-[148px]"
                    >
                      {/* Action buttons (overlay/hover) */}
                      <div className="absolute right-3 top-3 opacity-0 group-hover/card:opacity-100 flex items-center gap-1.5 transition-opacity bg-kumo-base pl-2 py-0.5 rounded-md z-10">
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleOpenEditAccount(account);
                          }}
                          className="w-6 h-6 flex items-center justify-center rounded hover:bg-kumo-recessed text-kumo-subtle hover:text-kumo-strong"
                          title="编辑"
                        >
                          <Edit className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleDeleteAccount(account);
                          }}
                          className="w-6 h-6 flex items-center justify-center rounded hover:bg-kumo-danger/10 text-kumo-subtle hover:text-kumo-danger"
                          title="删除"
                        >
                          <Trash className="w-3.5 h-3.5" />
                        </button>
                      </div>

                      {/* Header */}
                      <div className="flex items-center gap-3">
                        <div
                          className="w-8 h-8 rounded-lg flex items-center justify-center text-white text-base flex-shrink-0"
                          style={{ background: getIssuerColor(account.issuer) }}
                        >
                          <i className={getIssuerIcon(account.issuer)} />
                        </div>
                        <div className="min-w-0">
                          <h4 className="text-xs font-bold text-kumo-strong truncate">
                            {account.issuer}
                          </h4>
                          <p className="text-[10px] text-kumo-subtle truncate max-w-[160px]">
                            {totpSettings.maskAccount ? maskEmail(account.account) : account.account}
                          </p>
                        </div>
                      </div>

                      {/* Code Area */}
                      <div className="mt-4 flex flex-col justify-end">
                        <div
                          className={`text-2xl font-bold tracking-wide select-all font-mono ${
                            remaining <= 5 ? 'text-kumo-danger animate-pulse' : 'text-kumo-strong'
                          }`}
                        >
                          {formatTotpCode(account, codeDetail.code)}
                        </div>

                        <div className="mt-2.5 flex items-center justify-between text-[10px] text-kumo-subtle font-mono">
                          {account.otp_type === 'hotp' ? (
                            <div className="flex items-center gap-1.5 w-full justify-between">
                              <span>#{codeDetail.counter || 0}</span>
                              <button
                                onClick={(e) => {
                                  e.stopPropagation();
                                  incrementHotp(account);
                                }}
                                className="px-2 py-0.5 border border-kumo-line rounded hover:bg-kumo-recessed text-kumo-strong cursor-pointer flex items-center gap-1"
                              >
                                <RefreshCw className="w-3 h-3" />
                                <span>递增</span>
                              </button>
                            </div>
                          ) : (
                            <div className="flex items-center gap-2 w-full">
                              <div className="flex-1 h-1.5 bg-kumo-recessed rounded-full overflow-hidden">
                                <div
                                  className={`h-full rounded-full ${remaining === period ? '' : 'transition-all duration-1000 ease-linear'}`}
                                  style={{
                                    width: `${ratio}%`,
                                    background: issuerColor,
                                  }}
                                />
                              </div>
                              <span className="w-6 text-right select-none">{remaining}s</span>
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  </React.Fragment>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== 2. 分组列表 ==================== */}
      {totpCurrentTab === 'groups' && (
        <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden quick-fade-in">
          {totpGroups.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle">
              <FolderOpen className="w-12 h-12 opacity-30 mb-4" />
              <span>暂无分组，创建一个吧</span>
              <Button variant="primary" className="mt-4" onClick={handleOpenAddGroup}>
                创建分组
              </Button>
            </div>
          ) : (
            <div className="overflow-x-auto w-full">
              <table className="w-full text-sm border-collapse">
                <thead>
                  <tr className="bg-kumo-recessed/40 border-b border-kumo-line">
                    <th className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider text-left py-3.5 px-6 w-20">
                      颜色
                    </th>
                    <th className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider text-left py-3.5 px-6">
                      分组名称
                    </th>
                    <th className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider text-center py-3.5 px-6 w-28">
                      账号数
                    </th>
                    <th className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider text-center py-3.5 px-6 w-36">
                      操作
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {totpGroups.map((group) => (
                    <tr key={group.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors">
                      <td className="py-3 px-6">
                        <div
                          className="w-4 h-4 rounded-full border border-black/10"
                          style={{ background: group.color || '#8b5cf6' }}
                        />
                      </td>
                      <td className="py-3 px-6 font-semibold text-kumo-strong">
                        {group.name}
                      </td>
                      <td className="py-3 px-6 text-center tabular-nums text-kumo-default">
                        {groupAccountCounts[group.id] || 0}
                      </td>
                      <td className="py-3 px-6 text-center">
                        <div className="flex items-center justify-center gap-2">
                          <button
                            onClick={() => handleOpenEditGroup(group)}
                            className="w-7 h-7 flex items-center justify-center rounded hover:bg-kumo-recessed text-kumo-subtle hover:text-kumo-strong cursor-pointer"
                            title="编辑"
                          >
                            <Edit className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => handleDeleteGroup(group)}
                            className="w-7 h-7 flex items-center justify-center rounded hover:bg-kumo-danger/10 text-kumo-danger cursor-pointer"
                            title="删除"
                          >
                            <Trash className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* ==================== 3. 选项设置 ==================== */}
      {totpCurrentTab === 'settings' && (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 quick-fade-in">
          {/* Settings Options (Span 2) */}
          <div className="lg:col-span-2 bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 space-y-6">
            <h3 className="text-sm font-semibold text-kumo-strong border-b border-kumo-line pb-3 select-none">
              安全与显示配置
            </h3>

            {/* Toggle 1: maskAccount */}
            <div className="flex items-start justify-between">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">账号名称打码</h4>
                <p className="text-[10px] text-kumo-subtle">
                  对邮箱或长账号名称进行脱敏隐藏保护，避免屏幕泄露。
                </p>
              </div>
              <input
                type="checkbox"
                checked={totpSettings.maskAccount}
                onChange={(e) => updateSetting('maskAccount', e.target.checked)}
                className="w-9 h-5 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-4 before:h-4 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            {/* Toggle 2: hideCode */}
            <div className="flex items-start justify-between border-t border-kumo-line pt-4">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">遮挡实时验证码</h4>
                <p className="text-[10px] text-kumo-subtle">
                  隐藏验证码数值，仅在悬浮或点击复制时显示，防止身旁窥屏。
                </p>
              </div>
              <input
                type="checkbox"
                checked={totpSettings.hideCode}
                onChange={(e) => updateSetting('hideCode', e.target.checked)}
                className="w-9 h-5 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-4 before:h-4 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            {/* Toggle 3: groupByPlatform */}
            <div className="flex items-start justify-between border-t border-kumo-line pt-4">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">按服务商/平台归类</h4>
                <p className="text-[10px] text-kumo-subtle">
                  将相同发行商（如 Google, GitHub）下的账号汇聚在一起分组显示。
                </p>
              </div>
              <input
                type="checkbox"
                checked={totpSettings.groupByPlatform}
                onChange={(e) => updateSetting('groupByPlatform', e.target.checked)}
                className="w-9 h-5 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-4 before:h-4 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            {/* Toggle 4: autoSave */}
            <div className="flex items-start justify-between border-t border-kumo-line pt-4">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">解析二维码后自动导入</h4>
                <p className="text-[10px] text-kumo-subtle">
                  扫码或选取二维码图片后自动读取数据入库，不需要手动核对表单保存。
                </p>
              </div>
              <input
                type="checkbox"
                checked={totpSettings.autoSave}
                onChange={(e) => updateSetting('autoSave', e.target.checked)}
                className="w-9 h-5 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-4 before:h-4 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            {/* Toggle 5: lockInputMode */}
            <div className="flex items-start justify-between border-t border-kumo-line pt-4">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">锁定默认录入类型</h4>
                <p className="text-[10px] text-kumo-subtle">
                  开启后添加账号弹窗默认直接使用锁定的选项，不用每次手动选。
                </p>
              </div>
              <input
                type="checkbox"
                checked={totpSettings.lockInputMode}
                onChange={(e) => updateSetting('lockInputMode', e.target.checked)}
                className="w-9 h-5 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-4 before:h-4 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
              />
            </div>

            {totpSettings.lockInputMode && (
              <div className="pl-4 pt-3 flex items-center justify-between">
                <label className="text-xs font-medium text-kumo-subtle">默认录入模式</label>
                <select
                  value={totpSettings.defaultInputMode}
                  onChange={(e) => updateSetting('defaultInputMode', e.target.value)}
                  className="bg-kumo-base text-kumo-strong border border-kumo-line rounded-md text-xs px-2 py-1.5 focus:outline-none focus:ring-1 focus:ring-kumo-brand"
                >
                  <option value="scan">扫描二维码</option>
                  <option value="upload">上传二维码</option>
                  <option value="manual">手动录入表单</option>
                </select>
              </div>
            )}

            <div className="border-t border-kumo-line pt-5 flex items-center gap-3">
              <Button onClick={() => importUrisDirectly(prompt('请输入批量导入的 otpauth:// 链接列表 (每行一条)') || '')}>
                批量导入 URI
              </Button>
              <Button onClick={handleExportAccounts}>批量导出备份</Button>
              <Button onClick={refreshCodes} icon={<RotateCw className="w-3.5 h-3.5" />}>
                手动刷新验证码
              </Button>
            </div>
          </div>

          {/* Right Column: Browser Extension Helper Card */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 flex flex-col justify-between">
            <div className="space-y-4">
              <h3 className="text-sm font-semibold text-kumo-strong border-b border-kumo-line pb-3 select-none flex items-center gap-2">
                <Bot className="w-4 h-4 text-kumo-brand" />
                浏览器插件助手
              </h3>
              <p className="text-xs text-kumo-subtle leading-relaxed">
                下载安装 2FA 浏览器插件，在 PC 端登录账号需要验证码时可一键实现自动检索与快捷填充。
              </p>

              <div className="p-4 bg-kumo-recessed/60 border border-kumo-line rounded-lg flex items-start gap-3 mt-4">
                <div className="w-10 h-10 rounded-md bg-white flex items-center justify-center shadow-sm flex-shrink-0">
                  <img src="https://cdn.simpleicons.org/blueprint" className="w-7 h-7" alt="Extension" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong">API Monitor 2FA 助手</h4>
                  <p className="text-[10px] text-kumo-subtle mt-0.5">本地免登录实时一键同步</p>
                </div>
              </div>
            </div>

            <div className="space-y-2 mt-6">
              <a
                href="/api/totp/extension/download"
                className="w-full flex items-center justify-center gap-2 h-9 border border-kumo-line rounded-lg text-xs font-semibold text-kumo-strong hover:bg-kumo-recessed text-decoration-none transition-colors"
              >
                <Download className="w-3.5 h-3.5" />
                <span>下载插件 ZIP 包</span>
              </a>

              <Button variant="primary" className="w-full" onClick={syncConfigToExtension}>
                一键同步密码与地址到插件
              </Button>

              <Button
                variant="ghost"
                className="w-full text-xs text-kumo-subtle hover:text-kumo-strong"
                onClick={() => setShowExtensionGuide(!showExtensionGuide)}
              >
                {showExtensionGuide ? '关闭教程' : '查看安装教程'}
              </Button>

              {showExtensionGuide && (
                <div className="text-[11px] text-kumo-subtle space-y-2 mt-4 p-3 bg-kumo-recessed/50 rounded-lg border border-kumo-line">
                  <p className="font-bold text-kumo-strong">三步完成安装：</p>
                  <ol className="list-decimal pl-4 space-y-1">
                    <li>解压下载的 ZIP 压缩包至本地固定目录；</li>
                    <li>
                      打开 Chrome，访问 <code>chrome://extensions</code> 并开启右上角的
                      <strong>开发者模式</strong>；
                    </li>
                    <li>
                      点击<strong>加载已解压的扩展程序</strong>，选择刚才解压的目录文件夹。
                    </li>
                  </ol>
                  <div className="bg-kumo-brand/10 text-kumo-brand p-2 rounded border border-kumo-brand/20 mt-1 font-medium select-all">
                    配置插件地址: {window.location.origin}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ==================== 模态框 1: 账号添加/修改 ==================== */}
      <Dialog.Root open={showAccountModal} onOpenChange={setShowAccountModal}>
        <Dialog className="p-6 sm:max-w-lg">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1">
            {accountModalMode === 'add' ? '添加 / 导入 2FA 账号' : '编辑 2FA 账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            {accountModalMode === 'add' ? '扫码或手动填表记录新的动态验证码' : '修改现有 2FA 令牌的标签或分组配置'}
          </Dialog.Description>

          {accountModalMode === 'add' && (
            <div className="flex border-b border-kumo-line mb-4 select-none">
              <button
                onClick={() => {
                  stopQrScan();
                  setAccountAddTab('scan');
                }}
                className={`pb-2 px-4 text-xs font-semibold border-b-2 cursor-pointer transition-colors ${
                  accountAddTab === 'scan'
                    ? 'border-kumo-brand text-kumo-strong'
                    : 'border-transparent text-kumo-subtle hover:text-kumo-strong'
                }`}
              >
                扫码导入
              </button>
              <button
                onClick={() => {
                  stopQrScan();
                  setAccountAddTab('manual');
                }}
                className={`pb-2 px-4 text-xs font-semibold border-b-2 cursor-pointer transition-colors ${
                  accountAddTab === 'manual'
                    ? 'border-kumo-brand text-kumo-strong'
                    : 'border-transparent text-kumo-subtle hover:text-kumo-strong'
                }`}
              >
                手动录入
              </button>
            </div>
          )}

          {/* Form Content */}
          <div className="space-y-4 max-h-[50vh] overflow-y-auto pr-1 scrollbar-thin">
            {accountModalMode === 'add' && accountAddTab === 'scan' ? (
              <div className="space-y-4">
                <div className="flex gap-2 items-center">
                  <Button
                    onClick={isScanning ? stopQrScan : startQrScan}
                    variant={isScanning ? 'destructive' : 'secondary'}
                  >
                    {isScanning ? '停止摄像头' : '开启摄像头扫码'}
                  </Button>
                  <Button onClick={() => fileInputRef.current?.click()} icon={<Upload className="w-3.5 h-3.5" />}>
                    上传二维码图片
                  </Button>
                  <input
                    type="file"
                    ref={fileInputRef}
                    accept="image/*"
                    onChange={handleQrUpload}
                    className="hidden"
                  />
                </div>

                {isScanning && (
                  <div
                    id="qr-reader"
                    className="w-full aspect-square max-w-[280px] mx-auto rounded-xl overflow-hidden border border-kumo-line bg-black"
                  />
                )}

                {!isScanning && (
                  <div
                    onPaste={handleQrPaste}
                    tabIndex={0}
                    className="w-full py-10 border border-dashed border-kumo-line rounded-lg bg-kumo-recessed/10 flex flex-col items-center justify-center text-kumo-subtle cursor-pointer focus:border-kumo-brand focus:outline-none group"
                  >
                    {qrParsing ? (
                      <span className="flex items-center gap-2">
                        <RefreshCw className="w-4 h-4 animate-spin" />
                        <span>解析中...</span>
                      </span>
                    ) : (
                      <>
                        <Upload className="w-6 h-6 mb-2 opacity-50 group-hover:scale-105 transition-transform" />
                        <span className="text-xs">Ctrl+V 粘贴二维码图片 或 拖拽图片至此</span>
                      </>
                    )}
                  </div>
                )}

                {qrError && (
                  <div className="p-3 bg-kumo-danger/10 border border-kumo-danger/20 text-kumo-danger text-xs rounded-md">
                    {qrError}
                  </div>
                )}

                <div className="space-y-1.5 pt-2">
                  <label className="text-xs font-semibold text-kumo-subtle">
                    批量 OTP Auth URIs 导入 (每行一条)
                  </label>
                  <textarea
                    rows={4}
                    placeholder="otpauth://totp/GitHub:user@example.com?secret=XXXX..."
                    value={importUris}
                    onChange={(e) => setImportUris(e.target.value)}
                    className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand font-mono"
                  />
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                {/* OTP Type */}
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">验证码类型</label>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      onClick={() => setAccountForm((prev) => ({ ...prev, otp_type: 'totp' }))}
                      className={`flex-1 py-1.5 rounded border text-xs font-semibold transition-colors cursor-pointer ${
                        accountForm.otp_type === 'totp'
                          ? 'border-kumo-brand bg-kumo-brand/10 text-kumo-brand'
                          : 'border-kumo-line bg-kumo-recessed text-kumo-subtle'
                      }`}
                    >
                      TOTP (基于时间)
                    </button>
                    <button
                      type="button"
                      onClick={() => setAccountForm((prev) => ({ ...prev, otp_type: 'hotp' }))}
                      className={`flex-1 py-1.5 rounded border text-xs font-semibold transition-colors cursor-pointer ${
                        accountForm.otp_type === 'hotp'
                          ? 'border-kumo-brand bg-kumo-brand/10 text-kumo-brand'
                          : 'border-kumo-line bg-kumo-recessed text-kumo-subtle'
                      }`}
                    >
                      HOTP (基于计数)
                    </button>
                  </div>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">发行商 / 服务商</label>
                  <input
                    type="text"
                    placeholder="如: GitHub, Microsoft"
                    value={accountForm.issuer}
                    onChange={(e) => setAccountForm((prev) => ({ ...prev, issuer: e.target.value }))}
                    className="w-full bg-kumo-recessed text-kumo-strong text-sm px-3 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">账户名 / 标识</label>
                  <input
                    type="text"
                    placeholder="如: user@example.com"
                    value={accountForm.account}
                    onChange={(e) => setAccountForm((prev) => ({ ...prev, account: e.target.value }))}
                    className="w-full bg-kumo-recessed text-kumo-strong text-sm px-3 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">密钥 (Base32)</label>
                  <div className="relative">
                    <input
                      type={totpShowSecret ? 'text' : 'password'}
                      placeholder="JBSWY3DPEHPK3PXP"
                      disabled={accountModalMode === 'edit'}
                      value={accountForm.secret}
                      onChange={(e) => setAccountForm((prev) => ({ ...prev, secret: e.target.value }))}
                      className="w-full bg-kumo-recessed text-kumo-strong text-sm pl-3 pr-10 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand disabled:opacity-60"
                    />
                    <button
                      type="button"
                      onClick={toggleSecretVisibility}
                      className="absolute right-3 top-1/2 -translate-y-1/2 text-kumo-subtle hover:text-kumo-strong cursor-pointer"
                    >
                      {totpShowSecret ? '隐藏' : '显示'}
                    </button>
                  </div>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">关联分组</label>
                  <select
                    value={accountForm.group_id}
                    onChange={(e) => setAccountForm((prev) => ({ ...prev, group_id: e.target.value }))}
                    className="w-full bg-kumo-recessed text-kumo-strong text-sm px-3 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
                  >
                    <option value="">无分组</option>
                    {totpGroups.map((g) => (
                      <option key={g.id} value={g.id}>
                        {g.name}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Advanced parameters */}
                <details className="text-xs text-kumo-subtle cursor-pointer select-none">
                  <summary className="font-semibold text-kumo-strong hover:text-kumo-brand py-1">
                    高级设置选项
                  </summary>
                  <div className="pt-3 grid grid-cols-3 gap-3">
                    <div className="space-y-1">
                      <label className="font-semibold">加密算法</label>
                      <select
                        value={accountForm.algorithm}
                        onChange={(e) => setAccountForm((prev) => ({ ...prev, algorithm: e.target.value }))}
                        className="w-full bg-kumo-recessed text-kumo-strong border border-kumo-line rounded p-1 focus:outline-none"
                      >
                        <option value="SHA1">SHA1</option>
                        <option value="SHA256">SHA256</option>
                        <option value="SHA512">SHA512</option>
                      </select>
                    </div>

                    <div className="space-y-1">
                      <label className="font-semibold">码位长度</label>
                      <select
                        value={accountForm.digits}
                        onChange={(e) => setAccountForm((prev) => ({ ...prev, digits: e.target.value }))}
                        className="w-full bg-kumo-recessed text-kumo-strong border border-kumo-line rounded p-1 focus:outline-none"
                      >
                        <option value="6">6 位</option>
                        <option value="8">8 位</option>
                      </select>
                    </div>

                    {accountForm.otp_type === 'totp' ? (
                      <div className="space-y-1">
                        <label className="font-semibold">周期数 (s)</label>
                        <select
                          value={accountForm.period}
                          onChange={(e) => setAccountForm((prev) => ({ ...prev, period: e.target.value }))}
                          className="w-full bg-kumo-recessed text-kumo-strong border border-kumo-line rounded p-1 focus:outline-none"
                        >
                          <option value="30">30 秒</option>
                          <option value="60">60 秒</option>
                        </select>
                      </div>
                    ) : (
                      <div className="space-y-1">
                        <label className="font-semibold">计数起始</label>
                        <input
                          type="number"
                          value={accountForm.counter}
                          onChange={(e) => setAccountForm((prev) => ({ ...prev, counter: e.target.value }))}
                          className="w-full bg-kumo-recessed text-kumo-strong border border-kumo-line rounded p-1 focus:outline-none font-mono"
                        />
                      </div>
                    )}

                    <div className="col-span-full pt-2 flex items-center gap-2">
                      <label className="font-semibold whitespace-nowrap">自定义色值:</label>
                      <input
                        type="color"
                        value={accountForm.color || '#8b5cf6'}
                        onChange={(e) => setAccountForm((prev) => ({ ...prev, color: e.target.value }))}
                        className="w-8 h-8 rounded-md border border-kumo-line cursor-pointer"
                      />
                      <Button size="sm" onClick={() => setAccountForm((prev) => ({ ...prev, color: '' }))}>
                        使用平台默认
                      </Button>
                    </div>
                  </div>
                </details>
              </div>
            )}
          </div>

          {accountModalError && (
            <div className="mt-4 p-3 bg-kumo-danger/10 border border-kumo-danger/20 text-kumo-danger text-xs rounded-md">
              {accountModalError}
            </div>
          )}

          <div className="flex justify-end gap-3 mt-6">
            <Dialog.Close>
              <Button onClick={() => stopQrScan()}>取消</Button>
            </Dialog.Close>

            {accountModalMode === 'add' && accountAddTab === 'scan' ? (
              <Button
                variant="primary"
                onClick={() => importUrisDirectly(importUris)}
                disabled={!importUris.trim()}
              >
                执行导入
              </Button>
            ) : (
              <Button variant="primary" onClick={handleSaveAccount} loading={accountModalSaving}>
                保存账号
              </Button>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框 2: 新建/编辑分组 ==================== */}
      <Dialog.Root open={showGroupModal} onOpenChange={setShowGroupModal}>
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1">
            {groupModalMode === 'add' ? '创建新分组' : '编辑分组属性'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            设置分组的名称与卡片主题色值
          </Dialog.Description>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">分组名称</label>
              <input
                type="text"
                placeholder="如: 财务, 工作, 个人"
                value={groupForm.name}
                onChange={(e) => setGroupForm((prev) => ({ ...prev, name: e.target.value }))}
                className="w-full bg-kumo-recessed text-kumo-strong text-sm px-3 py-2 border border-kumo-line rounded-md focus:outline-none focus:border-kumo-brand"
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">卡片标识色值</label>
              <div className="flex items-center gap-3">
                <input
                  type="color"
                  value={groupForm.color}
                  onChange={(e) => setGroupForm((prev) => ({ ...prev, color: e.target.value }))}
                  className="w-10 h-10 rounded-md border border-kumo-line cursor-pointer"
                />
                <span className="text-xs font-mono text-kumo-subtle">{groupForm.color}</span>
              </div>
            </div>
          </div>

          <div className="flex justify-end gap-3 mt-6">
            <Dialog.Close>
              <Button>取消</Button>
            </Dialog.Close>
            <Button variant="primary" onClick={handleSaveGroup}>
              保存分组
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框 3: 备份导出账号 ==================== */}
      <Dialog.Root open={showExportModal} onOpenChange={setShowExportModal}>
        <Dialog className="p-6 sm:max-w-lg">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1">
            备份与导出
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            已成功生成标准 `otpauth://` 协议格式的 URI 列表，请复制妥善保管。
          </Dialog.Description>

          <div className="space-y-1.5">
            <textarea
              readOnly
              rows={8}
              value={exportUris}
              className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-md focus:outline-none font-mono"
            />
            <span className="text-[10px] text-kumo-subtle block">
              警告：密钥是明文显示，请务必保证导出环境的安全性。
            </span>
          </div>

          <div className="flex justify-end gap-3 mt-6">
            <Dialog.Close>
              <Button>关闭</Button>
            </Dialog.Close>
            <Button variant="primary" onClick={copyExportedUris}>
              复制到剪贴板
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default TotpPage;
