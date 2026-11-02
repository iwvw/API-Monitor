import React from 'react';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Check, Globe } from '../../components/Icons.jsx';
import { FieldRow, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { TIMEZONE_OPTIONS } from './constants.js';
import { formatFileSize } from './utils.js';

export function GeneralPanel({ backendOnline, currentOrigin, databaseSizeBytes, logFileInfo, logSettings, patchSettings, settings }) {
  return (
        <div className="grid min-h-0 items-start gap-4 cq-md:h-full cq-md:overflow-auto cq-xl:grid-cols-[minmax(16rem,1fr)_minmax(0,3fr)]">
          <SectionCard
            title="运行状态"
            icon={<Check className="h-4 w-4 text-brand" />}
            className="min-h-0 self-start"
            bodyPadding="none"
          >
            <FieldRow title="运行状态">
              <span className={`font-mono text-sm font-semibold ${backendOnline ? 'text-kumo-success' : 'text-kumo-danger'}`}>{backendOnline ? '正常' : '离线'}</span>
            </FieldRow>
            <FieldRow title="公网入口" >
              <span className="truncate font-mono text-sm font-medium text-kumo-strong">{settings.publicApiUrl || currentOrigin}</span>
            </FieldRow>
            <FieldRow title="数据库大小">
              <span className="font-mono text-sm font-medium text-kumo-strong">{formatFileSize(databaseSizeBytes)}</span>
            </FieldRow>
            <FieldRow title="日志文件">
              <span className="font-mono text-sm font-medium text-kumo-strong">上限 {logFileInfo?.sizeFormatted || `${logSettings.logFileSizeMB || 10} MB`}</span>
            </FieldRow>
          </SectionCard>

          <SectionCard
            title="部署访问地址"
            icon={<Globe className="h-4 w-4 text-brand" />}
            className="min-h-0 self-start"
            bodyPadding="none"
          >
            <FieldRow title="公网 API 地址" description="公网可访问时填写，留空用当前来源。">
              <Input size="sm"
                value={settings.publicApiUrl}
                onChange={(e) => patchSettings({ publicApiUrl: e.target.value })}
                placeholder="https://monitor.example.com"
                aria-label="公网 API 地址"
              />
            </FieldRow>
            <FieldRow title="系统时区" description="本地化时间；跟随服务器用默认时区。">
              <Select alignItemWithTrigger
                size="sm"
                value={settings.timezone}
                onValueChange={(value) => patchSettings({ timezone: value })}
                items={TIMEZONE_OPTIONS}
              />
            </FieldRow>
          </SectionCard>
        </div>
  );
}
