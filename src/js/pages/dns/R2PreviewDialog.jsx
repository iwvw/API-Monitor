import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { ExternalLink, FileText } from '../../components/Icons.jsx';

function R2PreviewDialog({
  modal,
  onCloseModal,
}) {
  return (
    <div className="flex min-h-0 flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Dialog.Title className="truncate text-base font-semibold text-kumo-strong">预览：{modal.data?.name}</Dialog.Title>
          <Dialog.Description className="mt-1 truncate text-xs text-kumo-subtle">{modal.data?.key}</Dialog.Description>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => window.open(modal.data?.url, '_blank', 'noopener,noreferrer')} icon={<ExternalLink className="h-4 w-4" />}>
            新窗口打开
          </Button>
          <Button size="sm" variant="secondary" onClick={onCloseModal}>关闭</Button>
        </div>
      </div>

      <div className="min-h-[28rem] overflow-hidden rounded-md border border-kumo-line bg-kumo-recessed/25">
        {modal.data?.kind === 'image' ? (
          <div className="flex h-[68vh] max-h-[42rem] min-h-[28rem] items-center justify-center overflow-auto p-3">
            <img src={modal.data.url} alt={modal.data.name} className="max-h-full max-w-full object-contain" />
          </div>
        ) : modal.data?.kind === 'video' ? (
          <div className="flex h-[68vh] max-h-[42rem] min-h-[28rem] items-center justify-center p-3">
            <video src={modal.data.url} controls className="max-h-full max-w-full rounded-md bg-black" />
          </div>
        ) : modal.data?.kind === 'audio' ? (
          <div className="flex h-44 items-center justify-center p-6">
            <audio src={modal.data.url} controls className="w-full max-w-xl" />
          </div>
        ) : modal.data?.kind === 'frame' ? (
          <iframe
            title={`预览 ${modal.data.name}`}
            src={modal.data.url}
            className="h-[68vh] max-h-[42rem] min-h-[28rem] w-full bg-kumo-base"
          />
        ) : (
          <div className="flex min-h-[28rem] flex-col items-center justify-center gap-3 p-8 text-center">
            <FileText className="h-8 w-8 text-kumo-subtle" />
              <div>
                <div className="font-medium text-kumo-strong">该类型暂不支持内嵌预览</div>
                <div className="mt-1 text-sm text-kumo-subtle">由浏览器按文件类型处理。</div>
              </div>
            <Button size="sm" onClick={() => window.open(modal.data?.url, '_blank', 'noopener,noreferrer')} icon={<ExternalLink className="h-4 w-4" />}>
              打开对象
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

export default R2PreviewDialog;
