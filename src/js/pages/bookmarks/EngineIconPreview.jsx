import React, { useState } from 'react';
import { Icon } from '@iconify/react';
import { cx } from '../../components/ui/AppPrimitives.jsx';

// 搜索引擎图标。
//
// 公开页（搜索框、引擎菜单）与管理页（引擎列表预览）共用同一套解析规则，
// 否则会出现「管理页看到 A、公开页显示 B」的偏差。
//
// icon 支持两种写法：
//   1. Iconify 名（如 logos:bing）—— 推荐，矢量、随构建打包、无外网请求；
//   2. 图片地址（http(s):// 或 / 开头的站内路径）—— 便于用户放自己的图标。
// 都不可用或图片加载失败时，回退为「品牌色圆底 + 名称首字」，
// 保证任何情况下都能看出是哪个引擎，而不是留一片空白。
export default function EngineIconPreview({ icon, label = '', color = '', size = 'h-5 w-5' }) {
  const [failed, setFailed] = useState(false);
  const raw = String(icon ?? '').trim();
  const isImage = /^https?:\/\//i.test(raw) || raw.startsWith('/');

  // 图片图标：加载失败回退到文字，并记住失败状态
  if (raw && isImage && !failed) {
    return (
      <img
        src={raw}
        alt=""
        className={cx('shrink-0 object-contain', size)}
        onError={() => setFailed(true)}
        aria-hidden="true"
      />
    );
  }

  // Iconify 图标：名称形如 "集合:图标"，否则当作无效（例如用户随手填的文字）
  if (raw && !isImage && raw.includes(':')) {
    return <Icon icon={raw} className={cx('shrink-0', size)} aria-hidden="true" />;
  }

  // 回退：品牌色圆底 + 首字
  const initial = String(label || '?').trim().slice(0, 1) || '?';
  return (
    <span
      className={cx(
        'flex shrink-0 items-center justify-center rounded-full text-[10px] font-bold',
        // 未填底色时用主题中性色 + 深色文字，避免在浅色主题下白字看不清
        color ? 'text-white' : 'bg-kumo-recessed text-kumo-strong',
        size
      )}
      style={color ? { backgroundColor: color } : undefined}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}
