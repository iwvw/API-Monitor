import { lazy, Suspense } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { ClipboardText, Meter, Tabs } from '@cloudflare/kumo';
import { formatFileSize } from '../../modules/utils.js';
import { fileboxDirectURL, fileboxShareURL } from '../../modules/fileboxLinks.js';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { FileText, Send, Upload } from '../../components/Icons.jsx';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { EXPIRY_OPTIONS } from './constants.js';
import { SHARE_TYPE_TABS } from './tabs.jsx';
import { expiryLabel } from './utils.js';

const MarkdownEditor = lazy(() => import('../../components/ui/MarkdownEditor.jsx'));

export function SharePanel({
  shareType,
  setShareType,
  shareText,
  setShareText,
  selectedFile,
  fileInputRef,
  maxFileSize,
  loading,
  uploadProgress,
  uploadSpeed,
  selectedNodeId,
  setSelectedNodeId,
  storageNodes,
  expiry,
  setExpiry,
  maxDownloads,
  setMaxDownloads,
  accessPassword,
  setAccessPassword,
  burnAfterReading,
  setBurnAfterReading,
  result,
  setResult,
  qrCode,
  abortControllerRef,
  selectFile,
  resetShare,
  createShare,
}) {
  return (
    <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1fr)_minmax(22rem,1fr)]">
      <SectionCard
        title="创建分享"
        icon={<Send className="h-4 w-4 text-brand" />}
        action={
          <Tabs
            {...TOOL_TABS_PROPS}
            value={shareType}
            onValueChange={(value) => {
              setShareType(value);
              setResult(null);
            }}
            tabs={SHARE_TYPE_TABS}
          />
        }
        bodyClassName="grid gap-4"
      >
        {shareType === 'file' ? (
          <div
            className="rounded-md border border-dashed border-kumo-line bg-kumo-recessed/35 p-6 text-center"
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              selectFile(event.dataTransfer.files?.[0]);
            }}
          >
            <Input ref={fileInputRef} type="file" aria-label="选择分享文件" className="hidden" onChange={(event) => selectFile(event.target.files?.[0])} />
            <Upload className="mx-auto h-9 w-9 text-kumo-subtle" />
            <div className="mt-3 text-sm font-semibold text-kumo-strong">{selectedFile ? selectedFile.name : '拖入文件或点击选择'}</div>
            <div className="mt-1 text-xs text-kumo-subtle">最大 {formatFileSize(maxFileSize)}</div>
            <div className="mt-4 flex justify-center">
              <Button size="sm" onClick={() => fileInputRef.current?.click()}>
                选择文件
              </Button>
            </div>
          </div>
        ) : (
          <Suspense fallback={<div className="flex min-h-72 items-center justify-center rounded-md border border-kumo-line text-xs text-kumo-subtle">正在加载文本编辑器</div>}>
            <MarkdownEditor
              label="分享文本"
              value={shareText}
              onChange={(value) => {
                setShareText(value);
                setResult(null);
              }}
              minHeight="18rem"
              placeholder="输入或粘贴文本内容"
            />
          </Suspense>
        )}

        {loading && shareType === 'file' && <Meter label="上传进度" value={uploadProgress} customValue={`${uploadProgress}% · ${uploadSpeed}`} />}

        <div className="grid gap-3 cq-md:grid-cols-2">
          {shareType === 'file' && (
            <div className="cq-md:col-span-2">
              <Select alignItemWithTrigger
                size="sm"
                label="存储位置"
                value={selectedNodeId}
                onValueChange={setSelectedNodeId}
                items={[
                  { value: 'local', label: '主站本地存储' },
                  ...storageNodes.map((n) => ({
                    value: n.id,
                    label: `${n.name || n.id} (${n.host}:${n.storagePort || 61208})`,
                  })),
                ]}
              />
              <div className="mt-1 text-[11px] text-kumo-subtle">
                选择边缘节点时，文件字节流将由浏览器直传至该节点，主站不中转流量。
              </div>
            </div>
          )}
          <Select alignItemWithTrigger size="sm" label="有效期" value={expiry} onValueChange={setExpiry} items={EXPIRY_OPTIONS} />
          <Input size="sm" label="最大下载次数" type="number" min="0" value={maxDownloads} onChange={(event) => setMaxDownloads(event.target.value)} placeholder="0 或留空为不限" />
          <Input size="sm" label="访问密码" type="text" value={accessPassword} onChange={(event) => setAccessPassword(event.target.value)} placeholder="可选" autoComplete="off" data-1p-ignore data-lpignore="true" data-bwignore="true" data-form-type="other" spellCheck={false} />
          <div className="flex items-center justify-between rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2">
            <div>
              <div className="text-xs font-semibold text-kumo-strong">阅后即焚</div>
              <div className="text-[11px] text-kumo-subtle">首次成功下载后删除</div>
            </div>
            <Switch checked={burnAfterReading} onCheckedChange={setBurnAfterReading} />
          </div>
        </div>

        <div className="flex flex-wrap justify-end gap-2 border-t border-kumo-line pt-4">
          {loading && (
            <Button size="sm" variant="secondary-destructive" onClick={() => abortControllerRef.current?.abort()}>
              取消上传
            </Button>
          )}
          <Button size="sm" variant="secondary" onClick={resetShare}>
            重置
          </Button>
          <Button size="sm" variant="primary" onClick={createShare} loading={loading} icon={<Send className="h-4 w-4" />}>
            创建分享
          </Button>
        </div>
      </SectionCard>

      <SectionCard title="分享结果" icon={<FileText className="h-4 w-4 text-brand" />}>
        {!result ? (
          <div className="space-y-3">
            <div className="rounded-md border border-dashed border-kumo-line p-8 text-center text-xs text-kumo-subtle">显示链接、二维码和取用信息。</div>
            <div className="grid gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs">
              <div className="flex justify-between gap-3">
                <span className="text-kumo-subtle">类型</span>
                <span className="font-semibold text-kumo-strong">{shareType === 'file' ? '文件' : '文本'}</span>
              </div>
              <div className="flex justify-between gap-3">
                <span className="text-kumo-subtle">有效期</span>
                <span className="font-semibold text-kumo-strong">{expiryLabel(expiry)}</span>
              </div>
              <div className="flex justify-between gap-3">
                <span className="text-kumo-subtle">下载次数</span>
                <span className="font-semibold text-kumo-strong">{maxDownloads || '不限'}</span>
              </div>
              <div className="flex justify-between gap-3">
                <span className="text-kumo-subtle">访问密码</span>
                <span className="font-semibold text-kumo-strong">{accessPassword ? '已设置' : '未设置'}</span>
              </div>
            </div>
          </div>
        ) : (
          <div className="mt-4 space-y-4">
				<div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/35 p-3 cq-sm:grid-cols-2">
				  <div className="min-w-0">
					<div className="text-xs text-kumo-subtle">分享链接</div>
					<ClipboardText text={fileboxShareURL(result.code)} className="mt-2" tooltip={{ text: '复制分享链接', copiedText: '分享链接已复制' }} labels={{ copyAction: '复制分享链接' }} />
				  </div>
				  <div className="min-w-0">
					<div className="text-xs text-kumo-subtle">直链（源码）</div>
					<ClipboardText text={fileboxDirectURL(result.code)} className="mt-2" tooltip={{ text: '复制直链', copiedText: '直链已复制' }} labels={{ copyAction: '复制直链' }} />
				  </div>
            </div>
            <div className="grid gap-3 cq-sm:grid-cols-[auto_minmax(0,1fr)] cq-sm:items-center">
              {qrCode && <img src={qrCode} alt="分享二维码" className="h-32 w-32 rounded-md border border-kumo-line bg-white p-2" />}
              <div className="space-y-2 text-xs text-kumo-subtle">
                <div>
                  <span className="font-semibold text-kumo-strong">分享码:</span> <span className="font-mono text-brand">{result.code}</span>
                </div>
                <div>链接可直接打开；需要密码时浏览器会提示输入。</div>
                <div>有效期: {expiryLabel(expiry)}</div>
                {accessPassword && <Badge variant="warning">已启用访问密码</Badge>}
              </div>
            </div>
          </div>
        )}
      </SectionCard>
    </div>
  );
}
