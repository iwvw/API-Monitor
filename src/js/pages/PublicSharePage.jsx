import React, { lazy, Suspense, useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { ClipboardText, Tabs } from '@cloudflare/kumo';
import { CodeFile, Download, Eye, FileText, FolderOpen, Home, LogIn } from '../components/Icons.jsx';
import useStore from '../store.js';
import { SectionCard } from '../components/ui/AppPrimitives.jsx';
import { fileboxDirectURL, fileboxDownloadEndpoint } from '../modules/fileboxLinks.js';
import { toast } from '../modules/toast.js';
import { formatDateTime, formatFileSize } from '../modules/utils.js';
import { renderMarkdown } from '../modules/markdown.js';
import { TOOL_TABS_PROPS } from '../modules/kumoTabs.js';

const CodeEditor = lazy(() => import('../components/ui/CodeEditor.jsx'));

const CONTENT_VIEW_TABS = [
  {
    value: 'rendered',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Eye className="h-3.5 w-3.5" />
        渲染
      </span>
    ),
  },
  {
    value: 'source',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <CodeFile className="h-3.5 w-3.5" />
        源码
      </span>
    ),
  },
];

function shareCodeFromPath() {
  const match = window.location.pathname.match(/^\/share\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

function saveBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename || 'filebox-download';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function formatExpiry(value) {
  return Number(value) === 0 ? '永久有效' : formatDateTime(value);
}

function PublicSharePage() {
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const code = useMemo(shareCodeFromPath, []);
  const [entry, setEntry] = useState(null);
  const [loading, setLoading] = useState(true);
  const [downloading, setDownloading] = useState(false);
  const [password, setPassword] = useState('');
  const [textPreview, setTextPreview] = useState('');
  const [contentView, setContentView] = useState('rendered');
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const res = await axios.get(`/api/filebox/public/${encodeURIComponent(code)}`);
        const nextEntry = res.data?.data || null;
        if (!cancelled) setEntry(nextEntry);
        if (nextEntry?.type === 'text' && !nextEntry.requiresPassword) {
          const contentRes = await axios.get(fileboxDownloadEndpoint(nextEntry.code), {
            responseType: 'blob',
          });
          const content = await contentRes.data.text();
          if (!cancelled) setTextPreview(content);
        }
      } catch (err) {
        if (!cancelled) setError(err.response?.data?.error || '分享不存在或已过期');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    if (code) load();
    else {
      setLoading(false);
      setError('分享码无效');
    }
    return () => {
      cancelled = true;
    };
  }, [code]);

  const fetchContent = async () => {
    if (!entry) return;
    setDownloading(true);
    setError('');
    try {
      const res = await axios.get(fileboxDownloadEndpoint(entry.code), {
        responseType: 'blob',
        headers: password ? { 'X-Filebox-Password': password } : {},
      });
      const filename =
        entry.originalName ||
        entry.filename ||
        (entry.type === 'text' ? `${entry.code}.md` : 'download');
      if (entry.type === 'text') {
        setTextPreview(await res.data.text());
        toast.success('文本已取回');
      } else {
        saveBlob(res.data, filename);
        toast.success('下载已开始');
      }
    } catch (err) {
      setError(
        err.response?.status === 403
          ? '访问密码错误或缺失'
          : err.response?.data?.error || '取用失败'
      );
    } finally {
      setDownloading(false);
    }
  };

  const isFile = entry?.type === 'file';
  const isMarkdown = !isFile;
  const title = isFile
    ? entry?.originalName || entry?.filename || '文件分享'
    : isMarkdown
      ? 'Markdown 分享'
      : '文本分享';
  const Icon = isFile ? FolderOpen : FileText;
  const displayedText = textPreview || entry?.preview || '';
  const displayedSize =
    Number(entry?.size) > 0
      ? Number(entry.size)
      : !isFile && textPreview
        ? new Blob([textPreview]).size
        : 0;

  return (
    <div className="h-screen h-dvh overflow-hidden bg-kumo-canvas p-4 text-kumo-default sm:p-6">
      <div className="mx-auto flex h-full w-full max-w-5xl min-h-0 flex-col gap-4">
        <div className="flex shrink-0 items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="text-base font-bold text-kumo-strong">
              {entry ? (isFile ? '文件分享' : '文本分享') : '分享内容'}
            </div>
            <div className="mt-1 font-mono text-xs text-kumo-subtle">{code || '-'}</div>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={entry?.requiresPassword ? 'warning' : 'secondary'}>
              {entry?.requiresPassword ? '需要密码' : '公开'}
            </Badge>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => { window.location.href = '/'; }}
              icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
              aria-label={isAuthenticated ? '主页' : '登录'}
              title={isAuthenticated ? '跳转到主页' : '跳转到登录页'}
            >
              {isAuthenticated ? '主页' : '登录'}
            </Button>
          </div>
        </div>

        <SectionCard
          title={loading ? '正在读取分享' : title}
          icon={<Icon className="h-4 w-4 text-brand" />}
          className="min-h-0 flex-1"
          bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
        >
          {loading ? (
            <div className="py-10 text-center text-sm text-kumo-subtle">读取中</div>
          ) : error && !entry ? (
            <div className="rounded-md border border-kumo-error/30 bg-kumo-error/10 p-4 text-sm font-semibold text-kumo-error">
              {error}
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-4">
              <div className="grid shrink-0 gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3 text-xs sm:grid-cols-2">
                <div>
                  <span className="text-kumo-subtle">类型</span>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {isFile ? '文件' : isMarkdown ? 'Markdown' : '文本'}
                  </div>
                </div>
                <div>
                  <span className="text-kumo-subtle">大小</span>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {formatFileSize(displayedSize)}
                  </div>
                </div>
                <div>
                  <span className="text-kumo-subtle">到期</span>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {formatExpiry(entry?.expiry)}
                  </div>
                </div>
                <div>
                  <span className="text-kumo-subtle">下载次数</span>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {entry?.downloads || 0}
                    {entry?.maxDownloads ? ` / ${entry.maxDownloads}` : ' / 不限'}
                  </div>
                </div>
              </div>

              {!isFile && displayedText ? (
                <div className="flex min-h-0 flex-1 flex-col rounded-md border border-kumo-line bg-kumo-base p-3">
                  <div className="mb-2 flex shrink-0 flex-wrap items-center justify-between gap-2">
                    <div className="text-xs font-semibold text-kumo-strong">
                      {textPreview ? 'Markdown 内容' : '内容预览'}
                    </div>
                    <div className="flex flex-wrap items-center justify-end gap-2">
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        value={contentView}
                        onValueChange={setContentView}
                        tabs={CONTENT_VIEW_TABS}
                      />
                      {textPreview ? (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() =>
                            navigator.clipboard
                              .writeText(textPreview)
                              .then(() => toast.success('内容已复制'))
                          }
                        >
                          复制
                        </Button>
                      ) : null}
                    </div>
                  </div>
                  {contentView === 'source' ? (
                    <Suspense
                      fallback={
                        <div className="flex min-h-0 flex-1 items-center justify-center rounded-md border border-kumo-line text-xs text-kumo-subtle">
                          正在加载源码
                        </div>
                      }
                    >
                      <CodeEditor
                        value={displayedText}
                        fileName="shared.md"
                        language="markdown"
                        label="Markdown 源码"
                        readOnly
                        className="public-share-source"
                        minHeight="0"
                        showHeader={false}
                        showLanguage={false}
                        lineWrapping
                      />
                    </Suspense>
                  ) : (
                    <div
                      className="app-markdown-rendered min-h-0 flex-1 overflow-y-auto overscroll-contain"
                      dangerouslySetInnerHTML={{ __html: renderMarkdown(displayedText) }}
                    />
                  )}
                </div>
              ) : null}

              {entry?.requiresPassword && (
                <Input
                  size="sm"
                  label="访问密码"
                  type="text"
                  value={password}
                  onChange={event => setPassword(event.target.value)}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                />
              )}

              {error && (
                <div className="rounded-md border border-kumo-error/30 bg-kumo-error/10 p-3 text-xs font-semibold text-kumo-error">
                  {error}
                </div>
              )}

              <div className="mt-auto flex shrink-0 flex-wrap items-center justify-between gap-3">
                <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(14rem,0.72fr)]">
                  <div className="min-w-0">
                    <div className="mb-1 text-[11px] text-kumo-subtle">分享链接</div>
                    <ClipboardText
                      text={window.location.href}
                      tooltip={{ text: '复制分享链接', copiedText: '分享链接已复制' }}
                      labels={{ copyAction: '复制分享链接' }}
                    />
                  </div>
                  <div className="min-w-0">
                    <div className="mb-1 text-[11px] text-kumo-subtle">直链（源码）</div>
                    <ClipboardText
                      text={fileboxDirectURL(entry.code)}
                      tooltip={{ text: '复制直链', copiedText: '直链已复制' }}
                      labels={{ copyAction: '复制直链' }}
                    />
                  </div>
                </div>
                {isFile || !textPreview ? (
                  <Button
                    size="sm"
                    variant="primary"
                    loading={downloading}
                    onClick={fetchContent}
                    icon={<Download className="h-4 w-4" />}
                  >
                    {isFile ? '下载文件' : isMarkdown ? '查看 Markdown' : '查看文本'}
                  </Button>
                ) : null}
              </div>
            </div>
          )}
        </SectionCard>
      </div>
    </div>
  );
}

export default PublicSharePage;
