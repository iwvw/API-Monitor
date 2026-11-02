import { Button } from '@cloudflare/kumo/components/button';
import { Popover } from '@cloudflare/kumo/components/popover';
import { Terminal as TerminalIcon, DesktopDisplay, Menu } from '../../components/Icons.jsx';

export function ServerConnectionActions({
  remoteDesktopAvailable,
  terminalLabel,
  terminalDisabled,
  onOpenRemoteDesktop,
  onOpenTerminal,
  buttonClassName = '',
}) {
  const terminalButton = (
    <Button
      shape="square"
      size="sm"
      variant="secondary"
      className={buttonClassName}
      title={terminalDisabled ? '终端不可用' : terminalLabel}
      aria-label={terminalDisabled ? '终端不可用' : terminalLabel}
      icon={<TerminalIcon className="h-3.5 w-3.5" />}
      onClick={onOpenTerminal}
      disabled={terminalDisabled}
    />
  );

  if (!remoteDesktopAvailable) return terminalButton;

  return (
    <Popover>
      <Popover.Trigger
        render={(
          <Button
            shape="square"
            size="sm"
            variant="secondary"
            className={buttonClassName}
            title="连接操作"
            aria-label="连接操作"
            icon={<Menu className="h-3.5 w-3.5" />}
          />
        )}
      />
      <Popover.Content side="left" align="center" className="w-44 p-2" onClick={event => event.stopPropagation()}>
        <Popover.Title className="mb-2 text-xs font-semibold text-kumo-strong">连接操作</Popover.Title>
        <div className="grid gap-1">
          <Button size="sm" variant="secondary" className="w-full justify-start" icon={<DesktopDisplay className="h-3.5 w-3.5" />} onClick={onOpenRemoteDesktop}>
            远程桌面
          </Button>
          <Button size="sm" variant="secondary" className="w-full justify-start" icon={<TerminalIcon className="h-3.5 w-3.5" />} onClick={onOpenTerminal} disabled={terminalDisabled}>
            {terminalLabel}
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}
