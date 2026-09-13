import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
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
      <Dialog.Root open={addModelOpen} onOpenChange={setAddModelOpen}>
        <Dialog className="!w-[min(30rem,calc(100vw-2rem))]">
          <div className="grid gap-1 px-6 pt-5 pb-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">手动添加模型</Dialog.Title>
          </div>
          <div className="px-6 pb-4">
            <Textarea
              autoFocus
              value={addModelText}
              onChange={e => setAddModelText(e.target.value)}
              placeholder="用逗号/换行分隔"
              className="w-full min-h-24 font-mono text-xs"
            />
          </div>
          <div className="flex items-center justify-end gap-2 border-t border-kumo-line px-6 py-4">
            <Dialog.Close
              render={props => (
                <Button size="sm" variant="secondary" {...props}>
                  取消
                </Button>
              )}
            />
            <Button size="sm" variant="primary" onClick={handleAddModelSubmit} loading={addModelSaving}>
              保存
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
