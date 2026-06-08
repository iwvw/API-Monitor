import React, { useEffect, useMemo, useState } from 'react';
import { DeleteResource } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { cancelDialog, resolveDialog, subscribeDialog } from '../modules/dialog.js';
import { X } from './Icons.jsx';

const QUOTED_RESOURCE_PATTERN = /[“"「『‘']([^”"」』’']+)[”"」』’']/;

const getConfirmVariant = (request) => {
  const options = request?.options || {};
  const marker = `${options.confirmClass || ''} ${options.variant || ''} ${options.type || ''}`.toLowerCase();
  return marker.includes('danger') || marker.includes('destructive')
    ? 'destructive'
    : 'primary';
};

const isDeleteResourceConfirm = (request) => {
  if (request?.type !== 'confirm') return false;
  const options = request.options || {};
  if (options.deleteResource === false) return false;
  return options.deleteResource === true || !!options.resourceName || !!options.resourceType;
};

const getDeleteResourceName = (options) => {
  if (options.resourceName) return String(options.resourceName);

  const message = String(options.message || '');
  const quotedMatch = message.match(QUOTED_RESOURCE_PATTERN);
  if (quotedMatch?.[1]) return quotedMatch[1].trim();

  const namedMatch = message.match(/(?:删除|移除|销毁|永久删除)\s+(.+?)\s+(?:的|吗|？|\?|$)/);
  if (namedMatch?.[1]) return namedMatch[1].trim();

  const selectedMatch = message.match(/选中的\s*(.+?)\s*(?:吗|？|\?|$)/);
  if (selectedMatch?.[1]) return selectedMatch[1].trim();

  return options.confirmationText || 'DELETE';
};

const getDeleteResourceType = (options, resourceName) => {
  if (options.resourceType) return String(options.resourceType);
  const message = String(options.message || '');
  const typeMatch = message.match(/删除\s*(?:此|该|这个|这台|选中的\s*\d+\s*(?:个|条)?)?\s*([A-Za-z0-9 ._-]*[\u4e00-\u9fa5A-Za-z0-9 ._-]{1,16})/);
  if (typeMatch?.[1]) {
    const type = typeMatch[1]
      .replace(resourceName, '')
      .replace(/[“"「『‘'].*$/, '')
      .replace(/吗.*$/, '')
      .replace(/的.*$/, '')
      .trim();
    if (type) return type;
  }
  return '资源';
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

  if (isDeleteResourceConfirm(request)) {
    const resourceName = getDeleteResourceName(options);
    const resourceType = getDeleteResourceType(options, resourceName);

    return (
      <DeleteResource
        open
        onOpenChange={(open) => {
          if (!open) handleCancel();
        }}
        resourceType={resourceType}
        resourceName={resourceName}
        onDelete={() => resolveDialog(true)}
        caseSensitive={false}
        deleteButtonText={options.confirmText || '删除'}
        size={options.size || 'sm'}
      />
    );
  }

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
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  onClick={handleCancel}
                />
              )}
            />
          </div>

          {request.type === 'prompt' ? (
            <Input size="sm"
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
                    variant="secondary" size="sm"
                    onClick={handleCancel}
                  >
                    {options.cancelText}
                  </Button>
                )}
              />
            ) : null}
            <Button
              type="submit"
              variant={getConfirmVariant(request)} size="sm"
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
