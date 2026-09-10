import { useState } from 'react';
import { EmptyState, cx } from '../../components/ui/AppPrimitives.jsx';
import { Globe, AntigravityBrand, DeepSeekBrand, CodeBuddyBrand } from '../../components/Icons.jsx';
import { ProxyPoolPlugin } from './plugins/ProxyPoolPlugin.jsx';
import { AntigravityPlugin } from './plugins/AntigravityPlugin.jsx';
import { DS2APIPlugin } from './plugins/DS2APIPlugin.jsx';
import { WorkBuddyPlugin } from './plugins/WorkBuddyPlugin.jsx';

// 插件注册表：后续新增插件只需向 PLUGINS 追加一项（id 唯一、提供详情组件）。
// 插件中心是列表式容器，本身不承载具体模块逻辑。
// 有明确品牌的上游（Antigravity / DeepSeek / CodeBuddy）用 @lobehub/icons 的单色品牌图标；
// 「代理池」是通用基础设施，无对应品牌，沿用通用图标。
const PLUGINS = [
  {
    id: 'proxypool',
    name: '代理池',
    description: '可被插件或网关端点复用的出口代理池。',
    icon: Globe,
    detail: ProxyPoolPlugin,
  },
  {
    id: 'antigravity',
    name: 'Antigravity',
    description: 'Antigravity 账号转 API。',
    icon: AntigravityBrand,
    detail: AntigravityPlugin,
  },
  {
    id: 'ds2api',
    name: 'DS2API',
    description: 'DeepSeek 网页版转 API。',
    icon: DeepSeekBrand,
    detail: DS2APIPlugin,
  },
  {
    id: 'workbuddy',
    name: 'WorkBuddy',
    description: '腾讯 CodeBuddy 转 API。',
    icon: CodeBuddyBrand,
    detail: WorkBuddyPlugin,
  },
];

// OpenAIPluginsPanel：模型网关「插件」中心。
// 左侧插件列表 + 右侧详情，点击左侧切换，右侧常驻显示当前插件详情。
export function OpenAIPluginsPanel() {
  const [selectedId, setSelectedId] = useState(PLUGINS[0]?.id ?? null);
  const selected = PLUGINS.find(p => p.id === selectedId) ?? PLUGINS[0];

  return (
    <div className="grid min-w-0 items-start gap-3 grid-cols-1 cq-lg:grid-cols-3">
      <div className="flex min-w-0 gap-1.5 overflow-x-auto pb-1 cq-sm:flex-col cq-sm:overflow-visible cq-sm:pb-0 cq-lg:pr-1">
        {PLUGINS.length === 0 ? (
          <EmptyState title="暂无插件" description="插件注册表为空。" />
        ) : (
          PLUGINS.map(plugin => {
            const Icon = plugin.icon;
            const isActive = plugin.id === selected?.id;
            return (
              <div
                key={plugin.id}
                role="button"
                tabIndex={0}
                className={cx(
                  'group flex shrink-0 cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5',
                  isActive
                    ? 'border-brand/60 bg-kumo-brand/10'
                    : 'border-kumo-line bg-kumo-base hover:border-brand/60',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand/50'
                )}
                onClick={() => setSelectedId(plugin.id)}
                onKeyDown={e => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    setSelectedId(plugin.id);
                  }
                }}
              >
                <div
                  className={cx(
                    'flex h-9 w-9 shrink-0 items-center justify-center rounded-md border',
                    isActive ? 'border-brand/60 bg-kumo-brand/10 text-brand' : 'border-kumo-line bg-kumo-recessed text-brand'
                  )}
                >
                  <Icon className="h-4 w-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className={cx('truncate text-sm', isActive ? 'font-medium text-kumo-strong' : 'font-medium text-kumo-strong')}>
                      {plugin.name}
                    </span>
                  </div>
                  <p className="mt-0.5 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{plugin.description}</p>
                </div>
              </div>
            );
          })
        )}
      </div>

      <div className="min-w-0 cq-lg:col-span-2">
        {selected ? (
          <selected.detail />
        ) : (
          <EmptyState title="请选择插件" description="从左侧选择一个插件查看详情。" />
        )}
      </div>
    </div>
  );
}
