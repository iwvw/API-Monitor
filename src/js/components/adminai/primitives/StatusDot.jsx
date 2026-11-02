import React from 'react';
import { Loader } from '@cloudflare/kumo';
import { Check, X } from '../../Icons.jsx';
import { STEP } from '../../../modules/adminAiMessages.js';

/* 工具/步骤状态环：running=品牌色 spinner / success=绿对勾 / failed=红叉。
 * 此前 ToolCallCard 里有两套独立实现（状态徽章与分组徽章），写法不一致，统一收敛到这里。
 * 尺寸走静态类名映射——Tailwind 无法从 `h-${n}` 这类拼接串生成 CSS，会静默失效。 */
const SIZES = {
  sm: 'h-4 w-4',
};

export function StatusDot({ status, size = 'sm' }) {
  const ring = `inline-flex ${SIZES[size] || SIZES.sm} shrink-0 items-center justify-center rounded-full`;
  if (status === STEP.RUNNING) {
    return (
      <span className={`${ring} bg-brand/10 text-brand`}>
        <Loader size={10} className="animate-spin" />
      </span>
    );
  }
  if (status === STEP.FAILED) {
    return (
      <span className={`${ring} bg-kumo-danger/10 text-kumo-danger`}>
        <X className="h-2.5 w-2.5" />
      </span>
    );
  }
  return (
    <span className={`${ring} bg-kumo-success/10 text-kumo-success`}>
      <Check className="h-2.5 w-2.5" />
    </span>
  );
}

export default StatusDot;
