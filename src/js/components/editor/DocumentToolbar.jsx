import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Badge } from '@cloudflare/kumo/components/badge';
import DocumentModeSwitch from './DocumentModeSwitch.jsx';
import {
  Save,
  Eye,
  EyeOff,
  LogList,
  Copy,
} from '../Icons.jsx';
import { iconButtonIconClass } from '../ui/AppPrimitives.jsx';

/**
 * 文档工具栏 — Kumo 组件主导的顶部工具栏。
 * 标题、保存状态、模式切换、操作按钮（大纲切换、预览切换、复制）。
 */
export default function DocumentToolbar({
  title,
  onTitleChange,
  readOnly = false,
  mode,
  onModeChange,
  dirty = false,
  saveState = 'idle', // idle | saving | saved | error
  onSave,
  showOutline,
  onToggleOutline,
  showPreview,
  onTogglePreview,
  onCopyMarkdown,
  extraActions,
  className = '',
}) {
  const saveLabel =
    saveState === 'saving' ? '保存中…' :
    saveState === 'saved' ? '已保存' :
    dirty ? '未保存' : '';

  return (
    <div className={`flex min-w-0 flex-col gap-3 border-b border-kumo-line pb-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between ${className}`.trim()}>
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {onTitleChange ? (
          <Input
            value={title || ''}
            onChange={(e) => onTitleChange(e.target.value)}
            placeholder="文档标题"
            className="min-w-0 flex-1 border-0 bg-transparent text-sm font-semibold text-kumo-strong placeholder:text-kumo-subtle"
            aria-label="文档标题"
          />
        ) : (
          <span className="min-w-0 truncate text-sm font-semibold text-kumo-strong">
            {title || '未命名文档'}
          </span>
        )}
        {saveLabel && (
          <Badge
            variant={
              saveState === 'saving' ? 'warning' :
              saveState === 'error' ? 'error' :
              dirty ? 'warning' : 'success'
            }
            className="shrink-0"
          >
            {saveLabel}
          </Badge>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-2">
        <DocumentModeSwitch mode={mode} onModeChange={onModeChange} />

        {onToggleOutline && (
          <Button
            type="button"
            size="sm"
            variant={showOutline ? 'primary' : 'secondary'}
            shape="square"
            aria-label={showOutline ? '隐藏大纲' : '显示大纲'}
            title={showOutline ? '隐藏大纲' : '显示大纲'}
            icon={<LogList className={iconButtonIconClass} />}
            onClick={onToggleOutline}
          />
        )}

        {onTogglePreview && mode === 'write' && (
          <Button
            type="button"
            size="sm"
            variant={showPreview ? 'primary' : 'secondary'}
            shape="square"
            aria-label={showPreview ? '隐藏预览' : '显示预览'}
            title={showPreview ? '隐藏预览' : '显示预览'}
            icon={showPreview ? <EyeOff className={iconButtonIconClass} /> : <Eye className={iconButtonIconClass} />}
            onClick={onTogglePreview}
          />
        )}

        {onCopyMarkdown && (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            shape="square"
            aria-label="复制 Markdown"
            title="复制 Markdown"
            icon={<Copy className={iconButtonIconClass} />}
            onClick={onCopyMarkdown}
          />
        )}

        {!readOnly && onSave && (
          <Button
            type="button"
            size="sm"
            variant={dirty ? 'primary' : 'secondary'}
            onClick={onSave}
            icon={<Save className={iconButtonIconClass} />}
          >
            保存
          </Button>
        )}

        {extraActions}
      </div>
    </div>
  );
}
