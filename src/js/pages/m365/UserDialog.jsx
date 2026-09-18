import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Meter } from '@cloudflare/kumo';
import { DEFAULT_NEW_USER_PASSWORD } from './constants.js';
import { RefreshCw } from '../../components/Icons.jsx';
import { formatBytes, getOneDriveUsagePercent, getOneDriveUsageTone, getSkuDisplayLabel } from './utils.js';

export default function UserDialog({
  open,
  onOpenChange,
  editingUser,
  loadingUserDialog,
  userForm,
  setUserForm,
  userEmailDomainItems,
  skus,
  userDialogSkuIds,
  setUserDialogSkuIds,
  submitUser,
  submittingUser,
  assigningLicense,
  userDriveQuota,
  userDriveQuotaLoading,
  userDriveQuotaError,
  reloadUserDriveQuota,
}) {
  const driveUsagePct = getOneDriveUsagePercent(userDriveQuota);
  const driveTone = getOneDriveUsageTone(driveUsagePct);
  const driveProgressTone =
    driveTone === 'danger'
      ? '!bg-kumo-danger'
      : driveTone === 'warning'
        ? '!bg-kumo-warning'
        : '!bg-brand';
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="lg">
        <LayerDialog.Title>{editingUser ? '编辑用户' : '新增用户'}</LayerDialog.Title>
        <LayerDialog.Description>
          {editingUser
            ? '更新用户资料、许可证与 OneDrive 容量。'
            : '填写账号信息并按需分配订阅许可证。'}
        </LayerDialog.Description>
        <LayerDialog.Body>
        <div className="@container space-y-4">
          {loadingUserDialog ? (
            <div className="space-y-3">
              <SkeletonLine className="h-10 w-full" />
              <SkeletonLine className="h-10 w-full" />
              <SkeletonLine className="h-10 w-full" />
              <SkeletonLine className="h-40 w-full" />
            </div>
          ) : (
            <div className="grid gap-4 cq-lg:grid-cols-[minmax(0,1fr)_minmax(20rem,24rem)]">
              <div className="grid gap-3">
                <Input
                  size="sm"
                  aria-label="显示名称"
                  value={userForm.displayName}
                  onChange={event =>
                    setUserForm(current => ({ ...current, displayName: event.target.value }))
                  }
                  placeholder="显示名称"
                />
                <div className="grid grid-cols-[minmax(0,1fr)_12rem] gap-2">
                  <Input
                    size="sm"
                    aria-label={editingUser ? '登录账号前缀' : '邮箱前缀'}
                    value={userForm.mailNickname}
                    onChange={event =>
                      setUserForm(current => ({ ...current, mailNickname: event.target.value }))
                    }
                    placeholder={editingUser ? '登录账号前缀' : '邮箱前缀'}
                  />
                  <Select alignItemWithTrigger
                    aria-label="邮箱后缀"
                    size="sm"
                    value={userForm.emailDomain}
                    onValueChange={value =>
                      setUserForm(current => ({ ...current, emailDomain: value }))
                    }
                    items={userEmailDomainItems}
                  />
                </div>
                <div className="text-xs text-kumo-subtle">
                  {!userForm.mailNickname.trim()
                    ? '显示名称留空时会自动使用邮箱前缀。'
                    : `登录账号预览：${userForm.mailNickname}${userForm.emailDomain ? `@${userForm.emailDomain}` : ''}`}
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    size="sm"
                    aria-label={editingUser ? '重置密码' : '初始密码'}
                    value={userForm.password}
                    onChange={event =>
                      setUserForm(current => ({ ...current, password: event.target.value }))
                    }
                    placeholder={editingUser ? '留空则不修改密码' : '初始密码'}
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    icon={<RefreshCw className="h-3.5 w-3.5" />}
                    onClick={() =>
                      setUserForm(current => ({
                        ...current,
                        password: DEFAULT_NEW_USER_PASSWORD,
                      }))
                    }
                  >
                    预设
                  </Button>
                </div>
                {!editingUser ? (
                  <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                    <Checkbox
                      checked={userForm.forceChangePasswordNextSignIn}
                      onCheckedChange={checked =>
                        setUserForm(current => ({
                          ...current,
                          forceChangePasswordNextSignIn: !!checked,
                        }))
                      }
                    />
                    下次登录时强制修改密码
                  </label>
                ) : null}
                <label className="flex items-center gap-2 text-xs text-kumo-subtle">
                  <Checkbox
                    checked={userForm.accountEnabled}
                    onCheckedChange={checked =>
                      setUserForm(current => ({ ...current, accountEnabled: !!checked }))
                    }
                  />
                  启用账号
                </label>
                {!editingUser ? (
                  <>
                    <Input
                      size="sm"
                      aria-label="部门"
                      value={userForm.department}
                      onChange={event =>
                        setUserForm(current => ({ ...current, department: event.target.value }))
                      }
                      placeholder="部门"
                    />
                    <Input
                      size="sm"
                      aria-label="职位"
                      value={userForm.jobTitle}
                      onChange={event =>
                        setUserForm(current => ({ ...current, jobTitle: event.target.value }))
                      }
                      placeholder="职位"
                    />
                    <Input
                      size="sm"
                      aria-label="办公地点"
                      value={userForm.officeLocation}
                      onChange={event =>
                        setUserForm(current => ({
                          ...current,
                          officeLocation: event.target.value,
                        }))
                      }
                      placeholder="办公地点"
                    />
                    <Input
                      size="sm"
                      aria-label="使用地区"
                      value={userForm.usageLocation}
                      onChange={event =>
                        setUserForm(current => ({
                          ...current,
                          usageLocation: event.target.value,
                        }))
                      }
                      placeholder="CN / US / HK"
                    />
                  </>
                ) : null}
              </div>

              <div className="space-y-2 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium text-kumo-strong">许可证</div>
                  <div className="text-xs text-kumo-subtle">
                    已选 {userDialogSkuIds.length} 项
                  </div>
                </div>
                <div className="text-xs text-kumo-subtle">
                  {editingUser
                    ? '可直接勾选，保存时会一并更新许可证。'
                    : '新增用户后会自动分配已勾选许可证。'}
                </div>
                {skus.length === 0 ? (
                  <div className="text-xs text-kumo-subtle">
                    暂无可选订阅
                  </div>
                ) : (
                  <div className="max-h-80 overflow-auto pr-1 scrollbar-thin">
                    <div className="grid gap-1">
                      {skus.map(sku => {
                        const normalizedId = String(sku.skuId);
                        const checked = userDialogSkuIds.includes(normalizedId);
                        return (
                          <label
                            key={sku.skuId}
                            className="flex min-w-0 items-center gap-2 rounded border border-transparent px-2 py-1.5 hover:border-kumo-line hover:bg-kumo-base/60"
                          >
                            <Checkbox
                              checked={checked}
                              onCheckedChange={value => {
                                setUserDialogSkuIds(current =>
                                  value
                                    ? current.includes(normalizedId)
                                      ? current
                                      : [...current, normalizedId]
                                    : current.filter(item => item !== normalizedId)
                                );
                              }}
                              aria-label={`选择 ${getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}`}
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

              {editingUser ? (
                <div className="space-y-2 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-sm font-medium text-kumo-strong">OneDrive 容量</div>
                    <div className="flex items-center gap-2">
                      {userDriveQuota?.quotaState ? (
                        <span className="text-xs text-kumo-subtle">
                          {userDriveQuota.quotaState}
                        </span>
                      ) : null}
                      {reloadUserDriveQuota ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label="刷新 OneDrive 容量"
                          icon={<RefreshCw className="h-3.5 w-3.5" />}
                          onClick={reloadUserDriveQuota}
                        />
                      ) : null}
                    </div>
                  </div>
                  {userDriveQuotaLoading ? (
                    <div className="text-xs text-kumo-subtle">加载中…</div>
                  ) : userDriveQuotaError ? (
                    <div className="text-xs text-kumo-danger">{userDriveQuotaError}</div>
                  ) : userDriveQuota?.provisioned === false ? (
                    <div className="text-xs text-kumo-subtle">
                      {userDriveQuota.message || '该用户尚未开通 OneDrive'}
                    </div>
                  ) : userDriveQuota ? (
                    <>
                      <div className="flex items-baseline gap-2 text-xs">
                        <span className="font-semibold text-kumo-strong">
                          {formatBytes(userDriveQuota.usedBytes)}
                        </span>
                        <span className="text-kumo-subtle">
                          / {formatBytes(userDriveQuota.totalBytes)}
                        </span>
                        <span className="ml-auto text-kumo-subtle">
                          {driveUsagePct.toFixed(1)}%
                        </span>
                      </div>
                      <Meter
                        label=""
                        value={Math.min(100, driveUsagePct)}
                        max={100}
                        showValue={false}
                        trackClassName="!h-1.5 bg-kumo-recessed/80"
                        indicatorClassName={driveProgressTone}
                      />
                      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] text-kumo-subtle">
                        <span>
                          剩余 {formatBytes(userDriveQuota.remainingBytes)}
                        </span>
                        <span>
                          回收站 {formatBytes(userDriveQuota.deletedBytes)}
                        </span>
                        {userDriveQuota.overQuota ? (
                          <span className="text-kumo-danger">已超额</span>
                        ) : null}
                      </div>
                    </>
                  ) : (
                    <div className="text-xs text-kumo-subtle">暂无 OneDrive 数据</div>
                  )}
                </div>
              ) : null}
            </div>
          )}
        </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            onClick={submitUser}
            loading={submittingUser || assigningLicense}
            disabled={loadingUserDialog}
          >
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
