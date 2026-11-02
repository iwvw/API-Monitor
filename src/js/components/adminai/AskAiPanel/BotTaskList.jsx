import React from 'react';
import { Sidebar } from '@cloudflare/kumo';
import { Terminal, ChevronDown } from '../../Icons.jsx';
import SessionItem from './SessionItem.jsx';

/* 任务会话列表：定时任务（source=cron）按任务名分组，组默认折叠，点击组头展开 */
function BotTaskList({
  taskGroups,
  collapsedTaskGroups,
  onToggleTaskGroup,
  activeSessionId,
  deleteIsArmed,
  onSelect,
  onDelete,
}) {
  return (
    <>
      {taskGroups.map((group) => {
        const collapsed = collapsedTaskGroups === null || collapsedTaskGroups.has(group.title);
        return (
          <div key={group.title}>
            <Sidebar.MenuButton
              active={!collapsed}
              onClick={() => onToggleTaskGroup(group.title)}
              icon={<Terminal className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
              className="!px-2"
            >
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-kumo-default">
                {group.title}
              </span>
              <span className="shrink-0 text-[10px] text-kumo-subtle">{group.items.length} 次</span>
              <ChevronDown
                className={`h-3 w-3 shrink-0 text-kumo-subtle transition-transform duration-base ${collapsed ? '' : 'rotate-180'}`}
              />
            </Sidebar.MenuButton>
            {!collapsed && (
              <div className="ml-2.5 border-l border-kumo-line pl-1.5">
                {group.items.map((s) => (
                  <SessionItem
                    key={s.id}
                    s={s}
                    active={s.id === activeSessionId}
                    deleteArmed={deleteIsArmed(s.id)}
                    onSelect={() => onSelect(s)}
                    onDelete={onDelete}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </>
  );
}

export default BotTaskList;

