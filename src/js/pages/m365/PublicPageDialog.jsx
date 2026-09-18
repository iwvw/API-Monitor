import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { ChevronDown } from '../../components/Icons.jsx';
import { cx } from '../../components/ui/AppPrimitives.jsx';
import { getSkuDisplayLabel } from './utils.js';

export default function PublicPageDialog({
  open,
  onOpenChange,
  publicPageForm,
  setPublicPageForm,
  accounts,
  getPublicAccountDomainList,
  skus,
  submitPublicPage,
  submittingPublicPage,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>{publicPageForm.id ? '编辑公开页' : '新建公开页'}</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="@container grid gap-4 cq-lg:grid-cols-[minmax(0,1fr)_minmax(18rem,22rem)]">
            <div className="grid gap-3">
              <Input
                size="sm"
                aria-label="公开页名称"
                value={publicPageForm.name}
                onChange={event =>
                  setPublicPageForm(current => ({ ...current, name: event.target.value }))
                }
                placeholder="例如：学生自助开通"
              />
              <div className="rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                <div className="text-sm font-medium text-kumo-strong">目标租户与域名</div>
                <div className="text-xs text-kumo-subtle">
                  先勾选租户，再展开域名做选择。未取消的域名都会允许注册。
                </div>
                <div className="mt-3 grid gap-2">
                  {accounts.map(account => {
                    const normalizedId = String(account.id);
                    const checked = publicPageForm.accountIds.includes(normalizedId);
                    const accountDomains = getPublicAccountDomainList(account);
                    const selectedCount = accountDomains.filter(domain =>
                      publicPageForm.domains.includes(domain)
                    ).length;
                    return (
                      <div
                        key={account.id}
                        className="rounded-lg border border-kumo-line/70 bg-kumo-base/50 px-3 py-2.5"
                      >
                        <label className="flex min-w-0 items-center gap-2">
                          <Checkbox
                            checked={checked}
                            onCheckedChange={value => {
                              setPublicPageForm(current => {
                                const nextAccountIds = value
                                  ? current.accountIds.includes(normalizedId)
                                    ? current.accountIds
                                    : [...current.accountIds, normalizedId]
                                  : current.accountIds.filter(item => item !== normalizedId);
                                const domainSet = new Set(current.domains);
                                if (value) {
                                  accountDomains.forEach(domain => domainSet.add(domain));
                                } else {
                                  accountDomains.forEach(domain => domainSet.delete(domain));
                                }
                                return {
                                  ...current,
                                  accountIds: nextAccountIds,
                                  domains: Array.from(domainSet).sort((a, b) =>
                                    a.localeCompare(b)
                                  ),
                                };
                              });
                            }}
                          />
                          <ChevronDown
                            className={cx(
                              'h-3.5 w-3.5 text-kumo-subtle transition',
                              checked ? 'rotate-0' : '-rotate-90'
                            )}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-xs font-medium text-kumo-strong">
                              {account.name}
                            </div>
                            <div className="mt-0.5 truncate text-[11px] text-kumo-subtle">
                              默认 @{account.defaultDomain || '-'}
                              {accountDomains.length > 0
                                ? `，共 ${accountDomains.length} 个域名`
                                : ''}
                            </div>
                          </div>
                          <span className="shrink-0 text-[11px] text-kumo-subtle">
                            {checked
                              ? `已选 ${selectedCount}/${accountDomains.length || 0}`
                              : '未启用'}
                          </span>
                        </label>

                        {checked ? (
                          <div className="mt-2 border-t border-kumo-line/60 pt-2">
                            {accountDomains.length === 0 ? (
                              <div className="text-[11px] text-kumo-subtle">
                                当前租户未读取到可选域名，请先校验租户连接。
                              </div>
                            ) : (
                              <div className="grid gap-1">
                                {accountDomains.map(domain => {
                                  const domainChecked = publicPageForm.domains.includes(domain);
                                  return (
                                    <label
                                      key={domain}
                                      className="flex min-w-0 items-center gap-2 rounded px-2 py-1 hover:bg-kumo-recessed/20"
                                    >
                                      <Checkbox
                                        checked={domainChecked}
                                        onCheckedChange={value => {
                                          setPublicPageForm(current => ({
                                            ...current,
                                            domains: value
                                              ? Array.from(
                                                  new Set([...current.domains, domain])
                                                ).sort((a, b) => a.localeCompare(b))
                                              : current.domains.filter(item => item !== domain),
                                          }));
                                        }}
                                      />
                                      <span className="min-w-0 flex-1 truncate text-xs text-kumo-strong">
                                        @{domain}
                                      </span>
                                    </label>
                                  );
                                })}
                              </div>
                            )}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              </div>
              <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                <Checkbox
                  checked={publicPageForm.enabled}
                  onCheckedChange={checked =>
                    setPublicPageForm(current => ({ ...current, enabled: !!checked }))
                  }
                />
                启用公开页
              </label>
              <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                <Checkbox
                  checked={publicPageForm.forceChangePasswordNextSignIn}
                  onCheckedChange={checked =>
                    setPublicPageForm(current => ({
                      ...current,
                      forceChangePasswordNextSignIn: !!checked,
                    }))
                  }
                />
                首次登录强制修改密码
              </label>
            </div>

            <div className="space-y-2 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="text-sm font-medium text-kumo-strong">许可证模板</div>
                <div className="text-xs text-kumo-subtle">
                  已选 {publicPageForm.skuIds.length} 项
                </div>
              </div>
              <div className="text-xs text-kumo-subtle">
                新注册账号会分配下方许可证。
              </div>
              <div className="rounded-lg border border-kumo-line/70 bg-kumo-base/60 px-3 py-2 text-xs text-kumo-subtle">
                保存后请到“邀请码”页面单独生成注册链接，每次最多生成 5 个一次性邀请码。
              </div>
              {publicPageForm.accountIds.length === 0 ? (
                <div className="text-xs text-kumo-subtle">
                  先勾选一个租户，再读取该租户的许可证模板。
                </div>
              ) : skus.length === 0 ? (
                <div className="text-xs text-kumo-subtle">暂无可选订阅</div>
              ) : (
                <div className="max-h-80 overflow-auto pr-1 scrollbar-thin">
                  <div className="grid gap-1">
                    {skus.map(sku => {
                      const normalizedId = String(sku.skuId);
                      const checked = publicPageForm.skuIds.includes(normalizedId);
                      return (
                        <label
                          key={sku.skuId}
                          className="flex min-w-0 items-center gap-2 rounded border border-transparent px-2 py-1.5 hover:border-kumo-line hover:bg-kumo-base/60"
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={value => {
                              setPublicPageForm(current => ({
                                ...current,
                                skuIds: value
                                  ? current.skuIds.includes(normalizedId)
                                    ? current.skuIds
                                    : [...current.skuIds, normalizedId]
                                  : current.skuIds.filter(item => item !== normalizedId),
                              }));
                            }}
                          />
                          <span className="min-w-0 flex-1 truncate text-xs text-kumo-strong">
                            {getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                          </span>
                        </label>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={submitPublicPage} loading={submittingPublicPage}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
