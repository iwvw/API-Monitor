// TileResizeHandle —— 图块缩放柄（右下角），结构与 Cloudflare 官方完全一致：
// 保留库默认的 react-resizable-handle / -se 类（负责定位与 se-resize 光标），
// 内部放 Phosphor Notches 图标，fill=currentColor + opacity-40，颜色随主题自适应。
// react-resizable 以位置参数 handle(axis, ref) 调用自定义柄，ref 需挂到元素上供拖拽绑定。
// 库默认样式的黑色直角箭头是 background-image（内联 SVG），用内联 style 覆盖隐藏。
import React from 'react';
import { Notches } from '@phosphor-icons/react';

export default function TileResizeHandle(axis, ref) {
  return (
    <span
      ref={ref}
      style={{ backgroundImage: 'none' }}
      className={`react-resizable-handle react-resizable-handle-${axis || 'se'} flex items-center justify-center bg-none text-kumo-inactive`}
    >
      <Notches className="tile-resize-icon opacity-40" />
    </span>
  );
}