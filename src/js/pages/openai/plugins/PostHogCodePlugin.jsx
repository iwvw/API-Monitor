import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, LayerCard, Input, Badge, Table, Meter, Select, Toolbar } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState, AppTable } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, PostHogBrand, Settings as SettingsIcon, Plus, RefreshCw, Trash, ExternalLink, ShieldCheck, TrendingUp, Upload, Download, Copy } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/posthogcode';

const POSTHOG_ACCOUNT_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg' },
];

const POSTHOG_USAGE_COLUMNS = [
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'remaining', role: 'content', grow: 2, verticalAlign: 'middle' },
  { id: 'used', role: 'count', align: 'right' },
  { id: 'burst', role: 'count', align: 'center' },
  { id: 'sustained', role: 'count', align: 'center' },
  { id: 'period', role: 'date', align: 'center' },
];

const POSTHOG_MODEL_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'source', role: 'type' },
  { id: 'context', role: 'count', align: 'center' },
  { id: 'multiplier', role: 'count', align: 'center' },
  { id: 'plan', role: 'status' },
];

// 回调地址由后端固定为 PostHog OAuth 应用注册过的值（默认 http://localhost/callback）。
// 不能按面板所在源推导：注册值与部署域名无关，用 window.location.origin 会得到
// "Mismatching redirect URI"。这里只用于提示占位，实际值取后端下发的 redirectUri。
const DEFAULT_CALLBACK_URI = 'http://localhost/callback';

const fmtLeft = seconds => {
  const s = Number(seconds) || 0;
  if (s <= 0) return '';
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  if (d > 0) return `${d}天${h}小时`;
  const m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}小时${m}分` : `${Math.max(1, m)}分`;
};

// PostHogCodePlugin：模型网关「插件中心」卡片——PostHog LLM Gateway 转 OpenAI 兼容 API。
// 鉴权走 PostHog OAuth 2.0 PKCE（llm_gateway:read 是 privileged scope，个人 API Key 拿不到），
// 插件内嵌授权码流程与 refresh token 自动轮换。
export function PostHogCodePlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [linkState, setLinkState] = useState(null);
  const [models, setModels] = useState([]);
  const [usage, setUsage] = useState(null);
  const [usageRows, setUsageRows] = useState([]);
  const [usageTotal, setUsageTotal] = useState(null);
  const [usdPerCredit, setUsdPerCredit] = useState(0.01);
  const [usageLoading, setUsageLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [linkBusy, setLinkBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [testingId, setTestingId] = useState('');
  const [prefixDraft, setPrefixDraft] = useState(null);
  const [authOpen, setAuthOpen] = useState(false);
  const [authUrl, setAuthUrl] = useState('');
  const [authState, setAuthState] = useState('');
  // authRedirectUri 是后端下发的权威回调地址（OAuth 应用注册值），
  // 仅用于占位提示，交换 token 时由后端从会话取用，前端不参与。
  const [authRedirectUri, setAuthRedirectUri] = useState(DEFAULT_CALLBACK_URI);
  const [authRegion, setAuthRegion] = useState('us');
  const [authAccountId, setAuthAccountId] = useState('');
  const [authStarting, setAuthStarting] = useState(false);
  const [callbackUrl, setCallbackUrl] = useState('');
  const [exchanging, setExchanging] = useState(false);
  // 自动登录（账号密码）：对话框状态机
  const [autoLoginOpen, setAutoLoginOpen] = useState(false);
  const [autoLoginRegion, setAutoLoginRegion] = useState('us');
  const [autoLoginEmail, setAutoLoginEmail] = useState('');
  const [autoLoginPassword, setAutoLoginPassword] = useState('');
  const [autoLoginAccountId, setAutoLoginAccountId] = useState('');
  const [autoLoginSessionId, setAutoLoginSessionId] = useState('');
  const [autoLoginPhase, setAutoLoginPhase] = useState('form'); // form | code | totp | done
  const [autoLoginCode, setAutoLoginCode] = useState('');
  const [autoLoginBusy, setAutoLoginBusy] = useState(false);
  const [autoLoginDetail, setAutoLoginDetail] = useState('');

  // 一键注册：生成收件邮箱 → 注册 → 自动邮箱验证 → 登录授权，全程后台跑、前端轮询进度。
  const [signupOpen, setSignupOpen] = useState(false);
  const [signupRegion, setSignupRegion] = useState('us');
  const [signupPrefix, setSignupPrefix] = useState('probe');
  const [signupPassword, setSignupPassword] = useState('');
  const [signupDomain, setSignupDomain] = useState('');
  const [signupDomains, setSignupDomains] = useState([]);
  const [signupSessionId, setSignupSessionId] = useState('');
  const [signupStatus, setSignupStatus] = useState('');
  const [signupSteps, setSignupSteps] = useState([]);
  const [signupEmail, setSignupEmail] = useState('');
  const [signupError, setSignupError] = useState('');
  const [signupBusy, setSignupBusy] = useState(false);
  const [signupMode, setSignupMode] = useState('auto'); // auto | manual
  const [signupReturned, setSignupReturned] = useState({ email: '', password: '', signupUrl: '' });
  const [proxyPools, setProxyPools] = useState([]);
  const [signupProxy, setSignupProxy] = useState('');
  // 注册对话框内可临时覆盖出口代理：空串表示沿用插件设置里的默认值。
  const [signupProxyOverride, setSignupProxyOverride] = useState('');

  // 代理池列表：注册与自动登录可借它换出口 IP，绕过 PostHog 的注册限流。
  const loadProxyPools = async () => {
    try {
      const res = await fetch('/api/proxypool', { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setProxyPools(data.pools || []);
    } catch {
      setProxyPools([]);
    }
  };
  const fileInputRef = useRef(null);

  const load = async () => {
    try {
      const res = await fetch(`${API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      setAccounts(data.settings?.accounts || []);
    } catch (e) {
      toast.error(`插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  const loadStatus = async () => {
    try {
      const res = await fetch(`${API}/status`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setStatus(data);
    } catch {
      setStatus(null);
    }
  };

  const loadLink = async () => {
    try {
      const res = await fetch(`${API}/link`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setLinkState(data);
    } catch {
      setLinkState(null);
    }
  };

  const loadModels = async () => {
    try {
      const res = await fetch(`${API}/models`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setModels(data.models || []);
    } catch {
      /* 无可用账号时静默 */
    }
  };

  const loadUsage = async () => {
    setUsageLoading(true);
    try {
      const res = await fetch(`${API}/usage`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) {
        setUsageRows(data.accounts || []);
        setUsageTotal(data.total || null);
        setUsdPerCredit(data.usdPerCredit ?? 0.01);
        // 单账号时后端保留顶层字段，沿用最简展示。
        setUsage(data.usage || null);
      } else if (data?.error) {
        toast.error(`用量查询失败：${data.error}`);
      }
    } catch (e) {
      toast.error(`用量查询失败：${e.message}`);
    } finally {
      setUsageLoading(false);
    }
  };

  // 轮询回调里只取最新函数引用：load 系列函数每次渲染重建，
  // 直接进 effect deps 会让输入框打字等无关重渲染也重建定时器。
  const pollFnsRef = useRef({});
  useEffect(() => {
    pollFnsRef.current = { load, loadStatus, loadModels, loadUsage };
  }, [load, loadStatus, loadModels, loadUsage]);

  const refreshAll = async () => {
    setRefreshing(true);
    try {
      await load();
      await loadStatus();
      await loadModels();
      await loadUsage();
    } finally {
      setRefreshing(false);
    }
  };

  const exportAccounts = () => window.open(`${API}/accounts/export`, '_blank');

  const importFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result || '{}'));
        const payload = Array.isArray(parsed) ? { accounts: parsed } : parsed;
        const res = await fetch(`${API}/accounts/import`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify(payload),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '导入失败');
        toast.success(`已导入 ${data.added ?? 0} 个账号`);
        await Promise.all([load(), loadStatus()]);
      } catch (e) {
        toast.error(`导入失败：${e.message}`);
      }
    };
    reader.readAsText(file);
  };

  useEffect(() => {
    load();
    loadProxyPools();
  }, []);

  useEffect(() => {
    loadStatus();
    loadLink();
  }, [settings?.enabled]);

  useEffect(() => {
    if ((status?.availableCount ?? 0) > 0) {
      loadModels();
      loadUsage();
    }
  }, [status?.availableCount]);

  const save = async (next, msg = '设置已保存') => {
    if (!next) return;
    setSaving(true);
    try {
      const res = await fetch(`${API}/settings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(next),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      setSettings(data.settings);
      setAccounts(data.settings?.accounts || []);
      toast.success(msg);
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    } finally {
      setSaving(false);
    }
  };

  const update = patch => {
    const next = { ...(settings || {}), ...patch };
    setSettings(next);
    void save(next);
  };

  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v });
  };

  // 发起授权：向后端要授权链接与 state，然后打开新窗口。
  // 新增账号时 region 默认取全局选择；重新授权指定账号时传该账号区域与 id，
  // 完成后后端会替换该账号凭据而非新建。
  const startOAuth = async (region = authRegion, accountId = '') => {
    setAuthStarting(true);
    setCallbackUrl('');
    setAuthRegion(region);
    setAuthAccountId(accountId || '');
    try {
      const res = await fetch(`${API}/oauth/auth-url`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ region, accountId: accountId || undefined }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '生成授权链接失败');
      setAuthUrl(data.url);
      setAuthState(data.state);
      setAuthRedirectUri(data.redirectUri || DEFAULT_CALLBACK_URI);
      setAuthOpen(true);
      window.open(data.url, '_blank', 'noopener');
    } catch (e) {
      toast.error(e.message);
    } finally {
      setAuthStarting(false);
    }
  };

  // 完成授权：把跳转后的完整回调地址粘回来。
  const finishOAuth = async () => {
    if (!callbackUrl.trim()) {
      toast.error('请粘贴回调地址');
      return;
    }
    setExchanging(true);
    try {
      const res = await fetch(`${API}/oauth/exchange`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ callbackUrl: callbackUrl.trim(), state: authState }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '授权失败');
      toast.success(`已授权：${data?.account?.email || data?.account?.id || ''}`);
      setAuthOpen(false);
      setAuthAccountId('');
      setCallbackUrl('');
      await load();
      await loadStatus();
      await loadModels();
      await loadUsage();
    } catch (e) {
      toast.error(`授权失败：${e.message}`);
    } finally {
      setExchanging(false);
    }
  };

  const testAccount = async id => {
    setTestingId(id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!data?.success) throw new Error(data?.error || '测试失败');
      toast.success('token 刷新成功');
    } catch (e) {
      toast.error(`刷新失败：${e.message}`);
    } finally {
      setTestingId('');
      await load();
    }
  };

  const deleteAccount = async id => {
    if (!confirmPress(`posthogcode-account-delete:${id}`, `删除账号 ${id}`)) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('账号已删除');
      await load();
      await loadStatus();
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  // 打开账号密码自动授权对话框。指定账号时预填邮箱/区域并锚定该账号。
  const openAutoLogin = account => {
    setAutoLoginAccountId(account?.id || '');
    setAutoLoginEmail(account?.email || '');
    setAutoLoginRegion(account?.region || settings?.region || 'us');
    setAutoLoginPassword('');
    setAutoLoginSessionId('');
    setAutoLoginPhase('form');
    setAutoLoginCode('');
    setAutoLoginDetail('');
    setAutoLoginOpen(true);
  };

  // 提交邮箱+密码 → 后端发起 PostHog 登录；需验证码时进入输入码阶段。
  const startAutoLogin = async () => {
    if (!autoLoginEmail.trim() || !autoLoginPassword) {
      toast.error('请填写邮箱与密码');
      return;
    }
    setAutoLoginBusy(true);
    try {
      const res = await fetch(`${API}/autologin/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          region: autoLoginRegion,
          email: autoLoginEmail.trim(),
          password: autoLoginPassword,
          accountId: autoLoginAccountId || undefined,
        }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '自动登录失败');
      if (data.status === 'awaiting_code') {
        setAutoLoginSessionId(data.sessionId);
        setAutoLoginPhase('code');
        setAutoLoginDetail('已向邮箱发送 6 位验证码，正在自动接收...');
      } else if (data.status === 'awaiting_totp') {
        setAutoLoginSessionId(data.sessionId);
        setAutoLoginPhase('totp');
        setAutoLoginDetail('请输入两步验证码');
      } else if (data.status === 'email_unverified') {
        setAutoLoginPhase('unverified');
        setAutoLoginDetail(data.detail || '邮箱尚未验证，请先完成 PostHog 邮箱验证');
      } else {
        toast.success('自动登录完成');
        setAutoLoginOpen(false);
        await Promise.all([load(), loadStatus(), loadModels(), loadUsage()]);
      }
    } catch (e) {
      toast.error(`自动登录失败：${e.message}`);
    } finally {
      setAutoLoginBusy(false);
    }
  };

  // 等待验证码阶段：后端在后台从邮件收件箱自动取码完成登录，前端轮询状态获取结果。
  useEffect(() => {
    if (!autoLoginOpen || autoLoginPhase !== 'code' || !autoLoginSessionId) return undefined;
    const { load, loadStatus, loadModels, loadUsage } = pollFnsRef.current;
    let cancelled = false;
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`${API}/autologin/status?sessionId=${encodeURIComponent(autoLoginSessionId)}`, {
          headers: getAuthHeaders(),
        });
        const data = await res.json();
        if (cancelled || !data?.success) return;
        if (data.status === 'logged_in') {
          toast.success('已自动完成登录授权');
          setAutoLoginOpen(false);
          await Promise.all([load(), loadStatus(), loadModels(), loadUsage()]);
        } else if (data.status === 'failed') {
          setAutoLoginPhase('form');
          toast.error(data.error || '自动接收验证码失败，请手动输入');
        }
      } catch {
        // 轮询失败不打断，等待下次
      }
    }, 2000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [autoLoginOpen, autoLoginPhase, autoLoginSessionId, getAuthHeaders]);

  // 提交邮箱验证码/TOTP → 完成登录并授权落库。
  const verifyAutoLogin = async () => {
    if (!autoLoginCode.trim()) {
      toast.error('请输入验证码');
      return;
    }
    setAutoLoginBusy(true);
    try {
      const res = await fetch(`${API}/autologin/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ sessionId: autoLoginSessionId, code: autoLoginCode.trim() }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '验证码提交失败');
      toast.success('自动登录完成');
      setAutoLoginOpen(false);
      await Promise.all([load(), loadStatus(), loadModels(), loadUsage()]);
    } catch (e) {
      toast.error(`验证码提交失败：${e.message}`);
    } finally {
      setAutoLoginBusy(false);
    }
  };

  // 取消自动登录，清理后端会话。
  const cancelAutoLogin = async () => {
    setAutoLoginOpen(false);
    if (autoLoginSessionId) {
      try {
        await fetch(`${API}/autologin/cancel`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ sessionId: autoLoginSessionId }),
        });
      } catch {
        // 会话清理尽力而为
      }
    }
    setAutoLoginSessionId('');
    setAutoLoginPhase('form');
  };

  // 打开一键注册对话框：拉取可用收件域名。
  const openSignup = async () => {
    setSignupRegion(settings?.region || 'us');
    setSignupPrefix('probe');
    setSignupPassword('');
    setSignupSessionId('');
    setSignupStatus('');
    setSignupSteps([]);
    setSignupEmail('');
    setSignupError('');
    setSignupMode('auto');
    setSignupReturned({ email: '', password: '', signupUrl: '' });
    setSignupProxy('');
    setSignupProxyOverride('');
    setSignupOpen(true);
    try {
      const res = await fetch('/api/emailcode/domains', { headers: getAuthHeaders() });
      const data = await res.json();
      const list = data?.data?.domains || [];
      setSignupDomains(list);
      setSignupDomain(prev => prev || list[0] || '');
    } catch {
      setSignupDomains([]);
    }
  };

  // 发起注册。auto 模式全自动；manual 模式只备邮箱密码，由用户在无痕窗口过 Turnstile。
  const startSignup = async () => {
    setSignupBusy(true);
    setSignupSteps([]);
    setSignupError('');
    try {
      const manual = signupMode === 'manual';
      const res = await fetch(`${API}/signup/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          region: signupRegion,
          prefix: signupPrefix.trim() || 'probe',
          domain: signupDomain || undefined,
          password: signupPassword || undefined,
          // 非空时按次覆盖插件设置里的默认出口代理。
          proxyPoolId: signupProxyOverride || undefined,
          manual,
        }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '注册失败');
      setSignupSessionId(data.sessionId || '');
      setSignupEmail(data.email || '');
      setSignupStatus(data.status || '');
      if (manual) {
        setSignupReturned({
          email: data.email || '',
          password: data.password || '',
          signupUrl: data.signupUrl || 'https://us.posthog.com/signup',
        });
      }
      if (data.status === 'no_inbox_domain') {
        setSignupError(data.error || '没有可用收信域名');
      }
    } catch (e) {
      setSignupError(e.message);
    } finally {
      setSignupBusy(false);
    }
  };

  // 轮询注册进度：后台是黑盒，必须把每一步实时反馈给用户。
  useEffect(() => {
    if (!signupOpen || !signupSessionId) return undefined;
    if (signupStatus === 'done' || signupStatus === 'failed' || signupStatus === 'throttled' || signupStatus === 'challenge_required') return undefined;
    const { load, loadStatus, loadModels, loadUsage } = pollFnsRef.current;
    let cancelled = false;
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`${API}/signup/status?sessionId=${encodeURIComponent(signupSessionId)}`, {
          headers: getAuthHeaders(),
        });
        const data = await res.json();
        if (cancelled || !data?.success) return;
        setSignupStatus(data.status || '');
        setSignupSteps(data.steps || []);
        if (data.email) setSignupEmail(data.email);
        if (data.proxy) setSignupProxy(data.proxy);
        if (data.error) setSignupError(data.error);
        if (data.status === 'done') {
          toast.success('一键注册完成，账号已授权');
          await Promise.all([load(), loadStatus(), loadModels(), loadUsage()]);
        } else if (data.status === 'failed') {
          toast.error(data.error || '注册失败');
        } else if (data.status === 'throttled') {
          toast.warning(data.error || '注册被限流');
        } else if (data.status === 'challenge_required') {
          toast.warning(data.error || '需要人机验证');
        }
      } catch {
        // 轮询失败不打断
      }
    }, 2000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [signupOpen, signupSessionId, signupStatus, getAuthHeaders]);

  const closeSignup = () => {
    setSignupOpen(false);
    setSignupSessionId('');
    setSignupStatus('');
    setSignupSteps([]);
    setSignupError('');
  };

  // 复制文本到剪贴板（一键注册手动模式需要把邮箱/密码带到浏览器）。
  const copyValue = async (value, label = '内容') => {
    const text = String(value || '').trim();
    if (!text) { toast.warning('没有可复制的内容'); return; }
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      toast.success(`${label}已复制`);
    } catch (e) {
      toast.error(`复制失败：${e.message}`);
    }
  };

  // 注册状态文案。
  const signupStatusText = status => ({
    queued: '已排队，准备创建账号',
    registering: '正在创建 PostHog 账号',
    awaiting_manual_signup: '请在浏览器完成注册',
    awaiting_email_verify: '已注册，等待验证邮件并自动验证邮箱',
    logging_in: '邮箱已验证，正在登录',
    awaiting_login_code: '等待登录验证码',
    done: '完成',
    failed: '失败',
    throttled: '被限流',
    challenge_required: '需要人机验证',
    no_inbox_domain: '无可用收信域名',
  }[status] || status);

  const toggleAccount = async (id, disabled) => {
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ disabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      toast.success(disabled ? '账号已停用' : '账号已启用');
      await load();
      await loadStatus();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const toggleModel = async (id, enabled) => {
    try {
      const res = await fetch(`${API}/models/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ id, enabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadModels();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const toggleAllModels = async enabled => {
    try {
      const res = await fetch(`${API}/models/toggle-batch`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled, ids: models.map(m => m.id) }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadModels();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const linkPlugin = async action => {
    setLinkBusy(true);
    try {
      const res = await fetch(`${API}/link`, {
        method: action === 'link' ? 'POST' : 'DELETE',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || (action === 'link' ? '接入失败' : '断开失败'));
      setLinkState(data);
      toast.success(action === 'link' ? '已接入模型网关端点列表' : '已从端点列表移除');
    } catch (e) {
      toast.error(e.message);
    } finally {
      setLinkBusy(false);
    }
  };

  const enabledModelCount = useMemo(() => models.filter(m => m.enabled).length, [models]);
  const allEnabled = models.length > 0 && enabledModelCount === models.length;

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center justify-between gap-2">
        {linkState?.linked && linkState?.baseUrl ? (
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
            <Rocket className="h-3.5 w-3.5 text-brand" />
            <span className="text-kumo-strong">已接入网关端点</span>
            <span className="truncate font-mono text-kumo-subtle" title="本插件在网关端点列表中的 base_url">
              {linkState.baseUrl}
            </span>
            <span className="text-kumo-subtle">· {linkState.models?.length || 0} 个模型</span>
          </div>
        ) : (
          <span />
        )}
        <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="PostHog Code" icon={<PostHogBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
          <FieldRow title={<span title="总开关：同时控制中继与模型网关接入。打开后启用中继并注册为网关端点；关闭后中继拒服并移除端点">启用中继</span>}>
            <Switch
              checked={!!settings?.enabled}
              disabled={linkBusy}
              onCheckedChange={v => {
                update({ enabled: v });
                if (v) linkPlugin('link');
                else linkPlugin('unlink');
              }}
            />
          </FieldRow>
          <FieldRow title={<span title="PostHog Cloud 区域：us（gateway.us.posthog.com）或 eu（gateway.eu.posthog.com）。切换后需重新授权">区域</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-36"
              value={settings?.region || 'us'}
              onValueChange={v => update({ region: v })}
              items={[
                { value: 'us', label: 'us · 美东' },
                { value: 'eu', label: 'eu · 欧区' },
              ]}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="PostHog 免费层模型（无需绑定付款方式）：GLM-5.2、DeepSeek V4 Flash、Kimi K3。开启后只暴露这些模型，高级模型（Claude Fable/Opus、GPT-6 等）需要 PostHog 付费计划">仅免费层模型</span>}>
            <Switch checked={!!settings?.freeTierOnly} onCheckedChange={v => update({ freeTierOnly: v })} />
          </FieldRow>
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 phc-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder="phc-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="多账号时的选号策略。固定首个：始终用列表第一个可用账号，其余作主备，行为最可预期。轮询：依次轮流，请求均匀分摊。按剩余额度：优先用剩余额度最多的账号，避免某个账号先撞上限。后两种都自动跳过已停用、token 过期与失败冷却中的账号">选号策略</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-44"
              value={settings?.accountStrategy || 'first'}
              onValueChange={v => update({ accountStrategy: v })}
              items={[
                { value: 'first', label: '固定首个可用' },
                { value: 'round-robin', label: '轮询' },
                { value: 'least-used', label: '按剩余额度' },
              ]}
              disabled={saving || (accounts.length < 2)}
            />
          </FieldRow>
          <FieldRow title={<span title="探测上游可达性：用当前账号拉一次模型列表">连通性测试</span>}>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={!status?.availableCount}
                onClick={async () => {
                  try {
                    const res = await fetch(`${API}/test`, { method: 'POST', headers: getAuthHeaders(), body: '{}' });
                    const data = await res.json();
                    if (!data?.success) throw new Error(data?.error || '测试失败');
                    toast.success(`上游可达，共 ${data.modelCount} 个模型`);
                    await loadModels();
                  } catch (e) {
                    toast.error(`测试失败：${e.message}`);
                  }
                }}
              >
                测试
              </Button>
              {status ? (
                <span className="text-xs text-kumo-subtle">
                  账号 {status.availableCount}/{status.accountCount} 可用 · 模型 {status.modelCount}
                </span>
              ) : null}
            </div>
          </FieldRow>
        </SectionCard>

        <SectionCard
          title="授权账号"
          icon={<ShieldCheck className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-1.5">
              <Toolbar size="sm" aria-label="账号导出导入" className="shrink-0">
                <Toolbar.Button onClick={exportAccounts} icon={<Upload className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导出</span>
                </Toolbar.Button>
                <Toolbar.Button onClick={() => fileInputRef.current?.click()} icon={<Download className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导入</span>
                </Toolbar.Button>
              </Toolbar>
              <input
                ref={fileInputRef}
                type="file"
                accept=".json,application/json"
                className="hidden"
                onChange={e => {
                  importFile(e.target.files?.[0]);
                  e.target.value = '';
                }}
              />
              <Button size="sm" variant="outline" disabled={refreshing} onClick={refreshAll} title="重新拉取账号、模型与用量">
                <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
                <span className="hidden cq-sm:inline">刷新</span>
              </Button>
              <Button size="sm" variant="outline" onClick={() => openAutoLogin(null)} title="用邮箱+密码自动登录 PostHog 并授权（密码加密保存）">
                <ShieldCheck className="h-3.5 w-3.5" />
                <span className="hidden cq-sm:inline">密码授权</span>
              </Button>
              <Button size="sm" variant="outline" onClick={openSignup} title="自动生成收件邮箱、注册 PostHog 并完成邮箱验证与授权">
                <Rocket className="h-3.5 w-3.5" />
                <span className="hidden cq-sm:inline">一键注册</span>
              </Button>
              <Button size="sm" variant="primary" disabled={authStarting} onClick={() => startOAuth()}>
                <Plus className="h-3.5 w-3.5" /> 添加账号
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <AppTable tableId="posthogcode-accounts" columns={POSTHOG_ACCOUNT_COLUMNS} className="w-full min-w-[42rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">调用</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.map(a => (
                    <Table.Row key={a.id} className="h-10">
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex justify-center">
                          <Switch
                            size="sm"
                            checked={!a.disabled}
                            onCheckedChange={v => toggleAccount(a.id, !v)}
                            aria-label={`${a.disabled ? '启用' : '停用'} ${a.email || a.id}`}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <div className="truncate text-sm font-medium text-kumo-strong" title={a.id}>
                              {a.email || a.id}
                            </div>
                            <Badge variant={a.region === 'eu' ? 'secondary' : 'info'} className="shrink-0 text-xs" title="该账号所属 Cloud 区域">
                              {String(a.region || 'us').toUpperCase()}
                            </Badge>
                          </div>
                          <div className="truncate font-mono text-kumo-subtle" title={a.id}>
                            {a.id !== a.email ? a.id : ''}
                          </div>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-xs text-kumo-strong">{a.callCount ?? 0}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {a.disabled ? (
                          <Badge variant="danger" className="text-xs" title={a.lastError || undefined} aria-label={a.lastError || undefined}>
                            {a.lastError && /吊销|重新授权/.test(a.lastError) ? '需重新授权' : '停用'}
                          </Badge>
                        ) : !a.scopeReady ? (
                          <Badge variant="warning" className="text-xs">需重新授权</Badge>
                        ) : !a.available ? (
                          <Badge variant="warning" className="text-xs">
                            {a.tokenState === 'expired' ? '已过期' : '不可用'}
                          </Badge>
                        ) : (
                          <Badge variant="success" className="text-xs">
                            {a.expiresInSeconds > 0 ? `有效 ${fmtLeft(a.expiresInSeconds)}` : '有效'}
                          </Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex items-center justify-center gap-1">
                          <Button size="sm" variant="secondary" disabled={testingId === a.id} onClick={() => testAccount(a.id)} title="强制刷新 token 验证凭据">
                            <RefreshCw className={`h-3 w-3 ${testingId === a.id ? 'animate-spin' : ''}`} /> 刷新
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary"
                            disabled={authStarting}
                            onClick={() => startOAuth(a.region || 'us', a.id)}
                            aria-label={`重新授权 ${a.email || a.id}`}
                            title="凭据失效时重新走 OAuth 授权（按该账号区域），会替换当前凭据"
                          >
                            <ExternalLink className="h-3 w-3" />
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary"
                            onClick={() => openAutoLogin(a)}
                            aria-label={`账号密码自动授权 ${a.email || a.id}`}
                            title={a.hasPassword ? '用已保存的密码自动登录并授权（可重新填写）' : '用邮箱+密码自动登录 PostHog 并授权，密码将加密保存'}
                          >
                            <ShieldCheck className="h-3 w-3" />
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant={isArmed(`posthogcode-account-delete:${a.id}`) ? 'destructive' : 'secondary-destructive'}
                            aria-label={isArmed(`posthogcode-account-delete:${a.id}`) ? `再次确认删除 ${a.id}` : `删除 ${a.id}`}
                            title={isArmed(`posthogcode-account-delete:${a.id}`) ? '再次点击确认删除' : `删除 ${a.id}`}
                            onClick={() => deleteAccount(a.id)}
                          >
                            <Trash className="h-3 w-3" />
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                     </Table.Body>
              </AppTable>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无账号"
                description="点击「添加账号」完成 PostHog OAuth 授权（需要 PostHog 账号，scope 为 llm_gateway:read）。"
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="用量"
          icon={<TrendingUp className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Button size="sm" variant="outline" disabled={!status?.availableCount || usageLoading} onClick={loadUsage}>
              <RefreshCw className={`h-3.5 w-3.5 ${usageLoading ? 'animate-spin' : ''}`} />
              <span className="hidden cq-sm:inline">刷新</span>
            </Button>
          }
        >
          {usageRows.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <AppTable tableId="posthogcode-usage" columns={POSTHOG_USAGE_COLUMNS} className="w-full min-w-[46rem] text-xs">
                  <Table.Header sticky variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">剩余额度</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-right">已用</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center" title="日窗口用量占比，超过会被网关限流">日窗口</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center" title="月窗口用量占比，超过会被网关限流">月窗口</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center" title="各账号额度互相独立，周期结束日按各自计费周期">计费周期</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {usageRows.map(row => {
                      const rowLimit = row.limitCredits ?? 0;
                      const rowUsed = row.usedCredits ?? 0;
                      const rowRemain = row.limitCredits != null ? Math.max(0, rowLimit - rowUsed) : null;
                      const pct = rowLimit > 0 ? Math.min(100, (rowRemain / rowLimit) * 100) : null;
                      const tone = pct == null ? '' : pct <= 10 ? 'bg-kumo-danger' : pct <= 30 ? 'bg-kumo-warning' : 'bg-kumo-success';
                      const burst = row.usage ? Number(row.usage.burst?.used_percent ?? 0) : null;
                      const sustained = row.usage ? Number(row.usage.sustained?.used_percent ?? 0) : null;
                      const windowTone = p => (p == null ? '' : p >= 90 ? 'text-kumo-danger' : p >= 70 ? 'text-kumo-warning' : 'text-kumo-subtle');
                      const windowTip = row.usage
                        ? `日窗口 ${Number(row.usage.burst?.used_percent ?? 0).toFixed(1)}%${
                            row.usage.burst?.resets_in_seconds ? `（${fmtLeft(row.usage.burst.resets_in_seconds)}后重置）` : ''
                          } · 月窗口 ${Number(row.usage.sustained?.used_percent ?? 0).toFixed(1)}%${
                            row.usage.sustained?.resets_in_seconds ? `（${fmtLeft(row.usage.sustained.resets_in_seconds)}后重置）` : ''
                          }`
                        : '';
                      return (
                        <Table.Row key={row.id} className="h-12">
                          <Table.Cell className="!px-2.5 !py-1.5">
                            <div className="flex items-center gap-1.5">
                              <div className="truncate text-kumo-strong" title={row.id}>
                                {row.email || row.id}
                              </div>
                              {row.limited ? (
                                <Badge variant="danger" className="shrink-0 text-xs">耗尽</Badge>
                              ) : null}
                            </div>
                            {row.error ? (
                              <div className="truncate text-kumo-warning" title={row.error}>
                                {row.scopeReady ? row.error : '缺少 project:read，需重新授权'}
                              </div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell className="!px-2.5 !py-1.5">
                            {pct != null ? (
                              <Meter
                                label={`$${(rowRemain * usdPerCredit).toFixed(2)} / $${(rowLimit * usdPerCredit).toFixed(2)}`}
                                value={pct}
                                max={100}
                                customValue={`${pct.toFixed(1)}%`}
                                trackClassName="!h-1 bg-kumo-recessed"
                                indicatorClassName={tone}
                              />
                            ) : (
                              <span className="text-kumo-subtle">未取到额度</span>
                            )}
                          </Table.Cell>
                          <Table.Cell className="!px-2 !py-1.5 text-right font-mono text-kumo-subtle">
                            {row.usedCredits != null ? `$${(rowUsed * usdPerCredit).toFixed(2)}` : '—'}
                          </Table.Cell>
                          <Table.Cell className={`!px-2 !py-1.5 text-center font-mono ${windowTone(burst)}`} title={windowTip}>
                            {burst != null ? `${burst.toFixed(1)}%` : '—'}
                          </Table.Cell>
                          <Table.Cell className={`!px-2 !py-1.5 text-center font-mono ${windowTone(sustained)}`} title={windowTip}>
                            {sustained != null ? `${sustained.toFixed(1)}%` : '—'}
                          </Table.Cell>
                          <Table.Cell className="!px-2 !py-1.5 text-center">
                            <span className="font-mono text-kumo-subtle">
                              {row.usage?.billing_period_end
                                ? String(row.usage.billing_period_end).slice(0, 10)
                                : '—'}
                            </span>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </AppTable>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无用量数据"
                description={status?.availableCount ? '点击「刷新」拉取上游额度与限额状态。' : '先完成账号授权，再查询用量。'}
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<SettingsIcon className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <span className="text-xs text-kumo-subtle">{enabledModelCount}/{models.length} 已启用</span>
          }
        >
          {models.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <AppTable tableId="posthogcode-models" columns={POSTHOG_MODEL_COLUMNS} className="w-full min-w-[44rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2 !py-1.5 text-center">
                      <div className="flex justify-center">
                        <Switch size="sm" checked={allEnabled} onCheckedChange={toggleAllModels} aria-label="全选模型" />
                      </div>
                    </Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">来源</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">上下文</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">倍率</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">计划</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {models.map(m => (
                    <Table.Row key={m.id} className="h-9">
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex justify-center">
                          <Switch size="sm" checked={!!m.enabled} onCheckedChange={v => toggleModel(m.id, v)} aria-label={`${m.enabled ? '停用' : '启用'} ${m.id}`} />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="truncate font-mono text-kumo-strong" title={m.rawId || m.id}>
                          {m.id}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="text-kumo-subtle">{m.ownedBy || '—'}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-kumo-subtle">
                          {m.contextWindow ? `${Math.round(m.contextWindow / 1000)}K` : '—'}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span
                          className="font-mono text-kumo-strong"
                          title={
                            m.price
                              ? `输入 $${m.price.inputPerMtok}/M · 输出 $${m.price.outputPerMtok}/M（基准 Claude Sonnet 5）`
                              : '上游未提供计价'
                          }
                        >
                          {m.price ? `${m.price.approximate ? '≈' : ''}${m.price.multiplier.toFixed(2)}×` : '—'}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {m.freeTier ? (
                          <Badge variant="success" className="text-xs">免费</Badge>
                        ) : m.allowed ? (
                          <Badge variant="info" className="text-xs">可用</Badge>
                        ) : (
                          <Badge variant="warning" className="text-xs">受限</Badge>
                        )}
                      </Table.Cell>
                    </Table.Row>
                  ))}
                     </Table.Body>
              </AppTable>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无模型"
                description={status?.availableCount ? '点击「连通性测试」拉取上游模型列表。' : '先完成账号授权，再拉取上游模型列表。'}
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={authOpen} onOpenChange={setAuthOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(46rem,calc(100vw-2rem))] !max-w-[min(46rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {authAccountId ? '重新授权账号' : 'PostHog 授权'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              {authAccountId
                ? `为 ${authAccountId} 重新走 OAuth 授权，完成后替换该账号的凭据。`
                : '在浏览器完成授权后，把跳转到的完整地址粘贴到下方。'}
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                <div className="mb-1 text-kumo-strong">步骤</div>
                <div>1. 点击下方按钮打开授权页（若被拦截可手动复制链接）</div>
                <div>2. 登录并同意授权</div>
                <div>3. 浏览器会跳转到回调地址（可能显示无法访问，属正常）</div>
                <div>4. 复制地址栏完整 URL，粘贴到下面</div>
              </div>
              <div className="flex items-center gap-2">
                <Input size="sm" className="w-full font-mono text-xs" readOnly value={authUrl} aria-label="授权链接" />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void navigator.clipboard?.writeText(authUrl);
                    toast.success('授权链接已复制');
                  }}
                >
                  复制
                </Button>
                <Button size="sm" variant="outline" onClick={() => window.open(authUrl, '_blank', 'noopener')}>
                  <ExternalLink className="h-3.5 w-3.5" /> 打开
                </Button>
              </div>
              <Input
                size="sm"
                label="回调地址"
                className="w-full font-mono text-xs"
                placeholder={`${authRedirectUri || DEFAULT_CALLBACK_URI}?code=...&state=...`}
                value={callbackUrl}
                onChange={e => setCallbackUrl(e.target.value)}
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={exchanging} onClick={finishOAuth}>
              {exchanging ? '验证中...' : '完成授权'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={autoLoginOpen} onOpenChange={open => { if (!open) cancelAutoLogin(); }}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {autoLoginAccountId ? '账号密码自动授权' : '新增：账号密码自动授权'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              用 PostHog 账号密码自动登录并完成授权，无需手动打开授权页。密码加密保存在本地，凭据失效时可一键重新授权。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            {autoLoginPhase === 'form' || autoLoginPhase === 'done' ? (
              <div className="flex flex-col gap-3">
                <Select
                  size="sm"
                  label="区域"
                  className="w-44"
                  value={autoLoginRegion}
                  onValueChange={v => setAutoLoginRegion(v)}
                  items={[
                    { value: 'us', label: 'us · 美东' },
                    { value: 'eu', label: 'eu · 欧区' },
                  ]}
                />
                <Input
                  size="sm"
                  label="PostHog 邮箱"
                  type="email"
                  className="w-full"
                  placeholder="you@example.com"
                  value={autoLoginEmail}
                  disabled={!!autoLoginAccountId}
                  onChange={e => setAutoLoginEmail(e.target.value)}
                />
                <Input
                  size="sm"
                  label="密码"
                  type="password"
                  className="w-full"
                  placeholder="PostHog 账号密码"
                  value={autoLoginPassword}
                  onChange={e => setAutoLoginPassword(e.target.value)}
                />
              </div>
            ) : autoLoginPhase === 'unverified' ? (
              <div className="flex flex-col gap-3">
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                  <div className="mb-1 text-kumo-strong">邮箱尚未验证</div>
                  <div>{autoLoginDetail}</div>
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                  <div className="mb-1 text-kumo-strong">{autoLoginPhase === 'totp' ? '输入两步验证码' : '正在自动接收邮箱验证码'}</div>
                  <div>{autoLoginDetail}</div>
                </div>
                <Input
                  size="sm"
                  label={autoLoginPhase === 'totp' ? '两步验证码' : '邮箱验证码（可手动输入）'}
                  className="w-full text-center font-mono text-base tracking-widest"
                  placeholder="6 位数字"
                  value={autoLoginCode}
                  onChange={e => setAutoLoginCode(e.target.value.replace(/[^\d]/g, '').slice(0, 6))}
                />
              </div>
            )}
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Button size="sm" variant="secondary" disabled={autoLoginBusy} onClick={cancelAutoLogin}>
              取消
            </Button>
            {autoLoginPhase === 'form' || autoLoginPhase === 'done' || autoLoginPhase === 'unverified' ? (
              <Button size="sm" variant="primary" disabled={autoLoginBusy} onClick={autoLoginPhase === 'unverified' ? cancelAutoLogin : startAutoLogin}>
                {autoLoginPhase === 'unverified' ? '知道了' : (autoLoginBusy ? '登录中...' : '登录并授权')}
              </Button>
            ) : (
              <Button size="sm" variant="primary" disabled={autoLoginBusy || !autoLoginCode.trim()} onClick={verifyAutoLogin}>
                {autoLoginBusy ? '验证中...' : '提交验证码'}
              </Button>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={signupOpen} onOpenChange={open => { if (!open) closeSignup(); }}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">一键注册 PostHog 账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              自动生成收件邮箱 → 注册 → 自动收取并提交验证码完成邮箱验证 → 登录并授权。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            {signupSessionId && signupStatus === 'awaiting_manual_signup' ? (
              <div className="flex flex-col gap-3">
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed">
                  <div className="mb-1 font-semibold text-kumo-strong">请在浏览器完成注册</div>
                  <div className="text-kumo-subtle">PostHog 注册有人机验证（Turnstile），无法在面板内自动通过。分两步：</div>
                </div>
                <div className="flex flex-col gap-1.5 text-xs text-kumo-subtle">
                  <span>1. 复制下面的邮箱与密码。</span>
                  <span>2. 在<strong className="text-kumo-strong">无痕窗口</strong>打开注册页，填好后提交（过 Turnstile）。</span>
                  <span>3. 邮箱验证码由面板自动提取提交，完成后自动登录并授权，无需你操作。</span>
                </div>
                <div className="flex flex-col gap-2 rounded border border-kumo-line p-3">
                  <div className="flex items-center gap-2">
                    <span className="w-14 shrink-0 text-xs text-kumo-subtle">邮箱</span>
                    <span className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong" title={signupReturned.email}>{signupReturned.email}</span>
                    <Button size="sm" shape="square" variant="secondary" aria-label="复制邮箱" title="复制邮箱" onClick={() => copyValue(signupReturned.email, '邮箱')}><Copy className="h-3 w-3" /></Button>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="w-14 shrink-0 text-xs text-kumo-subtle">密码</span>
                    <span className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong">{signupReturned.password}</span>
                    <Button size="sm" shape="square" variant="secondary" aria-label="复制密码" title="复制密码" onClick={() => copyValue(signupReturned.password, '密码')}><Copy className="h-3 w-3" /></Button>
                  </div>
                </div>
                <a
                  className="inline-flex items-center justify-center gap-1.5 rounded border border-kumo-line px-3 py-2 text-xs font-medium text-brand hover:border-brand/60"
                  href={signupReturned.signupUrl}
                  target="_blank"
                  rel="noreferrer"
                >
                  <ExternalLink className="h-3.5 w-3.5" /> 打开注册页（建议自行改为无痕窗口）
                </a>
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                  浏览器兼容性说明：网页无法强制打开无痕窗口，这是浏览器安全限制。请自行用无痕窗口打开上面的链接，避免与已登录的 PostHog 账号串号。
                </div>
                <div className="flex flex-col gap-1.5">
                  {signupSteps.map((step, i) => (
                    <div key={i} className="flex items-start gap-2 text-xs text-kumo-subtle">
                      <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-kumo-brand" />
                      <span>{step}</span>
                    </div>
                  ))}
                  <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                    <Loader className="h-3.5 w-3.5 animate-spin" /> 面板正在等待检测到你的注册，最长 2 小时
                  </div>
                </div>
              </div>
            ) : signupSessionId ? (
              <div className="flex flex-col gap-3">
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed">
                  <div className="mb-1 font-semibold text-kumo-strong">{signupStatusText(signupStatus)}</div>
                  {signupEmail ? <div className="font-mono text-kumo-subtle">{signupEmail}</div> : null}
                  {signupProxy ? <div className="text-kumo-subtle" title="本次注册使用的出网代理">出口代理：<span className="font-mono">{signupProxy}</span></div> : null}
                  {signupError ? <div className="mt-1 text-kumo-error">{signupError}</div> : null}
                </div>
                <div className="flex flex-col gap-1.5">
                  {signupSteps.length === 0 ? (
                    <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                      <Loader className="h-3.5 w-3.5" /> 正在准备...
                    </div>
                  ) : signupSteps.map((step, i) => (
                    <div key={i} className="flex items-start gap-2 text-xs text-kumo-subtle">
                      <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-kumo-brand" />
                      <span>{step}</span>
                    </div>
                  ))}
                  {signupStatus !== 'done' && signupStatus !== 'failed' && signupStatus !== 'throttled' && signupStatus !== 'challenge_required' && signupStatus !== 'no_inbox_domain' ? (
                    <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                      <Loader className="h-3.5 w-3.5 animate-spin" /> 处理中，请稍候（可能需要 1-2 分钟）
                    </div>
                  ) : null}
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                <div className="flex gap-2">
                  <Button size="sm" variant={signupMode === 'auto' ? 'primary' : 'outline'} onClick={() => setSignupMode('auto')}>全自动</Button>
                  <Button size="sm" variant={signupMode === 'manual' ? 'primary' : 'outline'} onClick={() => setSignupMode('manual')}>手动过验证</Button>
                </div>
                <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                  {signupMode === 'auto'
                    ? '全自动：面板直接注册。若 PostHog 要求人机验证（Turnstile）或被限流会失败：前者请改用「手动过验证」，后者可换个「出口代理」重试。'
                    : '手动过验证：面板备好随机邮箱与强密码，你在无痕窗口完成注册（过 Turnstile），面板随后自动提取验证码、验证邮箱、登录并授权。'}
                </div>
                <Select
                  size="sm"
                  label="区域"
                  className="w-44"
                  value={signupRegion}
                  onValueChange={v => setSignupRegion(v)}
                  items={[
                    { value: 'us', label: 'us · 美东' },
                    { value: 'eu', label: 'eu · 欧区' },
                  ]}
                />
                <Select
                  size="sm"
                  label="出口代理"
                  className="w-full"
                  aria-label="注册出口代理"
                  placeholder="直连"
                  value={signupProxyOverride || ''}
                  onValueChange={v => setSignupProxyOverride(v === '__direct__' ? '' : String(v))}
                  items={[
                    { value: '__direct__', label: '直连（不使用代理）' },
                    ...proxyPools.map(p => ({ value: p.id, label: `${p.name || p.id}（${(p.proxies || []).length} 节点）` })),
                  ]}
                />
                {proxyPools.length === 0 ? (
                  <div className="rounded border border-kumo-line p-3 text-xs text-kumo-subtle">
                    暂无可用代理池。被 PostHog 限流时可到「进口 / 代理池」创建后在此选择，换出口 IP 重试。
                  </div>
                ) : null}
                <Select
                  size="sm"
                  label="收件域名"
                  className="w-full"
                  aria-label="收件域名"
                  placeholder={signupDomains.length ? '选择已部署收件箱的域名' : '暂无可用域名'}
                  value={signupDomain || null}
                  onValueChange={v => setSignupDomain(String(v))}
                  items={signupDomains.map(d => ({ value: d, label: d }))}
                />
                {signupDomains.length === 0 ? (
                  <div className="rounded border border-kumo-line p-3 text-xs text-kumo-subtle">
                    没有可用收信域名。请先到「DNS → 邮件」Tab 部署收件箱 Worker，再回来注册。
                  </div>
                ) : null}
                <Input
                  size="sm"
                  label="邮箱前缀"
                  className="w-full"
                  placeholder="probe"
                  value={signupPrefix}
                  onChange={e => setSignupPrefix(e.target.value)}
                />
                <Input
                  size="sm"
                  label="密码（留空自动生成）"
                  type="password"
                  className="w-full"
                  placeholder="留空则由面板生成强密码"
                  value={signupPassword}
                  onChange={e => setSignupPassword(e.target.value)}
                />
              </div>
            )}
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Button size="sm" variant="secondary" onClick={closeSignup}>
              {signupStatus === 'done' ? '关闭' : '取消'}
            </Button>
            {signupSessionId ? (
              <Button size="sm" variant="primary" disabled={signupBusy || signupStatus === 'done' || signupStatus === 'awaiting_manual_signup'} onClick={startSignup}>
                重新注册
              </Button>
            ) : (
              <Button size="sm" variant="primary" disabled={signupBusy || signupDomains.length === 0} onClick={startSignup}>
                {signupBusy ? '发起中...' : (signupMode === 'manual' ? '准备邮箱与密码' : '开始注册')}
              </Button>
            )}
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
