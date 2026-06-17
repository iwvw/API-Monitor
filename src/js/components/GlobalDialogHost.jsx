import React, { useEffect, useState } from 'react';
import { Banner, ClipboardText } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { cancelDialog, resolveDialog, subscribeDialog } from '../modules/dialog.js';
import { X } from './Icons.jsx';

const QUOTE_PAIRS = [
  ['"', '"'],
  ["'", "'"],
  ['`', '`'],
  ['“', '”'],
  ['‘', '’'],
  ['「', '」'],
  ['『', '』'],
  ['《', '》'],
];

const normalizeText = (value) => String(value || '').trim().toLocaleLowerCase();

const extractQuotedText = (message) => {
  const text = String(message || '');
  for (const [open, close] of QUOTE_PAIRS) {
    const start = text.indexOf(open);
    if (start < 0) continue;
    const end = text.indexOf(close, start + open.length);
    if (end <= start + open.length) continue;
    const extracted = text.slice(start + open.length, end).trim();
    if (extracted) return extracted;
  }
  return '';
};

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
  return options.deleteResource === true || Boolean(options.resourceName) || Boolean(options.resourceType);
};

const getDeleteResourceName = (options = {}) => {
  if (options.resourceName) return String(options.resourceName).trim();

  const quotedName = extractQuotedText(options.message);
  if (quotedName) return quotedName;

  if (options.confirmationText) return String(options.confirmationText).trim();
  if (options.confirmText) return String(options.confirmText).trim();
  return 'DELETE';
};

const getDeleteResourceType = (options = {}) => {
  if (options.resourceType) return String(options.resourceType).trim();
  return '资源';
};

const getDeleteDescription = (options, resourceName, resourceType) => {
  const explicitMessage = String(options?.message || '').trim();
  if (explicitMessage) return explicitMessage;
  return `此操作无法撤销。删除后，将永久移除 ${resourceType}“${resourceName}”。`;
};

function DeleteResourceDialog({ options, promptValue, setPromptValue, onCancel }) {
  const resourceName = getDeleteResourceName(options);
  const resourceType = getDeleteResourceType(options);
  const canDelete = normalizeText(promptValue) === normalizeText(resourceName);

  return (
    <Dialog.Root
      open
      role="alertdialog"
      disablePointerDismissal={options.disablePointerDismissal}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <Dialog size={options.size || 'sm'} className="p-0">
        <form
          className="flex flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            if (canDelete) resolveDialog(true);
          }}
        >
          <div className="flex items-center justify-between gap-4 border-b border-kumo-line px-5 py-4">
            <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">
              删除 {resourceName}
            </Dialog.Title>
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
                  onClick={onCancel}
                />
              )}
            />
          </div>

          <div className="flex flex-col gap-4 px-5 py-4">
            {options.errorMessage ? (
              <Banner variant="error" title={options.errorMessage} />
            ) : null}

            <Dialog.Description className="text-sm leading-6 text-kumo-subtle">
              {getDeleteDescription(options, resourceName, resourceType)}
            </Dialog.Description>

            <div className="space-y-2">
              <div className="text-sm text-kumo-default">
                请输入下方内容以确认删除：
              </div>
              <ClipboardText
                size="sm"
                text={resourceName}
                className="w-full"
                tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }}
                labels={{ copyAction: `复制 ${resourceName}` }}
              />
            </div>

            <Input
              size="sm"
              autoFocus
              aria-label={`请输入 ${resourceName} 进行确认`}
              placeholder={resourceName}
              value={promptValue}
              onChange={(event) => setPromptValue(event.target.value)}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
              spellCheck={false}
            />
          </div>

          <div className="flex justify-end gap-2 border-t border-kumo-line px-5 py-4">
            <Dialog.Close
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={onCancel}
                >
                  取消
                </Button>
              )}
            />
            <Button
              type="submit"
              variant="destructive"
              size="sm"
              disabled={!canDelete}
            >
              {options.confirmText || '删除'}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

function GlobalDialogHost() {
  const [request, setRequest] = useState(null);
  const [promptValue, setPromptValue] = useState('');

  useEffect(() => subscribeDialog(setRequest), []);

  useEffect(() => {
    setPromptValue(request?.options?.defaultValue || '');
  }, [request?.id, request?.options?.defaultValue]);

  if (!request || !request.options) return null;

  const options = request.options;
  const role = options.role || (request.type === 'alert' ? 'dialog' : 'alertdialog');

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
    return (
      <DeleteResourceDialog
        options={options}
        promptValue={promptValue}
        setPromptValue={setPromptValue}
        onCancel={handleCancel}
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
              size="sm"
              autoFocus
              aria-label={options.placeholder || options.title || '输入框'}
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
