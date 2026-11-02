import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { applyCustomCss, FONT_OPTIONS, FONT_SIZE_OPTIONS } from '../../store.js';
import { FieldRow, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Sun, Terminal } from '../../components/Icons.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { THEME_OPTIONS } from './constants.js';

export function AppearancePanel({
  handleDashboardFooterRecordNumberChange,
  handleDashboardFooterVisibleChange,
  handleThemeModeChange,
  handleUIFontChange,
  handleUIFontSizeChange,
  handleVibrationEnabledChange,
  patchSettings,
  settings,
  themeMode,
  uiFontSize,
}) {
  return (
        <div className="grid min-h-0 items-start gap-3 overflow-auto cq-xl:grid-cols-[minmax(20rem,0.82fr)_minmax(0,1.18fr)]">
          <SectionCard
            title="界面外观"
            icon={<Sun className="h-4 w-4 text-brand" />}
            bodyPadding="none"
          >
            <FieldRow title="主题模式">
              <Select alignItemWithTrigger size="sm" value={themeMode} onValueChange={handleThemeModeChange} items={THEME_OPTIONS} />
            </FieldRow>
            <FieldRow title="界面字体">
              <Select alignItemWithTrigger size="sm" value={settings.uiFont || 'default'} onValueChange={handleUIFontChange} items={FONT_OPTIONS} />
            </FieldRow>
            <FieldRow title="字号与布局">
              <Select alignItemWithTrigger size="sm" value={uiFontSize} onValueChange={handleUIFontSizeChange} items={FONT_SIZE_OPTIONS} />
            </FieldRow>
            <FieldRow title="显示首页页脚">
              <Switch aria-label="显示首页页脚" checked={settings.dashboardFooterVisible} onCheckedChange={handleDashboardFooterVisibleChange} />
            </FieldRow>
            <FieldRow title="备案号">
              <Input
                size="sm"
                aria-label="首页页脚备案号"
                value={settings.dashboardFooterRecordNumber}
                onChange={handleDashboardFooterRecordNumberChange}
                placeholder="例如：京ICP备12345678号"
                className="w-full min-w-52"
              />
            </FieldRow>
            <FieldRow title="触感反馈">
              <Switch checked={settings.vibrationEnabled} onCheckedChange={handleVibrationEnabledChange} />
            </FieldRow>
          </SectionCard>

          <SectionCard
            title="自定义 CSS"
            icon={<Terminal className="h-4 w-4 text-brand" />}
            actions={
                <>
                  <Button size="sm" onClick={() => applyCustomCss(settings.customCss)}>预览</Button>
                  <Button size="sm" variant="secondary-destructive" onClick={() => {
                    patchSettings({ customCss: '' });
                    applyCustomCss('');
                  }}>清空</Button>
                </>
            }
            bodyPadding="none"
          >
            <CodeEditor
              variant="embedded"
              label="CSS"
              language="css"
              value={settings.customCss}
              onChange={(customCss) => patchSettings({ customCss })}
              placeholder="/* 在此输入自定义 CSS */"
              minHeight="18rem"
              showHeader={false}
              showLanguage={false}
            />
          </SectionCard>
        </div>
  );
}
