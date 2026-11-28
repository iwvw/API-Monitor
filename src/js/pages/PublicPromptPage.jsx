import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Loader } from '@cloudflare/kumo/components/loader';
import { Copy, ExternalLink } from '../components/PublicIcons.jsx';
import { iconButtonIconClass } from '../components/ui/CardPrimitives.jsx';
import { renderMarkdown } from '../modules/markdown.js';
import { toast } from '../modules/toast.js';

export default function PublicPromptPage() {
  const publicId = useMemo(() => window.location.pathname.split('/').filter(Boolean)[1] || '', []);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch(`/api/prompts/public/${encodeURIComponent(publicId)}`, { cache: 'no-store' })
      .then(async response => {
        if (!response.ok) throw new Error(response.status === 404 ? '提示词不存在或尚未发布' : `HTTP ${response.status}`);
        return response.json();
      })
      .then(setData)
      .catch(reason => setError(reason.message));
  }, [publicId]);

  if (error) return <main className="flex min-h-dvh items-center justify-center bg-kumo-canvas p-6 text-sm text-kumo-subtle">{error}</main>;
  if (!data) return <main className="flex min-h-dvh items-center justify-center bg-kumo-canvas"><Loader /></main>;

  const directUrl = `${window.location.origin}/api/prompts/d/${publicId}?format=markdown`;
  return (
    <main className="min-h-dvh bg-kumo-canvas text-kumo-default">
      <header className="border-b border-kumo-line bg-kumo-base">
        <div className="mx-auto flex max-w-4xl flex-wrap items-center justify-between gap-3 px-5 py-4">
          <div className="min-w-0"><h1 className="truncate text-lg font-semibold text-kumo-strong">{data.title}</h1><div className="mt-1 text-xs text-kumo-subtle">版本 {data.version_no} · {data.published_at?.slice(0, 16)}</div></div>
          <div className="flex gap-2"><Button size="sm" variant="secondary" icon={<Copy className={iconButtonIconClass} />} onClick={async () => { await navigator.clipboard.writeText(data.content_md); toast.success('内容已复制'); }}>复制内容</Button><Button size="sm" variant="primary" icon={<ExternalLink className={iconButtonIconClass} />} onClick={async () => { await navigator.clipboard.writeText(directUrl); toast.success('原始直链已复制'); }}>复制直链</Button></div>
        </div>
      </header>
      <article className="markdown-content mx-auto max-w-4xl px-5 py-8" dangerouslySetInnerHTML={{ __html: renderMarkdown(data.content_md) }} />
    </main>
  );
}
