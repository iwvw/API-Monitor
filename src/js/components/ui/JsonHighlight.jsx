import React, { useEffect, useState } from 'react';

/**
 * JSON 语法高亮只读展示（Shiki 引擎，跟随系统亮暗主题）。
 * 与 CodeEditor（CodeMirror，编辑用）分工：本组件只做展示。
 * 主题：dark 用 github-dark，light 用 github-light，与 kumo 语义相近。
 */
const THEMES = { dark: 'github-dark', light: 'github-light' };

let highlighterPromise = null;

function loadHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import('shiki').then(({ createHighlighter }) =>
      createHighlighter({
        themes: Object.values(THEMES),
        langs: ['json'],
      })
    );
  }
  return highlighterPromise;
}

function currentMode() {
  if (typeof document === 'undefined') return 'light';
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light';
}

export default function JsonHighlight({ code, className = '', minHeight }) {
  const [mode, setMode] = useState(currentMode);
  const [html, setHtml] = useState('');
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    const onModeChange = () => setMode(currentMode());
    const observer = new MutationObserver(onModeChange);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    loadHighlighter()
      .then((shiki) => {
        if (!active) return;
        setHtml(shiki.codeToHtml(code, { lang: 'json', theme: THEMES[mode] }));
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => {
      active = false;
      observer.disconnect();
    };
  }, [code, mode]);

  if (failed) {
    return <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-default">{code}</pre>;
  }
  if (!html) {
    return <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-default">{code}</pre>;
  }
  return (
    <div
      className={`shiki-json-block ${className}`.trim()}
      style={minHeight ? { minHeight } : undefined}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}