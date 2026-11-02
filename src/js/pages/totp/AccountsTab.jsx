import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { LayerCard, Meter } from '@cloudflare/kumo';
import { Edit, RefreshCw, Shield, Trash } from '../../components/Icons.jsx';
import { getIssuerColor } from '../../components/ui/BrandIcon.jsx';
import { maskEmail } from './utils.js';
import TotpBrandMark from './TotpBrandMark.jsx';

const AccountsTab = ({
  totpLoading,
  filteredAccounts,
  totpSearchQuery,
  handleOpenAddAccount,
  totpSettings,
  totpCodes,
  platformCounts,
  isArmed,
  handleCardMouseEnter,
  handleCardMouseLeave,
  copyCodeToClipboard,
  handleOpenEditAccount,
  handleDeleteAccount,
  incrementHotp,
  getTotpCodeParts,
}) => {
  if (totpLoading) {
    return (
      <div>
        <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-sm:gap-2.5 cq-md:grid-cols-4 cq-lg:grid-cols-5 cq-xl:grid-cols-6">
          {[...Array(6)].map((_, i) => (
            <LayerCard key={i} className="space-y-2 p-2 cq-sm:space-y-3 cq-sm:p-3">
              <div className="flex items-center gap-2">
                <SkeletonLine className="h-6 w-6 rounded-md" />
                <div className="flex-1 space-y-1.5">
                  <SkeletonLine className="w-1/2 h-3" />
                  <SkeletonLine className="w-3/4 h-2" />
                </div>
              </div>
              <div className="space-y-1.5">
                <SkeletonLine className="h-5 w-2/3" />
                <SkeletonLine className="w-1/3 h-2" />
              </div>
            </LayerCard>
          ))}
        </div>
      </div>
    );
  }

  if (filteredAccounts.length === 0) {
    return (
      <div>
        <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
          <Shield className="w-12 h-12 opacity-30 mb-4" />
          <div className="text-sm">
            {totpSearchQuery ? '没有找到匹配的账号' : '暂无 2FA 账号'}
          </div>
          {!totpSearchQuery && (
            <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddAccount}>
              添加第一个账号
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div>
      <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-sm:gap-2.5 cq-md:grid-cols-4 cq-lg:grid-cols-5 cq-xl:grid-cols-6">
        {filteredAccounts.map((account, index) => {
          const isFirstOfPlatform =
            index === 0 ||
            (account.issuer || '').toLowerCase() !==
              (filteredAccounts[index - 1].issuer || '').toLowerCase();

          const issuerColor = account.color || getIssuerColor(account.issuer);
          const codeDetail = totpCodes[account.id] || {};
          const remaining = codeDetail.remaining ?? 30;
          const period = account.period || 30;
          const ratio = Math.max(0, Math.min(100, (remaining / period) * 100));
          const codeParts = getTotpCodeParts(account, codeDetail.code);

          const showHeader =
            totpSettings.groupByPlatform &&
            totpSettings.showPlatformHeaders &&
            isFirstOfPlatform;

          return (
            <React.Fragment key={account.id}>
              {showHeader && (
                <div className="col-span-full mt-2 flex items-center justify-between border-b border-kumo-line pb-1.5">
                  {!totpSettings.hidePlatformText ? (
                    <div className="flex items-center gap-2">
                      <TotpBrandMark
                        issuer={account.issuer}
                        icon={account.icon}
                        size="header"
                      />
                      <span className="text-xs font-semibold text-kumo-strong">
                        {account.issuer || '未知平台'}
                      </span>
                      <span className="ml-1 rounded border border-kumo-line bg-kumo-recessed px-1 py-0.5 text-[9px] font-medium leading-none text-kumo-subtle">
                        {platformCounts[(account.issuer || '').toLowerCase()]} 个账号
                      </span>
                    </div>
                  ) : (
                    <div className="flex items-center gap-1.5">
                      <span
                        className="size-2.5 rounded-full"
                        style={{ background: getIssuerColor(account.issuer) }}
                      />
                      <span className="text-[10px] text-kumo-subtle font-medium">
                        {platformCounts[(account.issuer || '').toLowerCase()]} 个账号
                      </span>
                    </div>
                  )}
                </div>
              )}

              <LayerCard
                onMouseEnter={() => handleCardMouseEnter(account.id)}
                onMouseLeave={() => handleCardMouseLeave(account.id)}
                onClick={() => copyCodeToClipboard(account)}
                className="group/card relative grid min-h-[96px] min-w-0 cursor-pointer grid-rows-[auto_1fr_auto] overflow-hidden p-0 hover:border-brand cq-sm:min-h-[112px]"
              >
                <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-1.5 border-b border-kumo-line bg-kumo-recessed/35 px-2 py-1.5 cq-sm:gap-2 cq-sm:px-3 cq-sm:py-2">
                  <TotpBrandMark
                    issuer={account.issuer}
                    icon={account.icon}
                    color={issuerColor}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[10px] font-semibold leading-tight text-kumo-strong cq-sm:text-[11px]">
                      {account.issuer || '未知平台'}
                    </div>
                    <div className="mt-0.5 truncate pb-px text-[9px] leading-tight text-kumo-subtle cq-sm:mt-0.5 cq-sm:text-[10px]">
                      {totpSettings.maskAccount
                        ? maskEmail(account.account)
                        : account.account}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1 opacity-65 transition-opacity group-hover/card:opacity-100">
                    <Button
                      shape="square"
                      size="sm"
                      variant="secondary"
                      aria-label="编辑账号"
                      onClick={e => {
                        e.stopPropagation();
                        handleOpenEditAccount(account);
                      }}
                      className="!size-5 !p-0 cq-sm:!size-6"
                      title="编辑"
                    >
                      <Edit className="h-3 w-3" />
                    </Button>
                    <Button
                      shape="square"
                      size="sm"
                      variant={
                        isArmed(`totp-account-${account.id}`)
                          ? 'destructive'
                          : 'secondary-destructive'
                      }
                      aria-label="删除账号"
                      onClick={e => {
                        e.stopPropagation();
                        handleDeleteAccount(account);
                      }}
                      className="!size-5 !p-0 cq-sm:!size-6"
                      title={
                        isArmed(`totp-account-${account.id}`)
                          ? '再次点击确认删除'
                          : '删除'
                      }
                    >
                      <Trash className="h-3 w-3" />
                    </Button>
                  </div>
                </div>

                <div
                  className={`flex items-center justify-center gap-1 px-2 py-2 font-mono tabular-nums cq-sm:gap-2 cq-sm:px-3 cq-sm:py-2.5 ${
                    remaining <= 5 ? 'text-kumo-danger' : 'text-kumo-strong'
                  }`}
                >
                  {codeParts.map((part, partIndex) => (
                    <span
                      key={`${account.id}-${partIndex}`}
                      className="min-w-0 flex-1 rounded-md bg-kumo-recessed px-1.5 py-1 text-center text-sm font-semibold leading-none tracking-normal cq-sm:min-w-[4.25rem] cq-sm:flex-none cq-sm:px-2"
                    >
                      {part}
                    </span>
                  ))}
                </div>

                <div className="border-t border-kumo-line px-2 py-1.5 font-mono text-[10px] text-kumo-subtle cq-sm:px-3 cq-sm:py-2">
                  {account.otp_type === 'hotp' ? (
                    <div className="flex items-center justify-between gap-2">
                      <span>counter #{codeDetail.counter || 0}</span>
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={e => {
                          e.stopPropagation();
                          incrementHotp(account);
                        }}
                        className="flex h-6 items-center gap-1 px-2 text-[10px] text-kumo-strong"
                      >
                        <RefreshCw className="h-3 w-3" />
                        <span>递增</span>
                      </Button>
                    </div>
                  ) : (
                    <div
                      className="grid grid-cols-[minmax(0,1fr)_1.75rem] items-center gap-1.5 cq-sm:grid-cols-[minmax(0,1fr)_2rem] cq-sm:gap-2"
                      style={{ '--issuer-color': issuerColor }}
                    >
                      <Meter
                        label=""
                        value={ratio}
                        max={100}
                        showValue={false}
                        className="min-w-0"
                        trackClassName="!h-1.5 bg-kumo-recessed"
                        indicatorClassName="[background:var(--issuer-color)]"
                      />
                      <span className="select-none text-right text-[10px]">{remaining}s</span>
                    </div>
                  )}
                </div>
              </LayerCard>
            </React.Fragment>
          );
        })}
      </div>
    </div>
  );
};

export default AccountsTab;
