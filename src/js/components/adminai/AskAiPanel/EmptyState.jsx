import React, { useRef } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { useAskAiCloudMotion } from '../../../hooks/useAskAiCloudMotion.js';
import { getDisplayTimeZone } from '../../../modules/utils.js';
import { PROMPT_ICONS, SUGGESTED_PROMPTS } from './constants.jsx';

/* ---------- 空状态：云朵 + 问候 + 建议提示（Cloudflare 官方云朵 CSS 移植 + 动态/视差） ---------- */
function EmptyState({ onPrompt }) {
  const cloudRef = useRef(null);
  useAskAiCloudMotion(cloudRef);
  const displayTimeZone = getDisplayTimeZone();
  const hour = Number(
    new Intl.DateTimeFormat('zh-CN', {
      hour: '2-digit',
      hourCycle: 'h23',
      timeZone: displayTimeZone === 'system' ? undefined : displayTimeZone,
    }).format(new Date())
  );
  const greeting = hour < 6 ? '夜深了。' : hour < 12 ? '早上好。' : hour < 18 ? '下午好。' : '晚上好。';
  return (
    <div className="flex h-full flex-1 flex-col items-center overflow-y-auto overscroll-contain">
      <div className="my-auto flex w-full flex-col items-center gap-5 pt-4">
        <div ref={cloudRef} className="askai-cloud-container relative aspect-square -my-8" style={{ width: 150, '--blur-multiplier': 1 }} aria-hidden>
          {/* 节点顺序与 Cloudflare 官方 DOM 一致（5,4,2-blur,3,2,1-shadow,1-blur,1） */}
          <div className="askai-cloud-node askai-cloud-node-5" />
          <div className="askai-cloud-node askai-cloud-node-4" />
          <div className="askai-cloud-node askai-cloud-node-2-blur" />
          <div className="askai-cloud-node askai-cloud-node-3" />
          <div className="askai-cloud-node askai-cloud-node-2" />
          <div className="askai-cloud-node askai-cloud-node-1-shadow" />
          <div className="askai-cloud-node askai-cloud-node-1-blur" />
          <div className="askai-cloud-node askai-cloud-node-1" />
        </div>
        <div className="text-center">
          <h3 className="mb-1.5 text-lg font-medium text-kumo-default">{greeting}</h3>
          <p className="text-sm text-kumo-subtle">今天想做什么？</p>
        </div>

        <div className="flex w-full max-w-[300px] flex-col gap-1.5">
          {SUGGESTED_PROMPTS.map((p, i) => {
            const PromptIcon = PROMPT_ICONS[i % PROMPT_ICONS.length];
            return (
              <Button
                key={p.title}
                size="sm"
                variant="ghost"
                type="button"
                onClick={() => onPrompt(p.subtitle || p.title)}
                className="group relative flex !h-auto w-full cursor-pointer items-center gap-3 rounded-xl border border-kumo-line/50 bg-kumo-elevated p-2 text-left hover:border-brand/40 hover:bg-kumo-base"
              >
                <span className="absolute left-0 top-1/2 h-0 w-[2px] -translate-y-1/2 rounded-full bg-gradient-to-b from-brand/80 to-brand transition-all duration-base group-hover:h-5" />
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-kumo-fill/80 group-hover:bg-brand/10 dark:bg-kumo-control/60 dark:group-hover:bg-brand/20">
                  <PromptIcon className="h-3.5 w-3.5 text-kumo-subtle group-hover:text-brand" />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-xs font-medium text-kumo-subtle group-hover:text-kumo-default">{p.title}</span>
                  <span className="truncate text-xs text-kumo-subtle">{p.subtitle}</span>
                </span>
              </Button>
            );
          })}
        </div>
      </div>
    </div>
  );
}

export default EmptyState;

