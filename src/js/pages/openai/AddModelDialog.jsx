import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Textarea } from '@cloudflare/kumo/components/input';

export function AddModelDialog({
  addModelOpen,
  setAddModelOpen,
  addModelText,
  setAddModelText,
  handleAddModelSubmit,
  addModelSaving,
}) {
  return (
    <LayerDialog.Root open={addModelOpen} onOpenChange={setAddModelOpen}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>手动添加模型</LayerDialog.Title>
        <LayerDialog.Body>
          <Textarea
            autoFocus
            value={addModelText}
            onChange={e => setAddModelText(e.target.value)}
            placeholder="用逗号/换行分隔"
            className="w-full min-h-24 font-mono text-xs"
          />
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={handleAddModelSubmit} loading={addModelSaving}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
