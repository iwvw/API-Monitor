import React, { useRef } from 'react';
import { Button, DropdownMenu } from '@cloudflare/kumo';
import { ChevronDown, Sliders } from '../../Icons.jsx';

/* 模型选择器（composer 工具行）。
 * 选项来自 /api/openai/models；空选项时不渲染（降级为「用设置里的默认模型」）。
 * 选择结果只影响随后发送的消息，通过 POST /api/admin-ai/messages 的 model 字段传递。
 *
 * container：菜单必须渲染在 [data-askai-menu] 子树内。面板有 mousedown 外部点击监听
 * （点外部即关闭 @/会话菜单），默认 portal 到 body 会让菜单项点击被判为「外部点击」
 * 而在 click 触发前卸载，表现为「点了不切换」。 */
function ModelPicker({ options, value, onChange, disabled }) {
  const anchorRef = useRef(null);
  if (!options || options.length === 0) return null;
  const current = options.find((o) => o.value === value);
  const container = () => anchorRef.current?.closest('[data-askai-menu]') || document.body;
  return (
    <DropdownMenu>
      <span ref={anchorRef} className="inline-flex">
        <DropdownMenu.Trigger
          render={(
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={disabled}
              className="group flex h-6 max-w-[160px] min-w-0 items-center gap-1 rounded-md px-1.5 text-[11px] text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default"
              title={current ? `模型：${current.label}` : '使用默认模型'}
              aria-label="选择模型"
            >
              <Sliders className="h-3 w-3 shrink-0" />
              <span className="truncate">{current ? current.label : '默认模型'}</span>
              <ChevronDown className="h-3 w-3 shrink-0 transition-transform duration-base group-data-[popup-open]:rotate-180" />
            </Button>
          )}
        />
      </span>
      <DropdownMenu.Content
        side="top"
        align="start"
        sideOffset={6}
        container={container}
        className="min-w-52 max-h-72 overflow-y-auto"
      >
        <DropdownMenu.Item onClick={() => onChange('')}>
          {value ? '使用默认模型' : '默认模型（当前）'}
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        {options.map((o) => (
          <DropdownMenu.Item key={o.value} onClick={() => onChange(o.value)} selected={o.value === value}>
            {o.label}
          </DropdownMenu.Item>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

export default ModelPicker;
