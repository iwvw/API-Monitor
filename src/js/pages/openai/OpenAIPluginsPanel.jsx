import { useState } from 'react';
import { AppCard, EmptyState, PageStack } from '../../components/ui/AppPrimitives.jsx';
import { Rocket } from '../../components/Icons.jsx';
import { Vertex2APIPlugin } from './plugins/Vertex2APIPlugin.jsx';

// 插件注册表：后续新增插件只需向 PLUGINS 追加一项（id 唯一、提供详情组件）。
// 插件中心是卡片式容器，本身不承载具体模块逻辑。
const PLUGINS = [
  {
    id: 'vertex2api',
    name: 'Vertex to API',
    shortName: '免费 Gemini 中继',
    description: '内嵌免费 Gemini 代理引擎，OpenAI 兼容。可复用模型网关端点代理池出网，并一键接入网关端点列表。',
    icon: Rocket,
    statusHint: '免费 Gemini 中继',
    detail: Vertex2APIPlugin,
  },
];

// OpenAIPluginsPanel：模型网关「插件」中心（卡片式，可扩展）。
export function OpenAIPluginsPanel() {
  const [activePlugin, setActivePlugin] = useState(null);

  if (activePlugin) {
    const Detail = activePlugin.detail;
    return <Detail onBack={() => setActivePlugin(null)} />;
  }

  return (
    <PageStack viewport>
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        <AppCard title="插件中心" description="模型网关的扩展插件。每个插件独立开关、独立配置，可整体移除。">
          <p className="text-xs text-kumo-subtle">
            插件以内嵌引擎或外部服务的方式扩展网关能力；接入后会在下方以卡片展示，点击进入配置。
          </p>
        </AppCard>

        <div className="grid min-w-0 grid-cols-1 gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3">
          {PLUGINS.map(plugin => {
            const Icon = plugin.icon;
            return (
              <AppCard key={plugin.id} interactive className="cursor-pointer" onClick={() => setActivePlugin(plugin)}>
                <div className="flex min-w-0 items-start gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Icon className="h-5 w-5" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-semibold text-kumo-strong">{plugin.name}</div>
                    <div className="mt-0.5 text-xs text-kumo-subtle">{plugin.shortName}</div>
                    <p className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{plugin.description}</p>
                  </div>
                </div>
              </AppCard>
            );
          })}
        </div>

        {PLUGINS.length === 0 && (
          <EmptyState title="暂无插件" description="插件注册表为空。" />
        )}
      </div>
    </PageStack>
  );
}
