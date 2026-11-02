import React, { useEffect, useState } from 'react';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { Bookmark } from '../../components/Icons.jsx';
import { firstGrapheme } from '../../modules/sunpanelImport.js';

// 网址条目的图标渲染。
//
// 为什么抽成组件：这段逻辑原先在管理页（BookmarksPage.renderItemIcon）与
// 公开页（PublicBookmarksPage.PublicItemIcon）各写了一份，行为已经开始漂移
// （图片加载失败时一份只是隐藏、另一份回退到占位图标）。
// 编辑弹窗的「图标预览」又要用同一套规则，若再抄一份就是第三份副本，
// 迟早出现「预览长这样、保存后长那样」。因此统一到这里。
//
// size 控制盒子尺寸（Tailwind 类，如 'h-10 w-10'），
// 由调用方按密度或固定档位传入；inner 默认与盒子同尺寸（图片满格显示）。

/**
 * @param {object} props
 * @param {{icon_type?:number, icon_src?:string, icon_text?:string, icon_bg_color?:string}} props.item 图标字段来源
 * @param {string} props.size 盒子尺寸类，例如 'h-10 w-10'
 * @param {string} [props.innerSize] 图片/占位图标尺寸类，默认与 size 相同
 * @param {string} [props.className] 追加到根节点的类
 * @param {React.ReactNode} [props.fallback] 自定义占位图标，默认用 Bookmark 图标
 */
export default function ItemIcon({ item, size, innerSize, className, fallback }) {
  const { icon_type: type, icon_src: src, icon_text: text, icon_bg_color: bg } = item || {};
  const inner = innerSize || size;
  const wrapperClass = cx('flex shrink-0 items-center justify-center overflow-hidden rounded-xl', size, className);

  // 图片加载失败要回退到占位图标，否则卡片上会留下一块空白。
  // 需要在 src 变化时重置：预览会随用户改地址而换图，失败状态不能粘住。
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    setFailed(false);
  }, [src]);

  // 图片图标不加底色，只按固定尺寸显示原图（图片自带配色，衬底反而像蒙了一层）。
  if (type === 2 && src && !failed) {
    return (
      <div className={wrapperClass}>
        <img
          src={src}
          alt=""
          loading="lazy"
          className={cx(inner, 'object-contain')}
          onError={() => setFailed(true)}
        />
      </div>
    );
  }

  // 文字 / Emoji / 无图标：需要底色，否则浅色背景上几乎看不见。
  // 仅在 icon_bg_color 是可解析的颜色时才采用。
  const hasBg = typeof bg === 'string' && /^#([0-9a-f]{3,8})$/i.test(bg.trim());
  const containerStyle = hasBg ? { backgroundColor: bg.trim() } : undefined;
  const bgClass = hasBg ? '' : 'bg-kumo-recessed';

  if (type === 1 && text) {
    return (
      <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-xs font-semibold text-kumo-strong')}>
        {firstGrapheme(text)}
      </div>
    );
  }
  if (type === 3 && text) {
    return (
      <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-base')}>
        {text}
      </div>
    );
  }
  // 图片加载失败、或未设置图标：占位图标（同样需要底色）
  return (
    <div style={containerStyle} className={cx(wrapperClass, bgClass, 'text-kumo-subtle')}>
      {fallback ?? <Bookmark className={inner} />}
    </div>
  );
}
