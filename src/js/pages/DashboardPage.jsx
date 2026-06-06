import React, { useState, useEffect } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import useStore from '../store.js';
import {
  Server,
  Terminal,
  Cloud,
  Globe,
  Activity,
  History,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen,
  TrendingUp
} from '../components/Icons.jsx';

function DashboardPage() {
  const { setMainActiveTab } = useStore();

  const [stats, setStats] = useState({
    servers: { total: 0, online: 0, offline: 0, error: 0 },
    geminiCli: { total_calls: 0, success_calls: 0, daily_trend: [] },
    paas: {
      koyeb: { total: 0, running: 0 },
      fly: { total: 0, running: 0 },
    },
    dns: { zones: 0 },
    uptime: { total: 0, up: 0, down: 0 },
    filebox: { total: 0 },
    totp: { total: 0 },
  });

  const [loading, setLoading] = useState(false);
  const [lastUpdate, setLastUpdate] = useState('');

  // 串联并行请求所有模块数据
  const fetchDashboardStats = async (showLoading = true) => {
    if (showLoading) setLoading(true);

    const savedPassword = localStorage.getItem('admin_password') || '';
    const headers = {
      'Content-Type': 'application/json',
      'x-admin-password': savedPassword,
    };

    // 1. 获取主机监控
    const fetchServers = async () => {
      try {
        const res = await fetch('/api/server/accounts', { headers });
        const data = await res.json();
        if (data.success && Array.isArray(data.data)) {
          const list = data.data;
          return {
            total: list.length,
            online: list.filter((s) => s.status === 'online').length,
            offline: list.filter((s) => s.status === 'offline').length,
            error: list.filter((s) => s.status === 'error').length,
          };
        }
      } catch (e) {
        console.error('[Dashboard] Servers fetch failed:', e);
      }
      return { total: 0, online: 0, offline: 0, error: 0 };
    };

    // 2. 获取 API 网关
    const fetchApiStats = async () => {
      try {
        const res = await fetch('/api/gemini-cli/stats', { headers });
        if (res.ok) {
          const data = await res.json();
          const detail = data.data || data;
          return {
            total_calls: detail.total_calls || 0,
            success_calls: detail.success_calls || 0,
            daily_trend: detail.daily_trend || [],
          };
        }
      } catch (e) {
        console.error('[Dashboard] API stats fetch failed:', e);
      }
      return { total_calls: 0, success_calls: 0, daily_trend: [] };
    };

    // 3. 获取 PaaS (Koyeb & Fly.io)
    const fetchPaaS = async () => {
      let koyeb = { total: 0, running: 0 };
      let fly = { total: 0, running: 0 };

      // Koyeb
      try {
        const res = await fetch('/api/koyeb/data', { headers });
        if (res.ok) {
          const data = await res.json();
          if (data.success && data.accounts) {
            data.accounts.forEach((acc) => {
              if (acc.projects) {
                acc.projects.forEach((p) => {
                  if (p.services) {
                    p.services.forEach((s) => {
                      koyeb.total++;
                      if (s.status === 'HEALTHY' || s.status === 'RUNNING') {
                        koyeb.running++;
                      }
                    });
                  }
                });
              }
            });
          }
        }
      } catch (e) {
        console.error('[Dashboard] Koyeb fetch failed:', e);
      }

      // Fly.io
      try {
        const res = await fetch('/api/flyio/proxy/apps', { headers });
        if (res.ok) {
          const data = await res.json();
          if (data.success && data.data) {
            data.data.forEach((acc) => {
              if (acc.apps) {
                acc.apps.forEach((app) => {
                  fly.total++;
                  if (app.status === 'deployed' || app.status === 'running') {
                    fly.running++;
                  }
                });
              }
            });
          }
        }
      } catch (e) {
        console.error('[Dashboard] Fly.io fetch failed:', e);
      }

      return { koyeb, fly };
    };

    // 4. 获取 DNS 区域
    const fetchDns = async () => {
      try {
        const res = await fetch('/api/cloudflare/zones', { headers });
        if (res.ok) {
          const data = await res.json();
          if (data.success && Array.isArray(data.data)) {
            return { zones: data.data.length };
          }
        }
      } catch (e) {
        console.error('[Dashboard] DNS fetch failed:', e);
      }
      return { zones: 0 };
    };

    // 5. 获取 Uptime monitors
    const fetchUptime = async () => {
      try {
        const res = await fetch('/api/uptime/monitors', { headers });
        if (res.ok) {
          const data = await res.json();
          const monitors = Array.isArray(data) ? data : data.data || [];
          let up = 0;
          let down = 0;

          monitors.forEach((m) => {
            if (m.active) {
              if (m.lastHeartbeat) {
                const status = m.lastHeartbeat.status;
                if (status === 1 || status === 'up') {
                  up++;
                } else {
                  down++;
                }
              } else {
                up++;
              }
            }
          });

          return { total: monitors.length, up, down };
        }
      } catch (e) {
        console.error('[Dashboard] Uptime fetch failed:', e);
      }
      return { total: 0, up: 0, down: 0 };
    };

    // 6. 获取文件柜文件数量
    const fetchFilebox = async () => {
      try {
        const res = await fetch('/api/filebox/history', { headers });
        if (res.ok) {
          const data = await res.json();
          if (data.success && Array.isArray(data.data)) {
            return { total: data.data.length };
          }
        }
      } catch (e) {
        console.error('[Dashboard] Filebox fetch failed:', e);
      }
      return { total: 0 };
    };

    // 7. 获取 TOTP 数量
    const fetchTotp = async () => {
      try {
        const res = await fetch('/api/totp/accounts', { headers });
        if (res.ok) {
          const data = await res.json();
          if (data.success && Array.isArray(data.data)) {
            return { total: data.data.length };
          }
        }
      } catch (e) {
        console.error('[Dashboard] TOTP fetch failed:', e);
      }
      return { total: 0 };
    };

    const results = await Promise.allSettled([
      fetchServers(),
      fetchApiStats(),
      fetchPaaS(),
      fetchDns(),
      fetchUptime(),
      fetchFilebox(),
      fetchTotp(),
    ]);

    const updatedStats = {
      servers: results[0].status === 'fulfilled' ? results[0].value : { total: 0, online: 0, offline: 0, error: 0 },
      geminiCli: results[1].status === 'fulfilled' ? results[1].value : { total_calls: 0, success_calls: 0, daily_trend: [] },
      paas: results[2].status === 'fulfilled' ? results[2].value : { koyeb: { total: 0, running: 0 }, fly: { total: 0, running: 0 } },
      dns: results[3].status === 'fulfilled' ? results[3].value : { zones: 0 },
      uptime: results[4].status === 'fulfilled' ? results[4].value : { total: 0, up: 0, down: 0 },
      filebox: results[5].status === 'fulfilled' ? results[5].value : { total: 0 },
      totp: results[6].status === 'fulfilled' ? results[6].value : { total: 0 },
    };

    setStats(updatedStats);
    setLastUpdate(new Date().toLocaleTimeString());
    setLoading(false);
  };

  useEffect(() => {
    fetchDashboardStats();
  }, []);

  const apiSuccessRate = () => {
    const { total_calls, success_calls } = stats.geminiCli;
    if (total_calls === 0) return '0%';
    return `${Math.round((success_calls / total_calls) * 1000) / 10}%`;
  };

  // 绘制 SVG Sparkline
  const generateSvgPath = () => {
    const trend = stats.geminiCli.daily_trend || [];
    if (trend.length < 2) return '';
    const values = trend.map((t) => t.total || 0);
    const maxVal = Math.max(...values, 10);
    const width = 500;
    const height = 120;
    const padding = 10;
    const stepX = (width - padding * 2) / (values.length - 1);
    const stepY = (height - padding * 2) / maxVal;

    return values
      .map((val, idx) => {
        const x = padding + idx * stepX;
        const y = height - padding - val * stepY;
        return `${idx === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
      })
      .join(' ');
  };

  const generateSvgFillPath = () => {
    const trend = stats.geminiCli.daily_trend || [];
    if (trend.length < 2) return '';
    const values = trend.map((t) => t.total || 0);
    const maxVal = Math.max(...values, 10);
    const width = 500;
    const height = 120;
    const padding = 10;
    const stepX = (width - padding * 2) / (values.length - 1);
    const stepY = (height - padding * 2) / maxVal;

    const points = values.map((val, idx) => {
      const x = padding + idx * stepX;
      const y = height - padding - val * stepY;
      return `${x.toFixed(1)} ${y.toFixed(1)}`;
    });

    const firstX = padding;
    const lastX = padding + (values.length - 1) * stepX;
    const baseY = height - padding;

    return `M ${firstX.toFixed(1)} ${baseY.toFixed(1)} L ${points.join(' L ')} L ${lastX.toFixed(1)} ${baseY.toFixed(1)} Z`;
  };

  return (
    <div className="space-y-6">
      
      {/* ==================== Header ==================== */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-kumo-strong">系统控制台</h1>
          <p className="text-xs text-kumo-subtle mt-0.5">系统运行概览与状态指标</p>
        </div>

        <div className="flex items-center gap-3 flex-shrink-0">
          {lastUpdate && (
            <div className="text-[11px] text-kumo-subtle flex items-center gap-1.5 select-none">
              <History className="w-3.5 h-3.5" />
              <span>上次更新: {lastUpdate}</span>
            </div>
          )}
          
          <Button
            onClick={() => fetchDashboardStats(true)}
            variant="secondary"
            size="sm"
            loading={loading}
          >
            {!loading && <RefreshCw className="w-3.5 h-3.5" />}
            <span>刷新数据</span>
          </Button>
        </div>
      </div>

      {/* ==================== Stats Grid (5 Cards) ==================== */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
        
        {/* Servers Card */}
        <div
          onClick={() => setMainActiveTab('server')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-4">
              <div className="w-8 h-8 rounded-md bg-kumo-info-tint text-kumo-info flex items-center justify-center">
                <Server className="w-4 h-4" />
              </div>
              <span className={`text-[11px] font-semibold px-2 py-0.5 rounded border ${
                stats.servers.total === 0
                  ? 'text-kumo-subtle bg-kumo-recessed border-kumo-line'
                  : stats.servers.online === stats.servers.total
                    ? 'text-kumo-success bg-kumo-success/10 border-kumo-success/20'
                    : stats.servers.online === 0
                      ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
                      : 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20'
              }`}>
                {stats.servers.online}/{stats.servers.total} 在线
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">主机实例管理</span>
              <span className="text-2xl font-bold text-kumo-strong tabular-nums">
                {stats.servers.total} <span className="text-xs font-normal text-kumo-subtle">台主机</span>
              </span>
            </div>
          </div>
          <div className="mt-4 pt-3 border-t border-kumo-line flex items-center justify-between text-xs text-kumo-subtle group-hover:text-kumo-strong transition-colors">
            <span className={stats.servers.total > 0 && stats.servers.online < stats.servers.total ? (stats.servers.online === 0 ? 'text-kumo-danger font-medium' : 'text-kumo-warning font-medium') : ''}>
              {stats.servers.total === 0 
                ? '暂无主机实例' 
                : stats.servers.online === stats.servers.total 
                  ? '所有主机运行正常' 
                  : stats.servers.online === 0
                    ? '全部主机发生故障'
                    : `${stats.servers.offline} 台离线`
              }
            </span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* API Gateway Card */}
        <div
          onClick={() => setMainActiveTab('gemini-cli')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-4">
              <div className="w-8 h-8 rounded-md bg-kumo-brand/10 text-kumo-brand flex items-center justify-center">
                <Terminal className="w-4 h-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-subtle bg-kumo-recessed px-2 py-0.5 rounded border border-kumo-line">
                API 网关
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">调用次数</span>
              <span className="text-2xl font-bold text-kumo-strong tabular-nums">
                {stats.geminiCli.total_calls} <span className="text-xs font-normal text-kumo-subtle">次</span>
              </span>
            </div>
          </div>
          <div className="mt-4 pt-3 border-t border-kumo-line flex items-center justify-between text-xs text-kumo-subtle group-hover:text-kumo-strong transition-colors">
            <span>{apiSuccessRate()} 成功率</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* PaaS Applications Card */}
        <div
          onClick={() => setMainActiveTab('paas')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-4">
              <div className="w-8 h-8 rounded-md bg-kumo-badge-purple/10 text-kumo-badge-purple flex items-center justify-center">
                <Cloud className="w-4 h-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-badge-purple bg-kumo-badge-purple/10 px-2 py-0.5 rounded border border-kumo-badge-purple/20">
                {stats.paas.koyeb.running + stats.paas.fly.running} 运行
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">云应用实例</span>
              <span className="text-2xl font-bold text-kumo-strong tabular-nums">
                {stats.paas.koyeb.total + stats.paas.fly.total} <span className="text-xs font-normal text-kumo-subtle">个应用</span>
              </span>
            </div>
          </div>
          <div className="mt-4 pt-3 border-t border-kumo-line flex items-center justify-between text-xs text-kumo-subtle group-hover:text-kumo-strong transition-colors">
            <span>应用实例状态正常</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* Cloudflare DNS Card */}
        <div
          onClick={() => setMainActiveTab('dns')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-4">
              <div className="w-8 h-8 rounded-md bg-kumo-badge-orange/10 text-kumo-badge-orange flex items-center justify-center">
                <Globe className="w-4 h-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-subtle bg-kumo-recessed px-2 py-0.5 rounded border border-kumo-line">
                Cloudflare
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">域名解析</span>
              <span className="text-2xl font-bold text-kumo-strong tabular-nums">
                {stats.dns.zones} <span className="text-xs font-normal text-kumo-subtle">个区域</span>
              </span>
            </div>
          </div>
          <div className="mt-4 pt-3 border-t border-kumo-line flex items-center justify-between text-xs text-kumo-subtle group-hover:text-kumo-strong transition-colors">
            <span>域名配置正常</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* Uptime Monitors Card */}
        <div
          onClick={() => setMainActiveTab('uptime')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-4">
              <div className="w-8 h-8 rounded-md bg-kumo-success/10 text-kumo-success flex items-center justify-center">
                <Activity className="w-4 h-4" />
              </div>
              <span className={`text-[11px] font-semibold px-2 py-0.5 rounded border ${
                stats.uptime.down > 0
                  ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
                  : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20'
              }`}>
                {stats.uptime.up}/{stats.uptime.total} 在线
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">服务监控</span>
              <span className="text-2xl font-bold text-kumo-strong tabular-nums">
                {stats.uptime.total} <span className="text-xs font-normal text-kumo-subtle">个监测</span>
              </span>
            </div>
          </div>
          <div className="mt-4 pt-3 border-t border-kumo-line flex items-center justify-between text-xs text-kumo-subtle group-hover:text-kumo-strong transition-colors">
            <span className={stats.uptime.down > 0 ? 'text-kumo-danger font-medium' : ''}>
              {stats.uptime.down > 0 ? `${stats.uptime.down} 个监测发生故障` : '服务状态健康'}
            </span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

      </div>

      {/* ==================== Detail Column Split ==================== */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        
        {/* Left Column: API Trend Graph (Span 2) */}
        <div className="lg:col-span-2 bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 flex flex-col justify-between min-h-[340px]">
          <div className="flex items-center justify-between border-b border-kumo-line pb-3.5">
            <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2 select-none">
              <TrendingUp className="w-4 h-4 text-kumo-brand" />
              API 调用趋势
            </h3>
            <span className="text-[10px] text-kumo-subtle bg-kumo-recessed border border-kumo-line px-2 py-0.5 rounded font-medium">
              最近 24 小时
            </span>
          </div>

          {/* Svg Sparkline */}
          <div className="flex-1 flex flex-col justify-center py-6">
            {stats.geminiCli.daily_trend && stats.geminiCli.daily_trend.length >= 2 ? (
              <div className="w-full flex flex-col justify-between h-44">
                <div className="flex-1 relative">
                  <svg className="w-full h-full overflow-visible" viewBox="0 0 500 120" preserveAspectRatio="none">
                    <defs>
                      <linearGradient id="sparkline-grad" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor="var(--color-kumo-brand)" stopOpacity="0.15" />
                        <stop offset="100%" stopColor="var(--color-kumo-brand)" stopOpacity="0.0" />
                      </linearGradient>
                    </defs>
                    <path
                      d={generateSvgFillPath()}
                      fill="url(#sparkline-grad)"
                    />
                    <path
                      d={generateSvgPath()}
                      fill="none"
                      stroke="var(--color-kumo-brand)"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </div>
                {/* 刻度时间 */}
                <div className="flex items-center justify-between text-[10px] text-kumo-subtle mt-4 border-t border-kumo-line pt-2.5 select-none">
                  <span>{stats.geminiCli.daily_trend[0].date}</span>
                  <span>{stats.geminiCli.daily_trend[Math.floor(stats.geminiCli.daily_trend.length / 2)].date}</span>
                  <span>{stats.geminiCli.daily_trend[stats.geminiCli.daily_trend.length - 1].date}</span>
                </div>
              </div>
            ) : (
              <div className="text-center text-xs text-kumo-subtle py-12">
                暂无调用趋势数据
              </div>
            )}
          </div>

          <div className="text-[11px] text-kumo-subtle flex items-center gap-2 mt-2 select-none">
            <span className="w-1.5 h-1.5 rounded-full bg-kumo-brand flex-shrink-0" />
            <span>主网关 Gemini CLI API 调用正常</span>
          </div>
        </div>

        {/* Right Column: Services & Tools List */}
        <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-6 flex flex-col justify-between min-h-[340px]">
          <div className="border-b border-kumo-line pb-3.5">
            <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2 select-none">
              <Box className="w-4 h-4 text-kumo-brand" />
              服务 & 工具
            </h3>
          </div>

          <div className="flex-1 py-4 space-y-3.5">
            {/* Koyeb */}
            <div
              onClick={() => setMainActiveTab('paas')}
              className="flex items-center justify-between p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-md bg-kumo-badge-purple/10 text-kumo-badge-purple flex items-center justify-center text-sm flex-shrink-0">
                  <Box className="w-4 h-4" />
                </div>
                <div>
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Koyeb</h4>
                  <p className="text-[10px] text-kumo-subtle mt-0.5">边缘计算应用服务</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.koyeb.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.koyeb.running}
              </div>
            </div>

            {/* Fly.io */}
            <div
              onClick={() => setMainActiveTab('paas')}
              className="flex items-center justify-between p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-md bg-kumo-brand/10 text-kumo-brand flex items-center justify-center text-sm flex-shrink-0">
                  <Send className="w-4 h-4" />
                </div>
                <div>
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Fly.io</h4>
                  <p className="text-[10px] text-kumo-subtle mt-0.5">全球微型虚拟机</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.fly.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.fly.running}
              </div>
            </div>

            {/* 2FA */}
            <div
              onClick={() => setMainActiveTab('totp')}
              className="flex items-center justify-between p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-md bg-kumo-success/10 text-kumo-success flex items-center justify-center text-sm flex-shrink-0">
                  <Shield className="w-4 h-4" />
                </div>
                <div>
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">2FA 安全令牌</h4>
                  <p className="text-[10px] text-kumo-subtle mt-0.5">OTP 动态验证码账号</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.totp.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.totp.total}
              </div>
            </div>

            {/* FileBox */}
            <div
              onClick={() => setMainActiveTab('filebox')}
              className="flex items-center justify-between p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-md bg-kumo-info-tint text-kumo-info flex items-center justify-center text-sm flex-shrink-0">
                  <FolderOpen className="w-4 h-4" />
                </div>
                <div>
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">文件分享柜</h4>
                  <p className="text-[10px] text-kumo-subtle mt-0.5">文件与片段分享柜</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.filebox.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.filebox.total}
              </div>
            </div>
          </div>

          <div className="text-[10px] text-kumo-subtle border-t border-kumo-line pt-3 select-none text-center">
            点击以上卡片可直接跳转相应模块管理。
          </div>
        </div>

      </div>

    </div>
  );
}

export default DashboardPage;
