import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Badge, Loader } from '@cloudflare/kumo';
import { ResponsiveSearchInput } from '../../components/ui/AppPrimitives.jsx';
import { Download, X } from '../../components/Icons.jsx';

export default function LogViewerDialog({ logViewerOpen, setLogViewerOpen, logTitle, logSubtitle, logFilterText, setLogFilterText, logLevelFilter, setLogLevelFilter, logWrapText, setLogWrapText, logAutoScroll, setLogAutoScroll, logTailActive, logTailConnected, stopLogTail, setLogs, downloadLogs, logContainerRef, logLoading, filteredLogs }) {
  return (
      <Dialog.Root open={logViewerOpen} onOpenChange={(open) => { setLogViewerOpen(open); if (!open) stopLogTail(); }}>
        <Dialog className="@container flex h-[80vh] !w-[min(56rem,calc(100vw-2rem))] !max-w-[min(56rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          {/* Header */}
          <div className="p-4 border-b border-kumo-line bg-kumo-recessed/40 flex justify-between items-center">
            <div>
              <Dialog.Title className="text-sm font-semibold text-kumo-strong">{logTitle}</Dialog.Title>
              {logSubtitle && <p className="text-[10px] text-kumo-subtle mt-0.5">{logSubtitle}</p>}
            </div>
            <Button
              shape="square" size="sm"
              variant="ghost"
              aria-label="关闭日志查看器"
              onClick={() => setLogViewerOpen(false)}
              className="text-kumo-subtle hover:text-kumo-default"
            >
              <X className="w-4 h-4" />
            </Button>
          </div>

          {/* Log Controls */}
          <div className="p-3 border-b border-kumo-line flex flex-col cq-sm:flex-row justify-between items-start cq-sm:items-center gap-3 text-xs bg-kumo-base">
            <div className="flex items-center gap-2 w-full cq-sm:w-auto">
              <ResponsiveSearchInput
                value={logFilterText}
                onChange={(e) => setLogFilterText(e.target.value)}
                placeholder="搜索日志消息..."
                ariaLabel="搜索日志消息"
                className="cq-sm:w-48"
              />
            </div>
            <div className="flex flex-wrap items-center gap-2 text-[10px]">
              <Select alignItemWithTrigger
                aria-label="日志级别筛选" size="sm"
                value={logLevelFilter}
                onValueChange={(value) => setLogLevelFilter(String(value))}
                className="px-2 py-1 text-kumo-strong font-semibold"
                items={[
                  { value: 'ALL', label: '全部级别' },
                  { value: 'INFO', label: 'INFO' },
                  { value: 'WARN', label: 'WARN' },
                  { value: 'ERROR', label: 'ERROR' },
                  { value: 'DEBUG', label: 'DEBUG' },
                ]}
              />

              <Checkbox
                checked={logWrapText}
                onCheckedChange={(checked) => setLogWrapText(checked)}
                label="自动换行"
              />

              <Checkbox
                checked={logAutoScroll}
                onCheckedChange={(checked) => setLogAutoScroll(checked)}
                label="滚动到底部"
              />

              {logTailActive && (
                <>
                  <Badge variant={logTailConnected ? 'success' : 'warning'} appearance="dot">
                    {logTailConnected ? '已连接' : '已断开'}
                  </Badge>
                  <Button size="sm" variant="secondary-destructive" onClick={stopLogTail} className="text-kumo-danger font-semibold">
                    停止跟随
                  </Button>
                </>
              )}

              <Button size="sm"
                variant="secondary-destructive"
                onClick={() => setLogs([])}
                className="text-kumo-danger font-semibold"
              >
                清空
              </Button>

              <Button size="sm"
                variant="primary"
                onClick={downloadLogs}
                className="text-kumo-inverse font-semibold flex items-center gap-1"
              >
                <Download className="w-3 h-3" />
                <span>下载日志</span>
              </Button>
            </div>
          </div>

          {/* Logs Terminal Area */}
          <div
            ref={logContainerRef}
            id="log-viewer-container"
            className="flex-1 bg-kumo-control text-kumo-strong font-mono text-xs p-4 overflow-y-auto leading-relaxed select-text"
          >
            {logLoading ? (
              <div className="h-full flex items-center justify-center text-kumo-subtle gap-2">
                <Loader size={16} />
                <span>正在获取日志流中...</span>
              </div>
            ) : filteredLogs.length === 0 ? (
              <div className="text-center text-kumo-subtle py-12">暂无匹配日志记录</div>
            ) : (
              <div className="space-y-1">
                {filteredLogs.map((log) => {
                  let levelColor = 'text-kumo-info';
                  if (log.level === 'WARN') levelColor = 'text-kumo-warning';
                  if (log.level === 'ERROR' || log.level === 'FATAL') levelColor = 'text-kumo-danger';
                  if (log.level === 'DEBUG') levelColor = 'text-kumo-success';

                  return (
                    <div
                      key={log.id}
                      className={`${logWrapText ? 'break-all whitespace-pre-wrap' : 'whitespace-nowrap'} hover:bg-kumo-base/40 py-0.5`}
                    >
                      <span className="text-kumo-subtle mr-2">[{new Date(log.timestamp).toLocaleTimeString()}]</span>
                      <span className={`${levelColor} font-semibold mr-2`}>[{log.level}]</span>
                      <span>{log.message}</span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
