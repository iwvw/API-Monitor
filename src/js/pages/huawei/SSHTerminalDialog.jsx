import React, { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import { Dialog } from '@cloudflare/kumo/components/dialog';

export default function SSHTerminalDialog({ accountId, instance, onClose }) {
  const containerRef = useRef(null);
  const [status, setStatus] = useState('connecting');

  useEffect(() => {
    if (!containerRef.current) return;
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"Cascadia Code", Menlo, Consolas, monospace',
      theme: { background: '#0b0b0d', foreground: '#e6e6e6', cursor: '#ffcd8a', selectionBackground: '#3a3a44' },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    fit.fit();

    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const params = new URLSearchParams({
      host: instance.publicIp,
      cols: String(term.cols),
      rows: String(term.rows),
    });
    const ws = new WebSocket(`${proto}://${window.location.host}/api/huawei/accounts/${accountId}/ssh?${params.toString()}`);
    const send = (payload) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(payload));
    };

    ws.onopen = () => setStatus('connected');
    ws.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch {
        return;
      }
      if (msg.type === 'data') {
        term.write(msg.data);
      } else if (msg.type === 'status') {
        setStatus(msg.data);
      } else if (msg.type === 'error') {
        term.write(`\r\n\x1b[31m${msg.data}\x1b[0m\r\n`);
        setStatus('error');
      }
    };
    ws.onclose = () => setStatus('closed');
    ws.onerror = () => setStatus('error');

    const dataDisposable = term.onData((data) => send({ type: 'input', data }));

    const observer = new ResizeObserver(() => {
      fit.fit();
      send({ type: 'resize', cols: term.cols, rows: term.rows });
    });
    observer.observe(containerRef.current);

    term.focus();
    return () => {
      dataDisposable.dispose();
      observer.disconnect();
      send({ type: 'disconnect' });
      ws.close();
      term.dispose();
    };
  }, [accountId, instance.publicIp]);

  return (
    <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
      <Dialog className="@container !w-[min(64rem,calc(100vw-2rem))] !max-w-[min(64rem,calc(100vw-2rem))] p-0">
        <Dialog.Title className="flex items-center gap-2 px-4 pt-4 text-sm font-semibold text-kumo-strong">
          SSH 终端 · {instance.name}
          <span className="ml-auto flex items-center gap-2 text-xs font-normal text-kumo-subtle">
            {instance.publicIp}
            <span
              className={`h-2 w-2 rounded-full ${
                status === 'connected' ? 'bg-kumo-success' : status === 'error' || status === 'closed' ? 'bg-kumo-danger' : 'bg-kumo-warning'
              }`}
              title={status}
            />
            {status}
          </span>
        </Dialog.Title>
        <Dialog.Description className="sr-only">华为云实例 SSH 终端</Dialog.Description>
        <div className="p-4">
          <div ref={containerRef} className="h-[28rem] w-full overflow-hidden rounded-md border border-kumo-line bg-[#0b0b0d]" />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
