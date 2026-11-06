import { describe, expect, it } from 'vitest';
import { ICONIFY_COLLECTIONS, hasIcon } from '../../../assets/iconify/index.js';

// 图标子集是构建产物（tools/generate-iconify-subset.mjs 生成），
// 这里守的是「公开书签页用到的内置图标确实进了包」这一条：
// 一旦有人删了 logos:bing 之类的引用，生成脚本会把它从子集里剔掉，
// 公开页就会退化成「首字圆底」——那是可见的视觉回归，必须在 CI 拦住。
//
// 放在 src/js/pages/bookmarks/ 而不是 test/：本仓库 .gitignore 忽略了 test/，
// 只有早期两个文件被显式跟踪；新测试按约定与源码同目录（vitest 的
// include 已覆盖 src/js/pages/**/*.test.js）。

describe('iconify 图标子集', () => {
  it('至少包含一个集合，且每个集合带 prefix 与 icons', () => {
    expect(ICONIFY_COLLECTIONS.length).toBeGreaterThan(0);
    for (const collection of ICONIFY_COLLECTIONS) {
      expect(typeof collection.prefix).toBe('string');
      expect(collection.prefix.length).toBeGreaterThan(0);
      expect(Object.keys(collection.icons || {}).length).toBeGreaterThan(0);
    }
  });

  it('内置搜索引擎图标都已打包', () => {
    // 与 src/js/modules/publicSearch.js 的 DEFAULT_SEARCH_ENGINES 保持一致。
    for (const name of ['logos:bing', 'logos:google-icon', 'logos:duckduckgo']) {
      expect(hasIcon(name), `${name} 未包含在图标子集中`).toBe(true);
    }
  });

  it('hasIcon 对未打包的名字返回 false（UI 据此走文字兜底，不去请求远端）', () => {
    expect(hasIcon('logos:this-icon-does-not-exist')).toBe(false);
    expect(hasIcon('')).toBe(false);
    expect(hasIcon(undefined)).toBe(false);
    expect(hasIcon(null)).toBe(false);
  });

  it('体积受控：子集序列化后远小于上游整包', () => {
    // 上游 @iconify-json/logos 整包 7.9MB；子集应当只在几十 KB 量级。
    // 阈值放 200KB，既留余量又能在「有人整包 import 回来」时立刻报警。
    const size = JSON.stringify(ICONIFY_COLLECTIONS).length;
    expect(size).toBeLessThan(200 * 1024);
  });
});
