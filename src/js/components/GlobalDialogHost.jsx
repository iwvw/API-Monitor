import React, { useEffect, useState } from 'react';
import { Banner, ClipboardText } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { cancelDialog, resolveDialog, subscribeDialog } from '../modules/dialog.js';
import { LayerDialog } from './kumo/LayerDialog.jsx';

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
  return `此操作无法撤销，将永久移除 ${resourceType}“${resourceName}”。`;
};

function DeleteResourceDialog({ options, promptValue, setPromptValue, onCancel }) {
  const resourceName = getDeleteResourceName(options);
  const resourceType = getDeleteResourceType(options);
  const canDelete = normalizeText(promptValue) === normalizeText(resourceName);

  return (
    <LayerDialog.Alert
      open
      dismissDisabled={options.disablePointerDismissal}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <LayerDialog.Content size={options.size || 'base'}>
        <LayerDialog.Title>{`删除 ${resourceName}`}</LayerDialog.Title>
        <LayerDialog.Description>
          {getDeleteDescription(options, resourceName, resourceType)}
        </LayerDialog.Description>
        <LayerDialog.Body>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (canDelete) resolveDialog(true);
            }}
          >
            {options.errorMessage ? (
              <Banner variant="error" title={options.errorMessage} />
            ) : null}

            <div className="space-y-2">
              <div className="text-sm text-kumo-default">
                输入以下内容确认删除：
              </div>
              <ClipboardText
                size="sm"
                text={resourceName}
                className="w-full"
                tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }}
                labels={{ copyAction: `复制 ${resourceName}` }}
              />
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
          </form>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="submit"
            variant="destructive"
            disabled={!canDelete}
            onClick={() => {
              if (canDelete) resolveDialog(true);
            }}
          >
            {options.confirmText || '删除'}
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Alert>
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

  const isAlert = request.type === 'alert';

  return (
    <LayerDialog.Root
      open
      dismissDisabled={options.disablePointerDismissal}
      onOpenChange={(open) => {
        if (!open) handleCancel();
      }}
    >
      <LayerDialog.Content size={options.size || 'sm'}>
        <LayerDialog.Title>{options.title}</LayerDialog.Title>
        <LayerDialog.Body>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              handleConfirm();
            }}
          >
            {options.message ? (
              <div className="whitespace-pre-wrap text-sm leading-6 text-kumo-subtle">
                {options.message}
              </div>
            ) : null}
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
          </form>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel={options.cancelText || '取消'}>
          <LayerDialog.Actions.Primary
            type="button"
            variant={getConfirmVariant(request)}
            autoFocus={!isAlert && request.type !== 'prompt'}
            onClick={handleConfirm}
          >
            {options.confirmText}
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default GlobalDialogHost;
