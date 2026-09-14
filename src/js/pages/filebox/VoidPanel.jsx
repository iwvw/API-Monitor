import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../../modules/toast.js';
import { formatDateTime } from '../../modules/utils.js';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { ExternalLink, Lock, RefreshCw, Send, X } from '../../components/Icons.jsx';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { VOID_ROOM_TABS } from './constants.js';

export function VoidPanel({
  voidMode,
  setVoidMode,
  voidRooms,
  voidRoomsLoading,
  voidLaunching,
  loadVoidRooms,
  startVoidRoom,
  openVoidRoom,
  closeVoidRoom,
}) {
  return (
    <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
      <SectionCard
        title="房间管理"
        icon={<Send className="h-4 w-4 text-brand" />}
        action={
          <Button size="sm" variant="secondary" onClick={loadVoidRooms} loading={voidRoomsLoading} icon={<RefreshCw className="h-4 w-4" />}>
            刷新
          </Button>
        }
        bodyClassName="grid gap-4"
      >
        <div className="grid gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-4 cq-lg:grid-cols-[minmax(0,1fr)_auto] cq-lg:items-center">
          <div className="flex flex-wrap items-center gap-3 min-w-0">
            <div className="flex shrink-0 items-center gap-2">
              <div className="text-sm font-semibold text-kumo-strong">新建房间</div>
              <Badge variant={voidMode === 'persistent' ? 'success' : 'secondary'}>{voidMode === 'persistent' ? '持久' : '临时'}</Badge>
            </div>
            <div className="w-fit min-w-0 max-w-full">
              <Tabs {...TOOL_TABS_PROPS} value={voidMode} onValueChange={setVoidMode} tabs={VOID_ROOM_TABS} />
            </div>
          </div>
          <Button size="sm" variant="primary" loading={voidLaunching} onClick={startVoidRoom} icon={<ExternalLink className="h-4 w-4" />}>
            创建并打开
          </Button>
        </div>

        <div className="grid gap-3 cq-md:grid-cols-3">
          <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
            <div className="text-[11px] text-kumo-subtle">临时房间</div>
            <div className="mt-1 text-xs font-semibold text-kumo-strong">30 分钟</div>
          </div>
          <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
            <div className="text-[11px] text-kumo-subtle">持久房间</div>
            <div className="mt-1 text-xs font-semibold text-kumo-strong">数据库</div>
          </div>
          <div className="rounded-md border border-kumo-line bg-kumo-base p-3">
            <div className="text-[11px] text-kumo-subtle">在线房间</div>
            <div className="mt-1 text-xs font-semibold text-kumo-strong">{voidRooms.length}</div>
          </div>
        </div>

        <div className="divide-y divide-kumo-line overflow-hidden rounded-md border border-kumo-line">
          {voidRoomsLoading ? (
            Array.from({ length: 2 }).map((_, index) => (
              <div key={index} className="p-3">
                <SkeletonLine className="h-8 w-full" />
              </div>
            ))
          ) : voidRooms.length === 0 ? (
            <div className="p-6 text-center text-xs text-kumo-subtle">暂无房间</div>
          ) : (
            voidRooms.map((room) => {
              const roomId = room.roomId || room.id;
              const mode = room.mode || (room.persistent ? 'persistent' : 'temporary');
              return (
                <div key={roomId} className="grid gap-3 px-4 py-3 cq-lg:grid-cols-[minmax(0,1fr)_auto] cq-lg:items-center">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-sm font-semibold text-kumo-strong">{roomId}</span>
                      <Badge variant={mode === 'persistent' ? 'success' : 'secondary'}>{mode === 'persistent' ? '持久' : '临时'}</Badge>
                      <Badge variant="secondary">{(room.participants || []).filter((item) => item.online).length} 在线</Badge>
                    </div>
                    <div className="mt-1 text-[11px] text-kumo-subtle">{mode === 'persistent' ? '长期房间' : `到期 ${room.expiresAt ? formatDateTime(room.expiresAt) : '-'}`}</div>
                  </div>
                  <div className="flex shrink-0 flex-wrap justify-end gap-2">
                    <Button size="sm" variant="secondary" onClick={() => navigator.clipboard.writeText(`${window.location.origin}/void/${encodeURIComponent(roomId)}`).then(() => toast.success('房间链接已复制'))}>
                      复制
                    </Button>
                    <Button size="sm" variant="primary" onClick={() => openVoidRoom(room)} icon={<ExternalLink className="h-4 w-4" />}>
                      打开
                    </Button>
                    <Button size="sm" variant="secondary-destructive" onClick={() => closeVoidRoom(room)} icon={<X className="h-4 w-4" />}>
                      关闭
                    </Button>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </SectionCard>

      <SectionCard title="传输边界" icon={<Lock className="h-4 w-4 text-brand" />} bodyClassName="grid gap-3 text-xs">
        <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
          <span className="text-kumo-subtle">传输内容</span>
          <span className="font-semibold text-kumo-strong">浏览器直连</span>
        </div>
        <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
          <span className="text-kumo-subtle">房间元数据</span>
          <span className="font-semibold text-kumo-strong">按类型保存</span>
        </div>
        <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
          <span className="text-kumo-subtle">服务器流量</span>
          <span className="font-semibold text-kumo-strong">仅信令</span>
        </div>
        <div className="flex justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
          <span className="text-kumo-subtle">中继</span>
          <span className="font-semibold text-kumo-strong">禁用 TURN</span>
        </div>
      </SectionCard>
    </div>
  );
}
