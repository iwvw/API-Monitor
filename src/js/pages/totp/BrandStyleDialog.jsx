import React from 'react';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { LayerCard } from '@cloudflare/kumo';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { Trash, Upload, X } from '../../components/Icons.jsx';
import TotpBrandMark from './TotpBrandMark.jsx';

const BrandStyleDialog = ({
  showBrandStyleModal,
  setShowBrandStyleModal,
  brandUploadInputRef,
  handleCustomBrandIconUpload,
  customBrandIconUploading,
  handlePasteBrandIconFromClipboard,
  handleBrandLibraryPaste,
  customBrandIconsLoading,
  brandStyleOptions,
  resolveFormColor,
  accountForm,
  applyBrandStyleOption,
  isArmed,
  deletingCustomBrandIconId,
  deleteCustomBrandIcon,
}) => {
  return (
    <Dialog.Root open={showBrandStyleModal} onOpenChange={setShowBrandStyleModal}>
      <Dialog size="xl" className="@container flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden p-0">
        <div className="flex items-start justify-between gap-4 border-b border-kumo-line px-5 py-4">
          <div className="min-w-0">
            <Dialog.Title className="text-base font-semibold text-kumo-strong">
              选择品牌标识样式
            </Dialog.Title>
            <Dialog.Description className="mt-1 text-xs leading-5 text-kumo-subtle">
              选择系统样式或管理自定义图标；保存账号后同步到同发行商账号。
            </Dialog.Description>
          </div>
          <Dialog.Close
            render={props => (
              <Button
                {...props}
                size="sm"
                shape="square"
                variant="ghost"
                icon={<X className="size-4" />}
                aria-label="关闭"
              />
            )}
          />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-5 scrollbar-thin">
          <LayerCard className="mb-4 p-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="text-[11px] leading-5 text-kumo-subtle">
                支持 SVG、PNG、JPG、WebP 和 GIF，也可粘贴图片、SVG 源码或图片链接。
              </div>
              <div className="flex items-center gap-2">
                <input
                  ref={brandUploadInputRef}
                  type="file"
                  accept=".svg,image/svg+xml,image/png,image/jpeg,image/webp,image/gif"
                  className="hidden"
                  onChange={handleCustomBrandIconUpload}
                />
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => brandUploadInputRef.current?.click()}
                  loading={customBrandIconUploading}
                >
                  <Upload className="w-3.5 h-3.5" />
                  上传自定义图标
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={handlePasteBrandIconFromClipboard}
                  loading={customBrandIconUploading}
                >
                  粘贴图标
                </Button>
                <LinkButton
                  size="sm"
                  variant="secondary"
                  href="https://www.svgrepo.com/"
                  target="_blank"
                  rel="noreferrer"
                >
                  打开 SVG Repo
                </LinkButton>
              </div>
            </div>
          </LayerCard>

          <div
            tabIndex={0}
            onPaste={handleBrandLibraryPaste}
            className="mb-3 rounded-md border border-dashed border-kumo-line bg-kumo-recessed/15 px-3 py-2.5 text-[11px] text-kumo-subtle outline-none transition-colors focus:border-kumo-brand focus:ring-2 focus:ring-kumo-brand/20"
          >
            选中这里后按 `Ctrl+V`，可以直接粘贴截图、图标文件、SVG
            源码或图片链接，并自动下载应用。
          </div>

          <div className="grid grid-cols-1 items-start gap-2 cq-sm:grid-cols-2">
            {!customBrandIconsLoading && brandStyleOptions.length === 0 && (
              <div className="col-span-full rounded-md border border-kumo-line bg-kumo-recessed/20 px-3 py-6 text-center text-xs text-kumo-subtle">
                当前还没有可选图标。
              </div>
            )}
            {brandStyleOptions.map(option => {
              const canDelete = option.source === 'custom' && option.customId;
              return (
                <LayerCard
                  key={option.id}
                  className={cx(
                    'grid min-w-0 items-center gap-2 self-start p-1.5',
                    canDelete ? 'grid-cols-[minmax(0,1fr)_auto]' : 'grid-cols-1'
                  )}
                >
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() => applyBrandStyleOption(option)}
                    className="!h-auto min-w-0 w-full justify-start px-2 py-1.5 text-left"
                  >
                    <span className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
                      <TotpBrandMark
                        issuer={accountForm.issuer}
                        icon={option.icon}
                        color={option.color || resolveFormColor(accountForm)}
                        size="picker"
                      />
                      <span className="min-w-0">
                        <span className="block truncate text-xs font-semibold text-kumo-strong">
                          {option.label}
                        </span>
                        <span className="block truncate text-[11px] text-kumo-subtle">
                          {option.caption}
                        </span>
                      </span>
                    </span>
                  </Button>
                  {canDelete && (
                    <Button
                      type="button"
                      size="sm"
                      shape="square"
                      variant={
                        isArmed(`custom-icon-${option.customId}`)
                          ? 'destructive'
                          : 'secondary-destructive'
                      }
                      icon={<Trash className="size-4" />}
                      aria-label={`删除 ${option.label}`}
                      title={
                        isArmed(`custom-icon-${option.customId}`)
                          ? '再次点击确认删除'
                          : `删除 ${option.label}`
                      }
                      loading={deletingCustomBrandIconId === option.customId}
                      disabled={Boolean(deletingCustomBrandIconId)}
                      onClick={() => deleteCustomBrandIcon(option)}
                    />
                  )}
                </LayerCard>
              );
            })}
          </div>
        </div>

        <div className="flex shrink-0 justify-end gap-2 border-t border-kumo-line px-5 py-4">
          <Dialog.Close
            render={props => (
              <Button size="sm" {...props} variant="secondary">
                取消
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
};

export default BrandStyleDialog;
