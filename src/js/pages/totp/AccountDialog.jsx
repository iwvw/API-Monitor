import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Banner } from '@cloudflare/kumo/components/banner';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { LayerCard, Loader, SensitiveInput, Tabs } from '@cloudflare/kumo';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import { sectionCardHeaderClass, cx } from '../../components/ui/AppPrimitives.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import {
  ChevronRight,
  Upload,
} from '../../components/Icons.jsx';
import TotpBrandMark from './TotpBrandMark.jsx';
import { normalizeHexColor, normalizeSVGRepoIconRef, resolveFormColor } from './utils.js';

const AccountDialog = ({
  showAccountModal,
  setShowAccountModal,
  stopQrScan,
  accountModalMode,
  accountAddTab,
  setAccountAddTab,
  accountForm,
  setAccountForm,
  isScanning,
  startQrScan,
  fileInputRef,
  handleQrUpload,
  qrParsing,
  qrError,
  handleQrPaste,
  importUris,
  setImportUris,
  totpGroups,
  brandDetecting,
  detectAccountBrandIcon,
  openBrandStylePicker,
  showAdvancedAccountSettings,
  setShowAdvancedAccountSettings,
  accountModalError,
  importUrisDirectly,
  handleSaveAccount,
  accountModalSaving,
}) => {
  return (
    <LayerDialog.Root
      open={showAccountModal}
      onOpenChange={open => {
        setShowAccountModal(open);
        if (!open) void stopQrScan();
      }}
    >
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>
          {accountModalMode === 'add' ? '添加或导入 2FA 账号' : '编辑 2FA 账号'}
        </LayerDialog.Title>
        <LayerDialog.Description>
          {accountModalMode === 'add'
            ? '扫码、上传二维码或手动填写动态验证码信息。'
            : '修改令牌标签、品牌标识、分组和验证码参数。'}
        </LayerDialog.Description>
        <LayerDialog.Body>
        <div className="@container">
          {accountModalMode === 'add' && (
            <div className="mb-5">
              <Tabs
                {...TOOL_TABS_PROPS}
                value={accountAddTab}
                onValueChange={value => {
                  stopQrScan();
                  setAccountAddTab(value);
                }}
                tabs={[
                  { value: 'scan', label: '扫码导入' },
                  { value: 'manual', label: '手动录入' },
                ]}
              />
            </div>
          )}

          {/* Form Content */}
          <div className="space-y-4">
            {accountModalMode === 'add' && accountAddTab === 'scan' ? (
              <div className="space-y-4">
                <div className="flex gap-2 items-center">
                  <Button
                    size="sm"
                    onClick={isScanning ? stopQrScan : startQrScan}
                    variant={isScanning ? 'destructive' : 'secondary'}
                  >
                    {isScanning ? '停止摄像头' : '开启摄像头扫码'}
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => fileInputRef.current?.click()}
                    icon={<Upload className="w-3.5 h-3.5" />}
                  >
                    上传二维码图片
                  </Button>
                  <Input
                    size="sm"
                    aria-label="上传二维码图片"
                    type="file"
                    ref={fileInputRef}
                    accept="image/*"
                    onChange={handleQrUpload}
                    className="hidden"
                  />
                </div>

                {isScanning && (
                  <div
                    id="qr-reader"
                    className="app-qr-reader w-full aspect-square max-w-[280px] mx-auto rounded-xl overflow-hidden border border-kumo-line bg-black"
                  />
                )}

                {!isScanning && (
                  <div
                    onPaste={handleQrPaste}
                    tabIndex={0}
                    className="w-full py-10 app-empty-panel rounded-lg flex flex-col items-center justify-center text-kumo-subtle cursor-pointer focus:border-kumo-brand focus:outline-none group"
                  >
                    {qrParsing ? (
                      <span className="flex items-center gap-2">
                        <Loader size={16} />
                        <span>解析中...</span>
                      </span>
                    ) : (
                      <>
                        <Upload className="w-6 h-6 mb-2 opacity-50 group-hover:scale-105 transition-transform" />
                        <span className="text-xs">Ctrl+V 粘贴二维码图片 或 拖拽图片至此</span>
                      </>
                    )}
                  </div>
                )}

                {qrError && (
                  <div className="p-3 bg-kumo-danger/10 border border-kumo-danger/20 text-kumo-danger text-xs rounded-md">
                    {qrError}
                  </div>
                )}

                <div className="space-y-1.5 pt-2">
                  <label className="text-xs font-semibold text-kumo-subtle">
                    批量导入 OTP Auth URI
                  </label>
                  <CodeEditor
                    label="批量 OTP Auth URIs"
                    language="text"
                    placeholder="otpauth://totp/GitHub:user@example.com?secret=..."
                    value={importUris}
                    onChange={setImportUris}
                    minHeight="8rem"
                    showHeader={false}
                  />
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                {/* OTP Type 选择器 */}
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">验证码类型</label>
                  <Tabs
                    {...TOOL_TABS_PROPS}
                    value={accountForm.otp_type}
                    onValueChange={val => setAccountForm(prev => ({ ...prev, otp_type: val }))}
                    tabs={[
                      { value: 'totp', label: 'TOTP (基于时间)' },
                      { value: 'hotp', label: 'HOTP (基于计数)' },
                    ]}
                  />
                </div>

                <div className="grid items-start gap-4 cq-sm:grid-cols-2">
                  <Input
                    size="sm"
                    label="发行商"
                    type="text"
                    placeholder="如：GitHub、Microsoft"
                    value={accountForm.issuer}
                    onChange={e => setAccountForm(prev => ({ ...prev, issuer: e.target.value }))}
                    className="w-full"
                  />
                  <Input
                    size="sm"
                    label="账户名"
                    type="text"
                    placeholder="如：user@example.com"
                    value={accountForm.account}
                    onChange={e => setAccountForm(prev => ({ ...prev, account: e.target.value }))}
                    className="w-full"
                  />
                </div>

                <LayerCard>
                  <LayerCard.Secondary className={sectionCardHeaderClass}>
                    <div className="text-sm font-semibold leading-none text-kumo-strong">
                      品牌标识与主题色
                    </div>
                    <div className="flex items-center gap-2.5">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={detectAccountBrandIcon}
                        loading={brandDetecting}
                      >
                        检测图标
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setAccountForm(prev => ({ ...prev, icon: '', color: '' }))}
                      >
                        重置
                      </Button>
                    </div>
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="grid items-end gap-3 p-3 cq-sm:grid-cols-[auto_minmax(0,1fr)_11rem]">
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      shape="square"
                      onClick={() => openBrandStylePicker()}
                      title="选择或上传品牌图标"
                      aria-label="选择或上传品牌图标"
                      className="mb-px h-12 w-12 p-0"
                    >
                      <TotpBrandMark
                        issuer={accountForm.issuer}
                        icon={accountForm.icon}
                        color={resolveFormColor(accountForm)}
                        size="picker"
                      />
                    </Button>
                    <Input
                      size="sm"
                      label="图标标识"
                      type="text"
                      placeholder="品牌名或 svgrepo:448239-microsoft"
                      value={accountForm.icon}
                      onChange={e =>
                        setAccountForm(prev => ({
                          ...prev,
                          icon: normalizeSVGRepoIconRef(e.target.value),
                        }))
                      }
                      onBlur={e =>
                        setAccountForm(prev => ({
                          ...prev,
                          icon: normalizeSVGRepoIconRef(e.target.value),
                        }))
                      }
                      className="w-full"
                    />
                    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-end gap-2">
                      <span
                        className="mb-px size-8 rounded-md border border-kumo-line"
                        style={{ background: resolveFormColor(accountForm) }}
                        aria-hidden="true"
                      />
                      <Input
                        size="sm"
                        label="品牌色"
                        type="text"
                        inputMode="text"
                        placeholder="#f50049"
                        value={accountForm.color}
                        onChange={e =>
                          setAccountForm(prev => ({ ...prev, color: e.target.value }))
                        }
                        onBlur={e =>
                          setAccountForm(prev => ({
                            ...prev,
                            color: normalizeHexColor(e.target.value),
                          }))
                        }
                        className="w-full font-mono text-xs"
                      />
                    </div>
                  </LayerCard.Primary>
                </LayerCard>

                <div className="grid items-end gap-2">
                  <SensitiveInput
                    size="sm"
                    label="密钥 (Base32)"
                    placeholder="JBSWY3DPEHPK3PXP"
                    readOnly={accountModalMode === 'edit'}
                    value={accountForm.secret}
                    onValueChange={value =>
                      setAccountForm(prev => ({ ...prev, secret: value }))
                    }
                    autoComplete="off"
                    spellCheck={false}
                    className="w-full font-mono"
                    data-1p-ignore
                    data-lpignore="true"
                    data-bwignore="true"
                    data-form-type="other"
                  />
                </div>

                <Select alignItemWithTrigger
                  size="sm"
                  label="关联分组"
                  value={accountForm.group_id}
                  onValueChange={value =>
                    setAccountForm(prev => ({ ...prev, group_id: String(value) }))
                  }
                  placeholder="无分组"
                  className="w-full"
                  items={[
                    { value: '', label: '无分组' },
                    ...totpGroups.map(g => ({ value: String(g.id), label: g.name })),
                  ]}
                />

                {/* 高级参数配置折叠区 */}
                <div className="pt-1">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    icon={
                      <ChevronRight
                        className={cx(
                          'size-3.5 transition-transform duration-200',
                          showAdvancedAccountSettings && 'rotate-90'
                        )}
                      />
                    }
                    onClick={() => setShowAdvancedAccountSettings(prev => !prev)}
                  >
                    高级参数配置
                  </Button>

                  <AnimatedCollapse open={showAdvancedAccountSettings}>
                    <LayerCard className="mt-2 p-3">
                      <div className="grid items-start gap-3 cq-sm:grid-cols-3">
                        <Select alignItemWithTrigger
                          label="加密算法"
                          size="sm"
                          value={accountForm.algorithm}
                          onValueChange={value =>
                            setAccountForm(prev => ({ ...prev, algorithm: String(value) }))
                          }
                          className="w-full"
                          items={[
                            { value: 'SHA1', label: 'SHA1' },
                            { value: 'SHA256', label: 'SHA256' },
                            { value: 'SHA512', label: 'SHA512' },
                          ]}
                        />

                        <Select alignItemWithTrigger
                          label="码位长度"
                          size="sm"
                          value={accountForm.digits}
                          onValueChange={value =>
                            setAccountForm(prev => ({ ...prev, digits: String(value) }))
                          }
                          className="w-full"
                          items={[
                            { value: '6', label: '6 位' },
                            { value: '8', label: '8 位' },
                          ]}
                        />

                        {accountForm.otp_type === 'totp' ? (
                          <Select alignItemWithTrigger
                            label="周期数 (s)"
                            size="sm"
                            value={accountForm.period}
                            onValueChange={value =>
                              setAccountForm(prev => ({ ...prev, period: String(value) }))
                            }
                            className="w-full"
                            items={[
                              { value: '30', label: '30 秒' },
                              { value: '60', label: '60 秒' },
                            ]}
                          />
                        ) : (
                          <Input
                            size="sm"
                            label="计数起始"
                            type="number"
                            value={accountForm.counter}
                            onChange={e =>
                              setAccountForm(prev => ({ ...prev, counter: e.target.value }))
                            }
                            className="w-full font-mono"
                          />
                        )}
                      </div>
                    </LayerCard>
                  </AnimatedCollapse>
                </div>
              </div>
            )}
          </div>

          {accountModalError && (
            <Banner
              variant="error"
              title="无法保存账号"
              description={accountModalError}
              className="mt-4"
            />
          )}
        </div>
        </LayerDialog.Body>

        <LayerDialog.Actions dismissLabel="取消">
          {accountModalMode === 'add' && accountAddTab === 'scan' ? (
            <LayerDialog.Actions.Primary
              type="button"
              onClick={() => importUrisDirectly(importUris)}
              disabled={!importUris.trim()}
            >
              执行导入
            </LayerDialog.Actions.Primary>
          ) : (
            <LayerDialog.Actions.Primary
              type="button"
              onClick={handleSaveAccount}
              loading={accountModalSaving}
            >
              保存账号
            </LayerDialog.Actions.Primary>
          )}
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
};

export default AccountDialog;
