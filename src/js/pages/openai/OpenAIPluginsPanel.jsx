import { useState } from 'react';
import { EmptyState, cx } from '../../components/ui/AppPrimitives.jsx';
import { ChevronRight, Rocket } from '../../components/Icons.jsx';
import { Vertex2APIPlugin } from './plugins/Vertex2APIPlugin.jsx';

// 插件注册表：后续新增插件只需向 PLUGINS 追加一项（id 唯一、提供详情组件）。
// 插件中心是列表式容器，本身不承载具体模块逻辑。
const PLUGINS = [
  {
    id: 'vertex2api',
    name: 'Vertex to API',
    shortName: '免费 Gemini 中继',
    description: '内嵌免费 Gemini 代理引擎，OpenAI 兼容。可复用模型网关端点代理池出网，并一键接入网关端点列表。',
    icon: Rocket,
    detail: Vertex2APIPlugin,
  },
];

// OpenAIPluginsPanel：模型网关「插件」中心（列表式，可扩展）。
export function OpenAIPluginsPanel() {
  const [activePlugin, setActivePlugin] = useState(null);

  if (activePlugin) {
    const Detail = activePlugin.detail;
    return <Detail onBack={() => setActivePlugin(null)} />;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {PLUGINS.length === 0 ? (
        <EmptyState title="暂无插件" description="插件注册表为空。" />
      ) : (
        <div className="grid gap-1.5 cq-lg:grid-cols-2">
          {PLUGINS.map(plugin => {
            const Icon = plugin.icon;
            return (
              <div
                key={plugin.id}
                role="button"
                tabIndex={0}
                className={cx(
                  'group flex min-h-15 cursor-pointer items-center gap-3 rounded-lg border border-kumo-line bg-kumo-base px-3 py-2.5 transition-colors hover:border-brand/60 cq-sm:min-h-16',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand/50'
                )}
                onClick={() => setActivePlugin(plugin)}
                onKeyDown={e => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    setActivePlugin(plugin);
                  }
                }}
              >
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed text-brand">
                  <Icon className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-semibold text-kumo-strong">{plugin.name}</span>
                    <span className="shrink-0 text-xs text-kumo-subtle">{plugin.shortName}</span>
                  </div>
                  <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{plugin.description}</p>
                </div>
                <ChevronRight className="h-4 w-4 shrink-0 text-kumo-subtle transition-transform group-hover:translate-x-0.5 group-hover:text-kumo-strong" />
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
