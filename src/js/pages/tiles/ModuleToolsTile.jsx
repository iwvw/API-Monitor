// 小尺寸只显示名称+计数，full/wide/rich 追加一行描述与模块健康状态点。
import React from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { navigateModule, widthTier } from './constants.js';
import {
  Cloud,
  Globe,
  FolderOpen,
  Shield,
  Clock,
  Activity,
} from '../../components/Icons.jsx';

export default function ModuleToolsTile({ dash, uptime, density = 'full', w = 1 }) {
  const items = [
    { key: 'paas', name: 'PaaS 实例', desc: 'Koyeb / Fly 应用', count: dash?.paas ? dash.paas.koyeb.total + dash.paas.fly.total : null, ok: dash?.paas ? (dash.paas.koyeb.running + dash.paas.fly.running) > 0 : null, icon: Cloud },
    { key: 'dns', name: '域名解析', desc: 'Cloudflare 区域', count: dash ? dash.dns.zones : null, ok: dash ? dash.dns.zones > 0 : null, icon: Globe },
    { key: 'uptime', name: '服务监控', desc: '监控与状态页', count: uptime ? uptime.total : null, ok: uptime ? uptime.up > 0 : null, icon: Activity },
    { key: 'scheduler', name: '定时任务', desc: '任务与工作流', count: dash ? dash.scheduler.total : null, ok: dash ? dash.scheduler.enabled > 0 : null, icon: Clock },
    { key: 'totp', name: '双因子认证', desc: 'OTP 动态码', count: dash ? dash.totp.total : null, ok: dash ? dash.totp.total > 0 : null, icon: Shield },
    { key: 'filebox', name: '文件分享柜', desc: '文件与片段', count: dash ? dash.filebox.total : null, ok: dash ? dash.filebox.total > 0 : null, icon: FolderOpen },
  ];
  const isHalf = density === 'half';
  const tier = widthTier(w);
  // 列数：w=1（移动端/单列宽）一律单列显示；2×1=3 列 / 4×1=6 列横幅 / 2×2=3 列 / 4×2=4 列 / 2×4=3 列
  const cols = tier === 'narrow'
    ? 1
    : (isHalf ? (tier === 'wide' ? 6 : 3) : (density === 'rich' ? 3 : tier === 'wide' ? 4 : 3));
  const showDesc = !isHalf && density !== 'compact'; // 1×2 窄卡只显示名称+计数
  const isTiny = isHalf && tier === 'narrow'; // 0.5×1：条目高度稍增、更易点击

  // 数据未就绪（dash/uptime 任一未加载）时显示骨架，避免计数占位「—」闪烁
  if (!dash?.paas || !uptime) return <TileSkeleton variant="list" rows={3} />;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className="grid min-h-0 flex-1 auto-rows-min content-start gap-1.5 overflow-y-auto tile-scroll" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {items.map((it) => (
          <TileEntry
            key={it.key}
            name={it.name}
            desc={showDesc ? it.desc : undefined}
            title={`${it.name} · ${it.desc}`}
            onClick={() => navigateModule(it.key)}
            pad={isTiny ? 'py-1.5' : 'py-1'}
            leading={<it.icon className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
            badge={
              it.ok != null ? (
                <span className={`h-1 w-1 shrink-0 rounded-full ${it.ok ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
              ) : undefined
            }
            trailing={
              <span className="shrink-0 text-[10px] tabular-nums text-kumo-subtle">{it.count != null ? it.count : '—'}</span>
            }
          />
        ))}
      </div>
    </div>
  );
}
