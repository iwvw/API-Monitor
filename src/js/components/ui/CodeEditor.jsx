import React, { useEffect, useMemo, useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { EditorView } from '@codemirror/view';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';
import { findCodeLanguage, getCodeLanguageName } from '../../modules/codeEditorLanguage.js';

const baseEditorTheme = EditorView.theme({
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
    color: 'var(--text-color-kumo-subtle)',
    // 抹掉 CodeMirror 基座主题自带的行号右 border（.cm-gutters-before）：
    // 仅改背景不足以移除它，必须显式清除左右 border（风格 none）。
    borderLeft: 'none',
    borderRight: 'none',
  },
  // 选中区域：跟随主题语义色，避免默认浏览器蓝与亮/暗主题冲突
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'color-mix(in oklab, var(--color-brand) 25%, transparent)',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: 'var(--color-brand)',
  },
  '&.cm-focused': { outline: 'none' },
}, { dark: false });

// 行高亮背景（默认开；查看类只读场景可关闭）
const activeLineTheme = EditorView.theme({
  '.cm-activeLine, .cm-activeLineGutter': {
    backgroundColor: 'color-mix(in oklab, var(--color-brand) 7%, transparent)',
  },
}, { dark: false });

// 行号右侧竖线（默认开；查看类只读场景可关闭）
const gutterBorderTheme = EditorView.theme({
  '.cm-gutters': {
    borderRight: '1px solid var(--color-kumo-fill)',
  },
}, { dark: false });

// 语法高亮配色：全部使用 kumo 语义色变量，亮/暗主题自动适配。
// 经 syntaxHighlighting(HighlightStyle) 注入，生成 tok-* 类并替换默认配色。
const syntaxHighlightStyle = HighlightStyle.define([
  // 关键字/控制流
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword, t.definitionKeyword], color: 'var(--color-brand)', fontWeight: '600' },
  // 类型/类/命名空间
  { tag: [t.typeName, t.className, t.namespace, t.tagName], color: 'var(--color-kumo-info)' },
  // 函数/宏
  { tag: [t.function, t.macroName, t.labelName], color: 'var(--color-kumo-strong)' },
  // 属性/成员/属性值
  { tag: [t.propertyName, t.attributeName], color: 'var(--color-kumo-info)' },
  // 变量/普通名称
  { tag: [t.variableName, t.name], color: 'var(--text-color-kumo-default)' },
  // 字符串/文档串/字符
  { tag: [t.string, t.special(t.string), t.docString, t.character, t.attributeValue], color: 'var(--color-kumo-success)' },
  // 数字
  { tag: [t.number, t.integer, t.float], color: 'var(--color-kumo-warning)' },
  // 布尔/空/字面量/正则
  { tag: [t.bool, t.null, t.atom, t.literal, t.regexp], color: 'var(--color-kumo-danger)' },
  // 注释
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: 'color-mix(in oklab, var(--text-color-kumo-default) 55%, transparent)', fontStyle: 'italic' },
  // 运算符
  { tag: [t.operator, t.arithmeticOperator, t.logicOperator, t.compareOperator, t.updateOperator, t.derefOperator], color: 'var(--color-kumo-strong)' },
  // 标点/括号
  { tag: [t.punctuation, t.bracket, t.angleBracket, t.squareBracket, t.paren, t.brace], color: 'var(--color-kumo-subtle)' },
  // 转义/特殊
  { tag: [t.escape, t.special(t.name)], color: 'var(--color-kumo-info)', fontStyle: 'italic' },
]);

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
  showActiveLine = false,
  showGutterBorder = false,
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
      baseEditorTheme,
      syntaxHighlighting(syntaxHighlightStyle),
      ...(showActiveLine ? [activeLineTheme] : []),
      ...(showGutterBorder ? [gutterBorderTheme] : []),
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
    [label, languageSupport, lineWrapping, readOnly, showActiveLine, showGutterBorder]
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
        theme={baseEditorTheme}
        extensions={extensions}
        editable={!readOnly}
        readOnly={readOnly}
        height="100%"
        basicSetup={{
          autocompletion: !readOnly,
          bracketMatching: true,
          closeBrackets: !readOnly,
          foldGutter: true,
          highlightActiveLine: showActiveLine && !readOnly,
          highlightActiveLineGutter: showActiveLine && !readOnly,
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
