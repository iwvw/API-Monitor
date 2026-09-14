import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Sidebar } from '@cloudflare/kumo';
import { ChatsCircle } from '@phosphor-icons/react';
import { Check, Trash, WechatBrand, TelegramBrand, WeComBrand } from '../../Icons.jsx';
import { isBotSession, sessionSourceLabel } from './sessionHelpers.js';
import { formatSessionDate } from './constants.jsx';

/* 会话列表条目（全屏侧栏与下拉菜单共用）：机器人会话带来源标签 */
function SessionItem({ s, active, deleteArmed, onSelect, onDelete }) {
  const bot = isBotSession(s);
  // 不同渠道用对应品牌图标（TG/微信），其余（web/BOT 通用）沿用聊天气泡图标
  let ChannelIcon = ChatsCircle;
  if (s.channelType === 'wechat') ChannelIcon = WechatBrand;
  else if (s.channelType === 'telegram') ChannelIcon = TelegramBrand;
  else if (s.channelType === 'wecom') ChannelIcon = WeComBrand;
  return (
    <div className="group relative">
      <Sidebar.MenuButton
        active={active}
        aria-current={active ? 'page' : undefined}
        onClick={onSelect}
        icon={
          <ChannelIcon
            weight={s.channelType === 'wechat' || s.channelType === 'telegram' || s.channelType === 'wecom' ? undefined : 'duotone'}
            className={`${s.channelType === 'wechat' || s.channelType === 'telegram' || s.channelType === 'wecom' ? 'size-5' : 'size-4'} shrink-0 transition-all duration-200 ${
              active
                ? 'text-brand'
                : 'text-kumo-subtle group-hover:scale-110 group-hover:text-kumo-default'
            }`}
          />
        }
        className={`${active ? '!bg-brand/10' : ''} !px-2`}
      >
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span
              className={`truncate text-xs ${
                active
                  ? 'font-semibold text-kumo-default'
                  : 'font-medium text-kumo-subtle group-hover:text-kumo-default'
              }`}
            >
              {s.title || '新对话'}
            </span>
            {bot && (
              <span className="shrink-0 rounded bg-kumo-warning/10 px-1 py-px text-[9px] font-semibold leading-4 text-kumo-warning">
                {sessionSourceLabel(s.source, s.channelType)}
              </span>
            )}
          </span>
          <span className="truncate text-[10px] text-kumo-subtle/70">
            {formatSessionDate(s.createdAt)}
          </span>
        </span>
      </Sidebar.MenuButton>
      <Button
        size="sm"
        shape="square"
        variant={deleteArmed ? 'destructive' : 'ghost'}
        aria-label="删除会话"
        onClick={() => onDelete(s.id)}
        className={`!absolute right-1.5 top-1/2 z-10 -translate-y-1/2 !h-6 !w-6 !rounded-md !shadow-sm opacity-0 transition-all duration-200 group-hover:opacity-100 ${
          deleteArmed
            ? '!opacity-100 !bg-kumo-danger !text-kumo-inverse'
            : '!bg-kumo-base ring-1 ring-kumo-line hover:!bg-kumo-tint hover:!text-kumo-danger'
        }`}
      >
        {deleteArmed ? <Check className="h-3 w-3" /> : <Trash className="h-3 w-3" />}
      </Button>
    </div>
  );
}

export default SessionItem;

