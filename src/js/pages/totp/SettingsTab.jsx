import React from 'react';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Toolbar } from '@cloudflare/kumo';
import { dialog } from '../../modules/dialog.js';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Bot, Download, RotateCw, Shield, Upload } from '../../components/Icons.jsx';

const SettingsTab = ({
  totpSettings,
  updateSetting,
  importUrisDirectly,
  handleExportAccounts,
  refreshCodes,
  showExtensionGuide,
  setShowExtensionGuide,
  syncConfigToExtension,
}) => {
  return (
    <div className="grid grid-cols-1 items-start gap-4 cq-lg:grid-cols-3">
      {/* Settings Options (Span 2) */}
      <SectionCard
        title="安全与显示配置"
        icon={<Shield className="h-4 w-4 text-brand" />}
        className="cq-lg:col-span-2"
        bodyPadding="md"
        bodyClassName="divide-y divide-kumo-line/80"
      >
        {/* Toggle 1: maskAccount */}
        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">账号名称打码</h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              对邮箱或长账号名称脱敏隐藏，避免屏幕泄露。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.maskAccount}
            onCheckedChange={checked => updateSetting('maskAccount', checked)}
            size="sm"
          />
        </div>

        {/* Toggle 2: hideCode */}
        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">遮挡实时验证码</h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              隐藏验证码数值，仅在悬浮或点击复制时显示，防止窥屏。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.hideCode}
            onCheckedChange={checked => updateSetting('hideCode', checked)}
            size="sm"
          />
        </div>

        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">
              允许悬浮显示验证码
            </h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              开启后鼠标悬浮验证码卡片时临时显示被遮挡的验证码。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.allowRevealCode}
            onCheckedChange={checked => updateSetting('allowRevealCode', checked)}
            size="sm"
          />
        </div>

        {/* Toggle 3: groupByPlatform */}
        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">按站点分组</h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              将相同站点或服务（如 Google、GitHub）的账号汇聚分组显示。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.groupByPlatform}
            onCheckedChange={checked => updateSetting('groupByPlatform', checked)}
            size="sm"
          />
        </div>

        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">显示站点标题</h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              按站点分组时，在每组账号前显示站点名称和账号数量。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.showPlatformHeaders}
            onCheckedChange={checked => updateSetting('showPlatformHeaders', checked)}
            disabled={!totpSettings.groupByPlatform}
            size="sm"
          />
        </div>

        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">隐藏站点文字</h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              只保留颜色标识和账号数量，减少站点名称在共享屏幕中暴露。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.hidePlatformText}
            onCheckedChange={checked => updateSetting('hidePlatformText', checked)}
            disabled={!totpSettings.groupByPlatform || !totpSettings.showPlatformHeaders}
            size="sm"
          />
        </div>

        {/* Toggle 4: autoSave */}
        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">
              解析二维码后自动导入
            </h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              扫码或选取二维码图片后自动读取数据入库，无需手动核对表单保存。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.autoSave}
            onCheckedChange={checked => updateSetting('autoSave', checked)}
            size="sm"
          />
        </div>

        {/* Toggle 5: lockInputMode */}
        <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 pr-4">
            <h4 className="text-xs font-semibold leading-5 text-kumo-strong">
              锁定默认录入类型
            </h4>
            <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
              开启后添加账号弹窗默认直接使用锁定的选项，无需每次手动选择。
            </p>
          </div>
          <Switch
            checked={!!totpSettings.lockInputMode}
            onCheckedChange={checked => updateSetting('lockInputMode', checked)}
            size="sm"
          />
        </div>

        {totpSettings.lockInputMode && (
          <div className="flex flex-wrap items-center justify-between gap-3 py-3 pl-3 first:pt-0 last:pb-0">
            <label className="text-xs font-medium text-kumo-subtle">默认录入模式</label>
            <Select alignItemWithTrigger
              aria-label="默认录入模式"
              size="sm"
              value={totpSettings.defaultInputMode}
              onValueChange={value => updateSetting('defaultInputMode', String(value))}
              items={[
                { value: 'scan', label: '扫描二维码' },
                { value: 'upload', label: '上传二维码' },
                { value: 'manual', label: '手动录入表单' },
              ]}
            />
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2 pt-3 first:pt-0 last:pb-0">
          <Toolbar size="sm" aria-label="批量导入导出" className="shrink-0">
            <Toolbar.Button
              onClick={async () => {
                const uris = await dialog.prompt({
                  message: '请输入批量导入的 otpauth:// 链接列表 (每行一条)',
                });
                importUrisDirectly(uris || '');
              }}
              aria-label="批量导入 URI"
              title="批量导入 URI"
              icon={<Download className="h-3.5 w-3.5" />}
            >
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
            <Toolbar.Button
              onClick={handleExportAccounts}
              aria-label="批量导出备份"
              title="批量导出备份"
              icon={<Upload className="h-3.5 w-3.5" />}
            >
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
          </Toolbar>
          <Button size="sm" onClick={refreshCodes} icon={<RotateCw className="w-3.5 h-3.5" />}>
            手动刷新验证码
          </Button>
        </div>
      </SectionCard>

      {/* Right Column: Browser Extension Helper Card */}
      <SectionCard
        title="浏览器插件助手"
        icon={<Bot className="h-4 w-4 text-brand" />}
        className="cq-lg:self-start"
        bodyPadding="md"
        bodyClassName="flex flex-col gap-3"
      >
        <div className="space-y-3.5">
          <p className="text-xs text-kumo-subtle leading-relaxed">
            下载安装 2FA 浏览器插件，PC 端登录账号需要验证码时可自动检索并快捷填充。
          </p>

          <div className="p-3 bg-kumo-recessed/60 border border-kumo-line rounded-lg flex items-start gap-3 mt-3">
            <div className="w-9 h-9 rounded-md bg-kumo-base flex items-center justify-center flex-shrink-0">
              <img
                src="https://cdn.simpleicons.org/blueprint"
                className="w-6 h-6"
                alt="Extension"
              />
            </div>
            <div className="min-w-0">
              <h4 className="text-xs font-semibold text-kumo-strong">API Monitor 2FA 助手</h4>
              <p className="text-[10px] text-kumo-subtle mt-0.5">
                一次性配对，使用受限、可撤销的 API Key
              </p>
            </div>
          </div>
        </div>

        <div className="space-y-2">
          <LinkButton
            size="sm"
            variant="secondary"
            href="/api/totp/extension/download"
            className="w-full justify-center"
            icon={<Download className="w-3.5 h-3.5" />}
          >
            下载插件 ZIP 包
          </LinkButton>

          <Button
            size="sm"
            variant="primary"
            className="w-full"
            onClick={syncConfigToExtension}
          >
            生成安全配对码
          </Button>

          <Button
            size="sm"
            variant="secondary"
            className="w-full text-xs"
            onClick={() => setShowExtensionGuide(!showExtensionGuide)}
          >
            {showExtensionGuide ? '关闭教程' : '查看安装教程'}
          </Button>

          <AnimatedCollapse open={showExtensionGuide}>
            <div className="text-[11px] text-kumo-subtle space-y-2 mt-4 p-3 bg-kumo-recessed/50 rounded-lg border border-kumo-line">
              <p className="font-semibold text-kumo-strong">三步完成安装：</p>
              <ol className="list-decimal pl-4 space-y-1">
                <li>解压下载的 ZIP 压缩包至本地固定目录；</li>
                <li>
                  打开 Chrome，访问 <code>chrome://extensions</code> 并开启右上角的
                  <strong>开发者模式</strong>；
                </li>
                <li>
                  点击<strong>加载已解压的扩展程序</strong>，选择刚才解压的目录文件夹。
                </li>
              </ol>
              <div className="bg-brand/10 text-brand p-2 rounded border border-brand/20 mt-1 font-medium select-all">
                配置插件地址: {window.location.origin}
              </div>
              <p>
                在插件设置页填写上方地址，并粘贴刚生成的一次性配对码；配对码 10
                分钟后失效且只能使用一次。
              </p>
            </div>
          </AnimatedCollapse>
        </div>
      </SectionCard>
    </div>
  );
};

export default SettingsTab;
