import { FileText, FolderOpen } from '../../components/Icons.jsx';
import { formatFileSize } from '../../modules/utils.js';

export function EntryName({ entry }) {
  const isFile = entry.type === 'file';
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md border ${isFile ? 'border-brand/20 bg-brand/10 text-brand' : 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success'}`}>{isFile ? <FolderOpen className="h-4 w-4" /> : <FileText className="h-4 w-4" />}</div>
      <div className="min-w-0">
        <div className="truncate text-xs font-semibold text-kumo-strong">{isFile ? entry.originalName || entry.filename || '文件分享' : entry.preview || entry.content || '文本分享'}</div>
        <div className="mt-0.5 text-[11px] text-kumo-subtle">{isFile ? formatFileSize(entry.size || 0) : entry.textFormat === 'markdown' ? 'Markdown 内容' : '文本内容'}</div>
      </div>
    </div>
  );
}
