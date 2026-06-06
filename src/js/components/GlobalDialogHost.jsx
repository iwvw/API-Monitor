import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { cancelDialog, resolveDialog, subscribeDialog } from '../modules/dialog.js';
import { X } from './Icons.jsx';

const getConfirmVariant = (request) => {
  const options = request?.options || {};
  const marker = `${options.confirmClass || ''} ${options.variant || ''} ${options.type || ''}`.toLowerCase();
  return marker.includes('danger') || marker.includes('destructive')
    ? 'destructive'
    : 'primary';
};

function GlobalDialogHost() {
  const [request, setRequest] = useState(null);
  const [promptValue, setPromptValue] = useState('');

  useEffect(() => subscribeDialog(setRequest), []);

  useEffect(() => {
    setPromptValue(request?.options?.defaultValue || '');
  }, [request?.id, request?.options?.defaultValue]);

  const options = request?.options;
  const role = useMemo(() => {
    if (!request) return 'dialog';
    if (options?.role) return options.role;
    return request.type === 'alert' ? 'dialog' : 'alertdialog';
  }, [options?.role, request]);

  if (!request || !options) return null;

  const handleCancel = () => {
    cancelDialog();
  };

  const handleConfirm = () => {
    if (request.type === 'prompt') {
      resolveDialog(promptValue);
      return;
    }
    resolveDialog(true);
  };

  return (
    <Dialog.Root
      open
      role={role}
      disablePointerDismissal={options.disablePointerDismissal}
      onOpenChange={(open) => {
        if (!open) handleCancel();
      }}
    >
      <Dialog size={options.size || 'sm'} className="p-5">
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            handleConfirm();
          }}
        >
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                {options.title}
              </Dialog.Title>
              {options.message ? (
                <Dialog.Description className="mt-2 whitespace-pre-wrap text-sm leading-6 text-kumo-subtle">
                  {options.message}
                </Dialog.Description>
              ) : null}
            </div>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square"
                  size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  onClick={handleCancel}
                />
              )}
            />
          </div>

          {request.type === 'prompt' ? (
            <Input
              autoFocus
              aria-label={options.placeholder || options.title}
              placeholder={options.placeholder}
              value={promptValue}
              onChange={(event) => setPromptValue(event.target.value)}
            />
          ) : null}

          <div className="flex justify-end gap-2">
            {request.type !== 'alert' && options.cancelText ? (
              <Dialog.Close
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={handleCancel}
                  >
                    {options.cancelText}
                  </Button>
                )}
              />
            ) : null}
            <Button
              type="submit"
              variant={getConfirmVariant(request)}
              size="sm"
              autoFocus={request.type !== 'prompt'}
            >
              {options.confirmText}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

export default GlobalDialogHost;
