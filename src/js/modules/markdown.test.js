import { describe, expect, it } from 'vitest';
import { ALLOWED_URI_REGEXP } from './markdown.js';

// 回归测试：ALLOWED_URI_REGEXP 曾是非法正则（括号不匹配），
// 模块在解析阶段抛 "Invalid regular expression ... Unmatched ')'"，
// 导致整个 markdown 渲染模块无法加载。此处直接锁定字面量行为。
describe('ALLOWED_URI_REGEXP', () => {
  it('compiles and is a RegExp', () => {
    expect(ALLOWED_URI_REGEXP).toBeInstanceOf(RegExp);
  });

  it('允许安全协议与相对路径/锚点', () => {
    const allowed = [
      'http://example.com/a?b=1#c',
      'https://example.com',
      'ftp://mirror.example.com',
      'mailto:admin@example.com',
      'tel:+8613800138000',
      '/relative/path',
      './relative',
      '#anchor',
    ];
    for (const uri of allowed) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(true);
    }
  });

  it('允许 data:image/ 位图 data URL（Base64 图片预览）', () => {
    const allowed = [
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
      'data:image/jpeg,AAAA',
      'data:image/gif;base64,R0lGODlh',
    ];
    for (const uri of allowed) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(true);
    }
  });

  it('拦截 javascript:/vbscript:/file: 与不可信 data URL', () => {
    const blocked = [
      'javascript:alert(1)',
      'JavaScript:alert(1)',
      'vbscript:msgbox(1)',
      'file:///etc/passwd',
      'data:text/html,<script>alert(1)</script>',
      'data:image/svg+xml,<svg onload=alert(1)>',
      'data:application/javascript,alert(1)',
    ];
    for (const uri of blocked) {
      expect(ALLOWED_URI_REGEXP.test(uri), uri).toBe(false);
    }
  });
});