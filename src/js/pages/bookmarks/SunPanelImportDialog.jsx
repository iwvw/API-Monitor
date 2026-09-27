import { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Select } from '@cloudflare/kumo/components/select';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { toast } from '../../modules/toast.js';
import { SUNPANEL_GROUP_MODE_OPTIONS, parseSunPanelExport } from '../../modules/sunpanelImport.js';
import { Upload } from '../../components/Icons.jsx';

export default function SunPanelImportDialog({
  open,
  onOpenChange,
  fileInputRef,
  fileName,
  text,
  setText,
  setFileName,
  groupMode,
  setGroupMode,
  targetGroupId,
  setTargetGroupId,
  groups,
  options,
  setOptions,
  importing,
  onSubmit,
}) {
  const [preview, setPreview] = useState(null);
  const [previewError, setPreviewError] = useState('');
  const skipLocalIcons = options.skipLocalIcons;
  const includeSystemCards = options.includeSystemCards;

  // 文本变化时本地解析一次，给出即时的分组/条目统计与告警。
  // 注意：不要再加一个「open 时清空 preview」的 effect —— 选文件后
  // setText 会先触发本 effect 写入 preview，紧随其后的 open-effect 又把它
  // 清成 null，预览区就永远不显示。清空逻辑由父组件在打开时重置 text 完成。
  useEffect(() => {
    if (!text.trim()) {
      setPreview(null);
      setPreviewError('');
      return;
    }
    try {
      setPreview(parseSunPanelExport(text));
      setPreviewError('');
    } catch (error) {
      setPreview(null);
      setPreviewError(error.message);
    }
  }, [text]);

  const pickFile = () => fileInputRef.current?.click();
  const canSubmit = Boolean(text.trim()) && !previewError && !importing;

  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="lg">
        <LayerDialog.Title>导入网址导航</LayerDialog.Title>
        <LayerDialog.Description>
          支持 SunPanel 导出的 <code>.sun-panel.json</code>。导入在服务端单事务内完成，失败会整体回滚。
        </LayerDialog.Description>

        <LayerDialog.Body>
          <div className="flex flex-col gap-3">
          <input
            ref={fileInputRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            aria-label="选择 SunPanel 导出文件"
            onChange={event => {
              const file = event.target.files?.[0];
              if (!file) return;
              if (!file.name.toLowerCase().endsWith('.json')) {
                toast.error('仅支持导入 .json 文件');
                event.target.value = '';
                return;
              }
              file.text()
                .then(content => {
                  // 先在本地解析一次，坏文件在选文件阶段就报错。
                  parseSunPanelExport(content);
                  setText(content);
                  setFileName(file.name);
                  toast.success(`已载入 ${file.name}`);
                })
                .catch(error => toast.error(error.message || '读取导入文件失败'))
                .finally(() => { event.target.value = ''; });
            }}
          />

          <div className="flex items-center gap-2">
            <Button type="button" size="sm" variant="secondary" icon={<Upload className="h-4 w-4" />} onClick={pickFile}>
              选择文件
            </Button>
            {fileName ? (
              <span className="truncate text-xs text-kumo-subtle" title={fileName}>{fileName}</span>
            ) : (
              <span className="text-xs text-kumo-subtle">未选择文件，也可直接粘贴内容</span>
            )}
          </div>

          <CodeEditor
            value={text}
            onChange={setText}
            language="json"
            minHeight="14rem"
            placeholder='{"version":1,"icons":[{"title":"网站","children":[...]}]}'
            label="SunPanel 导出内容"
          />

          {previewError && (
            <div className="rounded-lg border border-kumo-danger/40 bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">
              {previewError}
            </div>
          )}

          {preview && (
            <div className="rounded-lg border border-kumo-line bg-kumo-recessed px-3 py-2 text-xs text-kumo-subtle">
              <div className="font-medium text-kumo-strong">
                共 {preview.groups.length} 个分组、{preview.itemCount} 个网址
              </div>
              <div className="mt-1 flex flex-wrap gap-1">
                {preview.groups.slice(0, 12).map(group => (
                  <span key={group.title} className="rounded bg-kumo-control px-1.5 py-0.5">
                    {group.title} · {group.count}
                  </span>
                ))}
                {preview.groups.length > 12 && <span className="px-1.5 py-0.5">…</span>}
              </div>
              {preview.warnings.length > 0 && (
                <ul className="mt-2 list-disc space-y-0.5 pl-4 text-[11px] text-kumo-warning">
                  {preview.warnings.map(warning => <li key={warning}>{warning}</li>)}
                </ul>
              )}
            </div>
          )}

          <div className="grid gap-3 cq-sm:grid-cols-2">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-strong">分组处理方式</div>
              <Select
                size="sm"
                className="w-full"
                value={groupMode}
                onValueChange={setGroupMode}
                items={SUNPANEL_GROUP_MODE_OPTIONS}
                aria-label="分组处理方式"
              />
            </div>
            {groupMode === 'single' && (
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-strong">目标分组</div>
                <Select
                  size="sm"
                  className="w-full"
                  value={targetGroupId ? String(targetGroupId) : ''}
                  onValueChange={value => setTargetGroupId(Number(value))}
                  placeholder="请选择分组"
                  items={groups.map(group => ({ value: String(group.id), label: group.title }))}
                  aria-label="目标分组"
                />
              </div>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <Checkbox
              size="sm"
              label="跳过 /uploads/ 等本地相对路径图标（改为标题首字）"
              checked={skipLocalIcons}
              onCheckedChange={checked => setOptions(current => ({ ...current, skipLocalIcons: Boolean(checked) }))}
            />
            <Checkbox
              size="sm"
              label="包含 SunPanel 系统应用卡片（无网址，将作为文字占位）"
              checked={includeSystemCards}
              onCheckedChange={checked => setOptions(current => ({ ...current, includeSystemCards: Boolean(checked) }))}
            />
          </div>

          <p className="text-[11px] leading-relaxed text-kumo-subtle">
            SunPanel 的图标集名称（如 <code>ri:ai</code>）在本系统无法渲染，会自动降级为标题首字；
            无网址的系统卡片默认跳过。导入完成后可在列表中调整图标。
          </p>
          </div>
        </LayerDialog.Body>

        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            icon={<Upload className="h-4 w-4" />}
            loading={importing}
            disabled={!canSubmit}
            onClick={onSubmit}
          >
            导入
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
