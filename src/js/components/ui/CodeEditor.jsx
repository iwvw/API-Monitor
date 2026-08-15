import React, { useEffect, useMemo, useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { EditorView } from '@codemirror/view';
import { findCodeLanguage, getCodeLanguageName } from '../../modules/codeEditorLanguage.js';

const editorTheme = EditorView.theme({
  '&': {
    height: '100%',
    backgroundColor: 'transparent',
    color: 'var(--text-color-kumo-default)',
    fontSize: '13px',
  },
  '.cm-scroller': {
    fontFamily: 'var(--font-sans)',
    lineHeight: '20px',
    overflow: 'auto',
    scrollbarColor: 'var(--color-kumo-fill) transparent',
  },
  '.cm-gutters': {
    backgroundColor: 'color-mix(in oklab, var(--color-kumo-base) 88%, var(--color-kumo-overlay))',
    borderRight: '1px solid var(--color-kumo-fill)',
    color: 'var(--text-color-kumo-subtle)',
  },
  '.cm-activeLine, .cm-activeLineGutter': {
    backgroundColor: 'color-mix(in oklab, var(--color-kumo-brand) 7%, transparent)',
  },
  '&.cm-focused': { outline: 'none' },
}, { dark: false });

const shiftWheelHorizontalScroll = EditorView.domEventHandlers({
  wheel(event, view) {
    if (!event.shiftKey) return false;
    const delta = Math.abs(event.deltaY) >= Math.abs(event.deltaX) ? event.deltaY : event.deltaX;
    if (!delta) return false;
    view.scrollDOM.scrollLeft += delta;
    event.preventDefault();
    return true;
  },
});

export default function CodeEditor({
  value = '',
  onChange,
  fileName = '',
  language = '',
  label = '代码编辑器',
  readOnly = false,
  className = '',
  minHeight = '16rem',
  height,
  placeholder = '',
  showLanguage = true,
  showHeader = true,
  lineWrapping = true,
  variant = 'default',
}) {
  const description = useMemo(() => findCodeLanguage({ fileName, language }), [fileName, language]);
  const [languageSupport, setLanguageSupport] = useState(description?.support || null);

  useEffect(() => {
    let active = true;
    setLanguageSupport(description?.support || null);
    if (description && !description.support) {
      description
        .load()
        .then(support => {
          if (active) setLanguageSupport(support);
        })
        .catch(() => {
          if (active) setLanguageSupport(null);
        });
    }
    return () => {
      active = false;
    };
  }, [description]);

  const extensions = useMemo(
    () => [
      editorTheme,
      shiftWheelHorizontalScroll,
      EditorView.contentAttributes.of({
        'aria-label': label,
        'aria-readonly': String(readOnly),
        autocapitalize: 'off',
        autocomplete: 'off',
        spellcheck: 'false',
      }),
      ...(lineWrapping ? [EditorView.lineWrapping] : []),
      ...(languageSupport ? [languageSupport] : []),
    ],
    [label, languageSupport, lineWrapping, readOnly]
  );
  const languageName = getCodeLanguageName({ fileName, language });
  const isEmbedded = variant === 'embedded';

  return (
    <div
      className={`app-code-editor ${isEmbedded ? 'app-code-editor--embedded' : ''} ${className}`.trim()}
      style={{ minHeight, ...(height ? { height } : {}) }}
    >
      {showHeader ? (
        <div className="app-code-editor-header">
          <span className="truncate font-semibold">{label}</span>
          {showLanguage ? <span className="shrink-0 font-mono">{languageName}</span> : null}
        </div>
      ) : null}
      <CodeMirror
        aria-label={label}
        value={String(value ?? '')}
        onChange={(nextValue, viewUpdate) => onChange?.(nextValue, viewUpdate)}
        placeholder={placeholder}
        theme={editorTheme}
        extensions={extensions}
        editable={!readOnly}
        readOnly={readOnly}
        height="100%"
        basicSetup={{
          autocompletion: !readOnly,
          bracketMatching: true,
          closeBrackets: !readOnly,
          foldGutter: true,
          highlightActiveLine: !readOnly,
          highlightActiveLineGutter: !readOnly,
          highlightSelectionMatches: true,
          lineNumbers: true,
          searchKeymap: true,
        }}
      />
      {!showHeader && showLanguage ? (
        <div className="app-code-editor-status">{languageName}</div>
      ) : null}
    </div>
  );
}
