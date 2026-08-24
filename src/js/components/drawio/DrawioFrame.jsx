import React, {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from 'react';
import { Loader } from '@cloudflare/kumo/components/loader';

const EDITOR_PATH = '/vendor/drawio/';

const DrawioFrame = forwardRef(function DrawioFrame(
  { xml = '', theme = 'light', readOnly = false, onChange, onReady, onError },
  ref
) {
  const iframeRef = useRef(null);
  const xmlRef = useRef(xml);
  const pendingExportRef = useRef(null);
  const readyRef = useRef(false);
  const [status, setStatus] = useState('loading');

  useEffect(() => {
    xmlRef.current = xml;
  }, [xml]);

  const post = useCallback(payload => {
    iframeRef.current?.contentWindow?.postMessage(JSON.stringify(payload), window.location.origin);
  }, []);

  const requestExport = useCallback(
    (format = 'xml', options = {}) =>
      new Promise((resolve, reject) => {
        if (!readyRef.current) {
          reject(new Error('编辑器尚未就绪'));
          return;
        }
        window.clearTimeout(pendingExportRef.current?.timer);
        const timer = window.setTimeout(() => {
          pendingExportRef.current = null;
          reject(new Error('编辑器导出超时'));
        }, 15000);
        pendingExportRef.current = { resolve, reject, format, timer };
        post({ action: 'export', format, spinKey: 'export', ...options });
      }),
    [post]
  );

  useImperativeHandle(
    ref,
    () => ({
      getXML: () => requestExport('xml'),
      exportSVG: () => requestExport('svg', { embedImages: true }),
      exportPNG: (scale = 3) => requestExport('png', { border: 0, scale }),
      load: nextXML => post({ action: 'load', xml: nextXML, autosave: 1 }),
    }),
    [post, requestExport]
  );

  useEffect(() => {
    const handleMessage = event => {
      if (
        event.origin !== window.location.origin ||
        event.source !== iframeRef.current?.contentWindow
      )
        return;
      let message = event.data;
      if (typeof message === 'string') {
        try {
          message = JSON.parse(message);
        } catch {
          return;
        }
      }
      if (!message || typeof message !== 'object') return;

      if (message.event === 'init') {
        readyRef.current = false;
        post({ action: 'load', xml: xmlRef.current, autosave: readOnly ? 0 : 1 });
        post({
          action: 'configure',
          config: {
            darkMode: theme === 'dark',
            defaultLibraries: 'general;basic;arrows2;flowchart;uml;er',
          },
        });
        readyRef.current = true;
        setStatus('ready');
        onReady?.();
        return;
      }
      if (message.event === 'autosave' || message.event === 'save') {
        if (typeof message.xml === 'string') {
          xmlRef.current = message.xml;
          onChange?.(message.xml);
        }
        return;
      }
      if (message.event === 'export') {
        const pending = pendingExportRef.current;
        if (!pending) return;
        window.clearTimeout(pending.timer);
        pendingExportRef.current = null;
        pending.resolve(message.data || message.xml || '');
        return;
      }
      if (message.event === 'exit' && typeof message.xml === 'string') {
        onChange?.(message.xml);
      }
    };
    window.addEventListener('message', handleMessage);
    return () => window.removeEventListener('message', handleMessage);
  }, [onChange, onReady, post, readOnly, theme]);

  useEffect(() => {
    if (status === 'ready') post({ action: 'configure', config: { darkMode: theme === 'dark' } });
  }, [post, status, theme]);

  const src = `${EDITOR_PATH}?embed=1&proto=json&spin=1&saveAndExit=0&noSaveBtn=1&noExitBtn=1&ui=kennedy&lang=zh&libs=general%3Bbasic%3Barrows2%3Bflowchart%3Buml%3Ber&math=1&dark=${theme === 'dark' ? '1' : '0'}`;

  return (
    <div className="relative flex min-h-0 flex-1 bg-kumo-base">
      {status === 'loading' && (
        <div className="absolute inset-0 z-10 flex items-center justify-center bg-kumo-base">
          <Loader />
        </div>
      )}
      {status === 'error' && (
        <div className="absolute inset-0 z-10 flex items-center justify-center p-6 text-sm text-kumo-error">
          图编辑器资源加载失败，检查自托管资源
        </div>
      )}
      <iframe
        ref={iframeRef}
        src={src}
        className="h-full min-h-[32rem] w-full border-0"
        title="Draw.io 编辑器"
        onError={() => {
          readyRef.current = false;
          setStatus('error');
          onError?.();
        }}
      />
    </div>
  );
});

export default DrawioFrame;
