import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import CodeEditor from './js/components/ui/CodeEditor.jsx';
import './css/app.css';

document.documentElement.dataset.mode = 'light';
document.documentElement.dataset.theme = 'kumo';
document.documentElement.dataset.displayMode = 'browser';

function Shell() {
  const [open, setOpen] = useState(true);
  return (
    <div className="app-main-shell flex h-dvh w-screen overflow-hidden text-kumo-default">
      <div className="app-main-panel flex-1 flex flex-col h-full overflow-hidden">
        <header className="app-main-topbar box-border flex h-[58px] flex-shrink-0 items-center border-b border-kumo-line px-3">
          顶栏
        </header>
        <main className="flex-1 min-w-0 overflow-x-clip">
          <div className="px-[var(--app-canvas-gutter-x)] pt-[var(--app-canvas-gutter-top)] pb-[var(--app-canvas-gutter-bottom)]">
            <button type="button" onClick={() => setOpen(true)}>打开对话框</button>
            <div style={{ height: 2000 }}>长页面内容</div>
          </div>
        </main>
      </div>
      <LayerDialog.Root open={open} onOpenChange={setOpen}>
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>滚动复现对话框</LayerDialog.Title>
          <LayerDialog.Body>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {Array.from({ length: 14 }).map((_, i) => (
                <p key={i}>段落 {i + 1}：这是用于撑高内容的占位文本，验证内部滚动是否生效。</p>
              ))}
              <CodeEditor
                label="配置 JSON"
                language="json"
                value={JSON.stringify({ a: 1, b: 2 }, null, 2)}
                onChange={() => {}}
                minHeight="24rem"
              />
              {Array.from({ length: 14 }).map((_, i) => (
                <p key={`t${i}`}>尾部段落 {i + 1}：验证底部区域是否可滚动到。</p>
              ))}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary type="button">保存</LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <Shell />
  </React.StrictMode>
);
