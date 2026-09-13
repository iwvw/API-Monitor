import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { BRAND_COLOR_FALLBACK } from '../../components/ui/BrandIcon.jsx';
import { HEX_COLOR_PATTERN } from './constants.js';
import { normalizeHexColor } from './utils.js';

const GroupDialog = ({
  showGroupModal,
  setShowGroupModal,
  groupModalMode,
  groupForm,
  setGroupForm,
  handleSaveGroup,
}) => {
  return (
    <Dialog.Root open={showGroupModal} onOpenChange={setShowGroupModal}>
      <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="text-base font-semibold text-kumo-strong mb-1">
          {groupModalMode === 'add' ? '创建新分组' : '编辑分组属性'}
        </Dialog.Title>
        <Dialog.Description className="text-xs text-kumo-subtle mb-4">
          设置分组的名称与卡片主题色值
        </Dialog.Description>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">分组名称</label>
            <Input
              size="sm"
              aria-label="分组名称"
              type="text"
              placeholder="如: 财务, 工作, 个人"
              value={groupForm.name}
              onChange={e => setGroupForm(prev => ({ ...prev, name: e.target.value }))}
              className="w-full"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">卡片标识色值</label>
            <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
              <span
                className="h-7 w-7 rounded-md border border-kumo-line"
                style={{
                  background: HEX_COLOR_PATTERN.test(normalizeHexColor(groupForm.color))
                    ? normalizeHexColor(groupForm.color)
                    : BRAND_COLOR_FALLBACK,
                }}
                aria-hidden="true"
              />
              <Input
                size="sm"
                aria-label="卡片标识色值"
                type="text"
                inputMode="text"
                placeholder="#4285f4"
                value={groupForm.color}
                onChange={e => setGroupForm(prev => ({ ...prev, color: e.target.value }))}
                onBlur={e =>
                  setGroupForm(prev => ({
                    ...prev,
                    color: normalizeHexColor(e.target.value) || BRAND_COLOR_FALLBACK,
                  }))
                }
                className="w-full font-mono text-xs"
              />
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-3 mt-6">
          <Dialog.Close
            render={props => (
              <Button size="sm" {...props} variant="secondary">
                取消
              </Button>
            )}
          />
          <Button size="sm" variant="primary" onClick={handleSaveGroup}>
            保存分组
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
};

export default GroupDialog;
