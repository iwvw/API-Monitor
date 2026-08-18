import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { cx } from '../ui/AppPrimitives.jsx';
import { Check, RefreshCw, Trash, Upload } from '../Icons.jsx';
import { dialog } from '../../modules/dialog.js';
import { toast } from '../../modules/toast.js';
import {
  deletePublicPageIcon,
  getPublicPageIconId,
  getPublicPageUploadedIconUrl,
  listPublicPageIcons,
  renderPublicPageDefaultIcon,
  uploadPublicPageIcon,
} from '../../modules/publicPageBranding.js';

const normalizeIconExt = (value) => {
  const ext = String(value || '').trim();
  if (!ext) return '';
  return ext.startsWith('.') ? ext : `.${ext}`;
};

const getIconDisplayName = (item) => {
  const rawName = String(item?.name || '').trim();
  const ext = normalizeIconExt(item?.ext);
  if (!rawName) {
    return `${String(item?.id || '图标').trim()}${ext}`;
  }
  if (!ext || rawName.toLowerCase().endsWith(ext.toLowerCase())) {
    return rawName;
  }
  return `${rawName}${ext}`;
};

const splitIconDisplayName = (value) => {
  const text = String(value || '').trim();
  if (!text) return { stem: '', ext: '' };
  const dotIndex = text.lastIndexOf('.');
  if (dotIndex > 0 && dotIndex < text.length - 1) {
    return {
      stem: text.slice(0, dotIndex),
      ext: text.slice(dotIndex),
    };
  }
  return {
    stem: text,
    ext: '',
  };
};

export function PublicPageBrandIcon({
  pageKind,
  config,
  iconClassName = 'h-5 w-5',
  customIconClassName,
}) {
  const iconId = getPublicPageIconId(config);
  const iconUrl = getPublicPageUploadedIconUrl(iconId);
  const [brokenIconUrl, setBrokenIconUrl] = useState('');

  useEffect(() => {
    setBrokenIconUrl('');
  }, [iconUrl]);

  if (iconUrl && brokenIconUrl !== iconUrl) {
    return (
      <img
        src={iconUrl}
        alt=""
        className={cx('object-contain', customIconClassName || iconClassName)}
        onError={() => setBrokenIconUrl(iconUrl)}
      />
    );
  }

  return renderPublicPageDefaultIcon(pageKind, { className: iconClassName });
}

function PublicPageIconOption({
  active = false,
  title,
  fullTitle,
  description,
  preview,
  previewFramed = true,
  onClick,
  onDelete,
  deleteBusy = false,
  disabled = false,
}) {
  const content = (
    <>
      <div
        className={cx(
          'flex h-12 w-12 shrink-0 items-center justify-center',
          previewFramed ? 'rounded-lg border border-kumo-line bg-kumo-base' : 'bg-transparent',
        )}
      >
        {preview}
      </div>
      <div className="min-w-0 flex-1">
        <div
          className="min-w-0 overflow-hidden pb-px text-sm font-semibold leading-5 text-kumo-strong"
          title={fullTitle || (typeof title === 'string' ? title : '')}
        >
          {title}
        </div>
        {description ? <div className="mt-1 text-xs leading-5 text-kumo-subtle">{description}</div> : null}
      </div>
      {active ? <Check className="h-4 w-4 shrink-0 text-kumo-brand" /> : null}
    </>
  );

  if (!onDelete) {
    return (
      <button
        type="button"
        onClick={onClick}
        disabled={disabled}
        className={cx(
          'flex w-full items-center gap-3 rounded-xl border px-3 py-3 text-left transition-colors',
          disabled ? 'cursor-not-allowed opacity-70' : 'hover:bg-kumo-recessed/35',
          active ? 'border-kumo-brand bg-kumo-brand/8' : 'border-kumo-line bg-kumo-base',
        )}
      >
        {content}
      </button>
    );
  }

  return (
    <div className="group flex items-stretch gap-2">
      <button
        type="button"
        onClick={onClick}
        disabled={disabled}
        className={cx(
          'flex min-w-0 flex-1 items-center gap-3 rounded-xl border px-3 py-3 text-left transition-colors',
          disabled ? 'cursor-not-allowed opacity-70' : 'hover:bg-kumo-recessed/35',
          active ? 'border-kumo-brand bg-kumo-brand/8' : 'border-kumo-line bg-kumo-base',
        )}
      >
        {content}
      </button>
      <div className="flex w-8 shrink-0 items-center justify-center">
        <Button
          shape="square"
          size="sm"
          variant="ghost"
          className={cx(
            'border border-kumo-line !bg-kumo-base shadow-sm transition-all',
            deleteBusy
              ? 'cursor-wait opacity-100'
              : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover:!border-kumo-danger/45 hover:!bg-kumo-danger/10 hover:!text-kumo-danger',
          )}
          onMouseDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onDelete();
          }}
          disabled={disabled || deleteBusy}
          loading={deleteBusy}
          aria-label="删除图标"
          title="删除图标"
        >
          {!deleteBusy && <Trash className="h-3.5 w-3.5" />}
        </Button>
      </div>
    </div>
  );
}

export default function PublicPageIconPicker({
  pageKind,
  config,
  isAuthenticated,
  onChange,
  triggerClassName,
  iconClassName = 'h-5 w-5',
  customIconClassName,
}) {
  const fileInputRef = useRef(null);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [deletingId, setDeletingId] = useState('');
  const selectedIconId = getPublicPageIconId(config);
  const hasCustomIcon = Boolean(selectedIconId);
  const resolvedCustomIconClassName = customIconClassName || 'h-8 w-8';

  const loadItems = async () => {
    setLoading(true);
    try {
      const nextItems = await listPublicPageIcons();
      setItems(nextItems);
      setLoaded(true);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!open || !isAuthenticated || loaded || loading) return;
    loadItems().catch((error) => toast.error(error.message || '加载图标失败'));
  }, [open, isAuthenticated, loaded, loading]);

  const saveChange = async (iconId, { closeOnSuccess = true } = {}) => {
    if (!onChange) return;
    if (iconId === selectedIconId) {
      if (closeOnSuccess) setOpen(false);
      return true;
    }
    setSaving(true);
    try {
      await onChange(iconId);
      if (closeOnSuccess) setOpen(false);
      return true;
    } catch (error) {
      toast.error(error.message || '保存图标失败');
      return false;
    } finally {
      setSaving(false);
    }
  };

  const commitChange = async (iconId) => saveChange(iconId);

  const handleUpload = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    setUploading(true);
    try {
      const item = await uploadPublicPageIcon(file);
      setItems((current) => [item, ...current.filter((entry) => entry.id !== item.id)]);
      setLoaded(true);
      await commitChange(item.id || '');
    } catch (error) {
      toast.error(error.message || '上传图标失败');
    } finally {
      setUploading(false);
      if (event.target) event.target.value = '';
    }
  };

  const handleDelete = async (item) => {
    const iconId = String(item?.id || '').trim();
    if (!iconId || deletingId) return;
    const fullName = getIconDisplayName(item);
    const confirmed = await dialog.confirm(`确定删除图标“${fullName || iconId}”吗？`);
    if (!confirmed) return;

    if (selectedIconId === iconId) {
      const restored = await saveChange('', { closeOnSuccess: false });
      if (!restored) return;
    }

    setDeletingId(iconId);
    try {
      await deletePublicPageIcon(iconId);
      await loadItems();
      toast.success('图标已删除');
    } catch (error) {
      toast.error(error.message || '删除图标失败');
    } finally {
      setDeletingId('');
    }
  };

  const trigger = isAuthenticated ? (
    <button
      type="button"
      className={cx(
        triggerClassName,
        hasCustomIcon
          ? 'border-transparent bg-transparent shadow-none'
          : 'transition-colors hover:border-brand/45',
      )}
      onClick={() => setOpen(true)}
      aria-label="更换公开页图标"
      title="更换公开页图标"
    >
      <PublicPageBrandIcon
        pageKind={pageKind}
        config={config}
        iconClassName={iconClassName}
        customIconClassName={resolvedCustomIconClassName}
      />
    </button>
  ) : (
    <div className={cx(triggerClassName, hasCustomIcon ? 'border-transparent bg-transparent shadow-none' : '')}>
      <PublicPageBrandIcon
        pageKind={pageKind}
        config={config}
        iconClassName={iconClassName}
        customIconClassName={resolvedCustomIconClassName}
      />
    </div>
  );

  return (
    <>
      {trigger}
      {isAuthenticated ? (
        <Dialog.Root open={open} onOpenChange={(nextOpen) => { if (!saving && !uploading && !deletingId) setOpen(nextOpen); }}>
          <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),36rem)] !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
            <div className="flex items-start justify-between border-b border-kumo-line px-4 py-3">
              <div>
                <Dialog.Title className="text-base font-semibold text-kumo-strong">公开页图标</Dialog.Title>
                <Dialog.Description className="mt-1 text-xs leading-relaxed text-kumo-subtle">
                  只影响当前公开页，可随时恢复默认。
                </Dialog.Description>
              </div>
              <Dialog.Close />
            </div>
            <div className="flex flex-wrap items-center gap-2 border-b border-kumo-line px-4 py-3">
              <input
                ref={fileInputRef}
                type="file"
                accept=".svg,.png,.jpg,.jpeg,.gif,.webp,image/svg+xml,image/png,image/jpeg,image/gif,image/webp"
                className="hidden"
                onChange={handleUpload}
              />
              <Button
                size="sm"
                variant="secondary"
                onClick={() => fileInputRef.current?.click()}
                loading={uploading}
                icon={<Upload className="h-4 w-4" />}
              >
                上传图标
              </Button>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => commitChange('')}
                disabled={!selectedIconId || saving}
              >
                恢复默认
              </Button>
              <Button
                size="sm"
                variant="secondary"
                shape="square"
                onClick={() => loadItems().catch((error) => toast.error(error.message || '加载图标失败'))}
                loading={loading}
                icon={<RefreshCw className="h-4 w-4" />}
                aria-label="刷新图标列表"
                title="刷新图标列表"
              />
            </div>
            <div className="overflow-y-auto p-4">
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <PublicPageIconOption
                  active={!selectedIconId}
                  title="默认图标"
                  description="恢复该公开页的默认图标。"
                  onClick={() => commitChange('')}
                  disabled={saving}
                  preview={<PublicPageBrandIcon pageKind={pageKind} config={{}} iconClassName="h-6 w-6" />}
                />
                {items.map((item) => {
                  const fullName = getIconDisplayName(item);
                  const { stem, ext } = splitIconDisplayName(fullName);
                  return (
                    <PublicPageIconOption
                      key={item.id}
                      active={selectedIconId === item.id}
                      title={(
                        <span className="flex min-w-0 items-center gap-0.5 overflow-hidden leading-5">
                          <span className="flex-1 min-w-0 truncate">{stem || item.id}</span>
                          {ext ? <span className="shrink-0">{ext}</span> : null}
                        </span>
                      )}
                      fullTitle={fullName || item.id}
                      onClick={() => commitChange(item.id || '')}
                      onDelete={() => handleDelete(item)}
                      deleteBusy={deletingId === item.id}
                      disabled={saving || deletingId === item.id}
                      previewFramed={false}
                      preview={<img src={item.publicUrl || item.url} alt="" className="h-11 w-11 object-contain" />}
                    />
                  );
                })}
              </div>
              {loading ? <div className="mt-3 text-xs text-kumo-subtle">正在加载已上传图标...</div> : null}
              {!loading && loaded && items.length === 0 ? (
                <div className="mt-3 rounded-lg border border-dashed border-kumo-line px-3 py-4 text-xs text-kumo-subtle">
                  还没有上传过图标。支持 svg/png/jpg/webp/gif，建议使用方形图标。
                </div>
              ) : null}
            </div>
          </Dialog>
        </Dialog.Root>
      ) : null}
    </>
  );
}
