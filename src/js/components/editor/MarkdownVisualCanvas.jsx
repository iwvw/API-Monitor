import React, { useEffect, useRef, useState } from 'react';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

/**
 * 可视化编辑画布 — 只承载 Milkdown headless 编辑内核，
 * 不含产品级边框、工具栏或头部。所有 UI 由外层 Kumo 宿主页负责。
 */
export default function MarkdownVisualCanvas({
  adapterRef,
  onCreateAdapter,
  readOnly = false,
  placeholder = '开始输入...',
  label = '可视化编辑器',
}) {
  const rootRef = useRef(null);
  const [status, setStatus] = useState('loading'); // loading | ready | error

  useEffect(() => {
    if (!adapterRef || !rootRef.current) return;
    adapterRef.current?.setReadonly(readOnly);
  }, [readOnly, adapterRef]);

  useEffect(() => {
    let cancelled = false;
    let instance = null;
    const root = rootRef.current;
    if (!root) return;

    setStatus('loading');

    const init = async () => {
      try {
        instance = onCreateAdapter({
          root,
          placeholder,
        });
        if (cancelled) {
          instance.destroy();
          return;
        }
        await instance.create();
        if (cancelled) {
          instance.destroy();
          return;
        }
        if (adapterRef) {
          adapterRef.current = instance;
        }
        setStatus('ready');
      } catch {
        if (!cancelled) setStatus('error');
      }
    };

    init();

    return () => {
      cancelled = true;
      if (adapterRef && adapterRef.current === instance) {
        adapterRef.current = null;
      }
      if (instance) {
        instance.destroy();
      }
    };
  }, []);  

  return (
    <div
      className="app-markdown-visual-canvas flex min-h-0 flex-1 flex-col"
      aria-label={label}
      aria-busy={status === 'loading'}
    >
      {status === 'loading' && (
        <div className="flex flex-col gap-3 p-4">
          <SkeletonLine className="h-5 w-3/4" />
          <SkeletonLine className="h-4 w-full" />
          <SkeletonLine className="h-4 w-5/6" />
          <SkeletonLine className="h-4 w-2/3" />
        </div>
      )}
      {status === 'error' && (
        <div className="flex min-h-[200px] items-center justify-center p-6 text-sm text-kumo-error">
          编辑器加载失败，刷新页面后重试
        </div>
      )}
      <div
        ref={rootRef}
        className="app-markdown-visual-editor min-h-0 flex-1 overflow-auto px-4 py-3"
        style={{ display: status === 'ready' ? undefined : 'none' }}
      />
    </div>
  );
}
