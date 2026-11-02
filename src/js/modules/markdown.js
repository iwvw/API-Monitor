/**
 * Markdown 渲染模块 — 独立于 utils.js，避免 marked/DOMPurify/katex
 * 被只需要格式化函数的页面连带打包。
 */

import { marked } from 'marked';
import DOMPurify from 'dompurify';
import katex from 'katex';
import 'katex/dist/katex.min.css';

/**
 * DOMPurify 允许的 URI 白名单：
 * 放行 http(s)/ftp/mailto/tel/callto/cid/xmpp 协议与
 * data:image/(png|jpe?g|gif|webp|bmp|avif|ico)[;,] 位图 data URL（Base64 图片预览）；
 * javascript:/vbscript:/data:text/html 等一律拦截。
 */
export const ALLOWED_URI_REGEXP =
  /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|cid|xmpp:|data:image\/(?:png|jpe?g|gif|webp|bmp|avif|ico)[;,])|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i;

/**
 * 渲染 Markdown 为 HTML (安全模式)
 */
export function renderMarkdown(text) {
  if (text === undefined || text === null) return '';

  let source;

  // 1. 处理多模态数组 (OpenAI 格式)
  if (Array.isArray(text)) {
    source = text
      .map(part => {
        if (!part) return '';
        if (typeof part === 'string') return part;
        if (typeof part === 'object') {
          // 支持带 thought 属性的文本块 (Gemini/DeepSeek 后端适配)
          if (part.type === 'text' || !part.type) {
            const content = part.text || part.content || '';
            if (part.thought) {
              return `<think>${content}</think>`;
            }
            return content;
          }
          if (part.type === 'image_url') {
            const url = part.image_url?.url || '';
            // 极致去空白处理：HTML 保持在一行，杜绝段落包裹
            return `<div class="msg-image-container"><a class="img-preview-trigger"><img src="${url}" class="msg-inline-image" alt="图片" /></a></div>`;
          }
          return `\`${JSON.stringify(part)}\``;
        }
        return String(part);
      })
      .join(''); // 使用空字符串拼接
  }
  // 2. 处理单对象
  else if (typeof text === 'object') {
    source = `\`\`\`json\n${JSON.stringify(text, null, 2)}\n\`\`\``;
  }
  // 3. 处理字符串 (防 object object 逃逸)
  else {
    source = String(text);
    if (source === '[object Object]') {
      try {
        source = '```json\n' + JSON.stringify(text, null, 2) + '\n```';
      } catch (e) { }
    }
  }

  // 4. 预处理数学公式 (LaTeX) - 使用占位符保护公式不被 marked 破坏
  const mathBlocks = [];
  const addMath = (content, displayMode) => {
    try {
      const html = katex.renderToString(content.trim(), { displayMode, throwOnError: false });
      mathBlocks.push(displayMode ? `<div class="math-block">${html}</div>` : `<span class="math-inline">${html}</span>`);
      return `@@MATH_${mathBlocks.length - 1}@@`;
    } catch (e) {
      return content;
    }
  };

  // 4.1 块级公式 (优先处理)
  source = source.replace(/\$\$([\s\S]+?)\$\$/g, (m, c) => addMath(c, true));
  source = source.replace(/\\\[([\s\S]+?)\\\]/g, (m, c) => addMath(c, true));

  // 4.2 行内公式
  source = source.replace(/\\\(([\s\S]+?)\\\)/g, (m, c) => addMath(c, false));
  source = source.replace(/\$([^\s$][^$]*?[^\s$])\$/g, (m, c) => addMath(c, false));
  source = source.replace(/\$([^\s$])\$/g, (m, c) => addMath(c, false));

  // 5. 预处理思考标签 <think>/<thinking>/<think_nya> 等变体 (DeepSeek/Gemini)
  source = source.replace(/<(think(?:ing|_\w+)?)\s*>([\s\S]*?)<\/\1>/gi, (match, tag, content) => {
    return `<details class="reasoning-details"><summary><i class="fas fa-brain" style="margin-right: 6px;"></i>思考过程</summary><div class="reasoning-content-inner">\n\n${content}\n\n</div></details>`;
  });

  try {
    // 渲染 Markdown
    let rawHtml = marked.parse(source, { breaks: true, gfm: true });

    // 6. 还原数学公式
    mathBlocks.forEach((html, index) => {
      rawHtml = rawHtml.replace(`@@MATH_${index}@@`, html);
    });

    return DOMPurify.sanitize(rawHtml, {
      ADD_ATTR: ['target', 'title', 'rel', 'open', 'class', 'style', 'aria-hidden', 'viewBox', 'd', 'fill'],
      ADD_TAGS: ['a', 'img', 'div', 'details', 'summary', 'i', 'span', 'svg', 'path', 'math', 'semantics', 'mrow', 'annotation', 'mstyle', 'mo', 'mi', 'mn', 'msup', 'msub', 'mfrac', 'msqrt', 'root', 'mtd', 'mtr', 'mtable'],
      // 允许 data:image/ 位图（Base64 图片预览）；data:text/html 与
      // data:image/svg+xml 等可执行脚本的 data URL 一律不放行。
      ALLOWED_URI_REGEXP,
    });
  } catch (e) {
    console.error('Markdown 解析失败:', e);
    return source;
  }
}
