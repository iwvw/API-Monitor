import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Save } from '../../components/Icons.jsx';

export function ChannelDialog({
  showChannelModal,
  setShowChannelModal,
  channelForm,
  setChannelForm,
  syncEmailSecure,
  handleSaveChannel,
  notificationSaving,
}) {
  return (
    <Dialog.Root open={showChannelModal} onOpenChange={setShowChannelModal}>
      <Dialog className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="text-base font-semibold text-kumo-strong mb-1 select-none">
          {channelForm.id ? '编辑通知渠道' : '新建通知渠道'}
        </Dialog.Title>
        <Dialog.Description className="text-xs text-kumo-subtle mb-4 select-none">
          配置告警投递渠道
        </Dialog.Description>

        <div className="-mx-1 min-h-0 flex-1 space-y-4 overflow-y-auto px-1 pb-2 pr-2 scrollbar-thin">
          {/* Channel Type */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">渠道类型</label>
            <Select alignItemWithTrigger size="sm"
              aria-label="渠道类型"
              value={channelForm.type}
              disabled={channelForm.id !== null}
              onValueChange={(value) => setChannelForm(prev => ({ ...prev, type: String(value) }))}
              className="w-full"
              items={[
                { value: 'email', label: '电子邮件' },
                { value: 'telegram', label: 'Telegram Bot' },
              ]}
            />
          </div>

          {/* Channel Name */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-kumo-subtle">显示名称 *</label>
            <Input size="sm"
              aria-label="显示名称"
              type="text"
              placeholder="如：运维值班邮箱"
              value={channelForm.name}
              onChange={(e) => setChannelForm(prev => ({ ...prev, name: e.target.value }))}
              className="w-full"
            />
          </div>

          {/* Email configuration */}
          {channelForm.type === 'email' && (
            <>
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">SMTP 主机 *</label>
                <Input size="sm"
                  aria-label="SMTP 主机服务器地址"
                  type="text"
                  placeholder="smtp.gmail.com / smtp.exmail.qq.com"
                  value={channelForm.config.host}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, host: e.target.value }
                  }))}
                  className="w-full"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">连接端口</label>
                  <Input size="sm"
                    aria-label="连接端口"
                    type="number"
                    placeholder="465"
                    value={channelForm.config.port}
                    onChange={(e) => syncEmailSecure(parseInt(e.target.value, 10) || 0)}
                    className="w-full font-mono"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">加密方式</label>
                  <Select alignItemWithTrigger size="sm"
                    aria-label="加密安全协议"
                    value={channelForm.config.secure ? 'ssl' : 'tls'}
                    onValueChange={(value) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, secure: String(value) === 'ssl' }
                    }))}
                    className="w-full"
                    items={[
                      { value: 'tls', label: 'STARTTLS / TLS（587）' },
                      { value: 'ssl', label: 'SSL（465）' },
                    ]}
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">发件邮箱 *</label>
                <Input size="sm"
                  aria-label="发件账户邮箱账号"
                  type="email"
                  placeholder="account@gmail.com"
                  value={channelForm.config.auth.user}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: {
                      ...prev.config,
                      auth: { ...prev.config.auth, user: e.target.value }
                    }
                  }))}
                  className="w-full"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">SMTP 授权码 *</label>
                <Input size="sm"
                  aria-label="SMTP 授权口令"
                  type="text"
                  placeholder="your_smtp_app_password"
                  value={channelForm.config.auth.pass}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: {
                      ...prev.config,
                      auth: { ...prev.config.auth, pass: e.target.value }
                    }
                  }))}
                  className="w-full"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">发件人名（可选）</label>
                <Input size="sm"
                  aria-label="发件人昵称"
                  type="text"
                  placeholder="告警机器人"
                  value={channelForm.config.sender_name}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, sender_name: e.target.value }
                  }))}
                  className="w-full"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">收件邮箱 *</label>
                <Input size="sm"
                  aria-label="收件目的邮箱"
                  type="email"
                  placeholder="recipient@domain.com"
                  value={channelForm.config.to}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, to: e.target.value }
                  }))}
                  className="w-full"
                />
              </div>
            </>
          )}

          {/* Telegram configuration */}
          {channelForm.type === 'telegram' && (
            <>
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">Telegram Bot 令牌 *</label>
                <Input size="sm"
                  aria-label="Telegram Bot 令牌"
                  type="text"
                  placeholder="123456789:ABCDefgh..."
                  value={channelForm.config.bot_token}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, bot_token: e.target.value }
                  }))}
                  className="w-full font-mono"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">Chat ID *</label>
                <Input size="sm"
                  aria-label="接收目标 Chat ID"
                  type="text"
                  placeholder="如：123456789 或 -100987654321"
                  value={channelForm.config.chat_id}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, chat_id: e.target.value }
                  }))}
                  className="w-full font-mono"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">代理（可选）</label>
                <Input size="sm"
                  aria-label="Telegram 代理地址"
                  type="text"
                  placeholder="如：http://127.0.0.1:7890"
                  value={channelForm.config.proxy_url || ''}
                  onChange={(e) => setChannelForm(prev => ({
                    ...prev,
                    config: { ...prev.config, proxy_url: e.target.value }
                  }))}
                  className="w-full font-mono"
                />
              </div>
            </>
          )}

          {/* Status enable toggle */}
          <div className="flex items-center justify-between border-t border-kumo-line pt-4 select-none">
            <span className="text-xs font-semibold text-kumo-strong">启用渠道</span>
            <Switch
              checked={!!channelForm.enabled}
              onCheckedChange={(checked) => setChannelForm(prev => ({ ...prev, enabled: checked }))}
              size="sm"
            />
          </div>
        </div>

        <div className="flex justify-end gap-3 mt-6 border-t border-kumo-line pt-4 select-none">
          <Dialog.Close
            render={(props) => (
              <Button size="sm" {...props} variant="secondary">
                取消
              </Button>
            )}
          />
          <Button size="sm" variant="primary" onClick={handleSaveChannel} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
            保存渠道
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
