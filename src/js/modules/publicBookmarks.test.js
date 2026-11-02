import { describe, expect, it } from 'vitest';
import {
  AGGREGATE_SLUG,
  DEFAULT_DENSITY,
  DEFAULT_SORT,
  DENSITY,
  DENSITY_KEYS,
  SORT_VALUES,
  applyFetchedIcon,
  cardClassForDensity,
  countItems,
  filterGroups,
  gridStyleForDensity,
  isAggregateSlug,
  parsePublicBookmarksPath,
  parseSortSpec,
  resolveDensity,
  shellClassForDensity,
} from './publicBookmarks.js';

describe('parsePublicBookmarksPath', () => {
  it('识别公开页前缀', () => {
    expect(parsePublicBookmarksPath('/bookmarks/all')).toBe('all');
    expect(parsePublicBookmarksPath('/b/all')).toBe('all');
    expect(parsePublicBookmarksPath('/bm/all')).toBe('all');
    expect(parsePublicBookmarksPath('/bookmarks/alpha')).toBe('alpha');
  });

  it('容忍尾斜杠', () => {
    expect(parsePublicBookmarksPath('/bookmarks/all/')).toBe('all');
    expect(parsePublicBookmarksPath('/bookmarks/all///')).toBe('all');
  });

  it('解码百分号编码的 slug（中文分组名）', () => {
    expect(parsePublicBookmarksPath('/bookmarks/u7f51-u7ad9')).toBe('u7f51-u7ad9');
    expect(parsePublicBookmarksPath(`/bookmarks/${encodeURIComponent('网站')}`)).toBe('网站');
  });

  it('非公开页路径返回 null', () => {
    expect(parsePublicBookmarksPath('/')).toBeNull();
    expect(parsePublicBookmarksPath('/bookmarks')).toBeNull();
    expect(parsePublicBookmarksPath('/bookmarks/all/extra')).toBeNull();
    expect(parsePublicBookmarksPath('/status/foo')).toBeNull();
    expect(parsePublicBookmarksPath('')).toBeNull();
  });

  it('非法百分号编码不抛错', () => {
    expect(() => parsePublicBookmarksPath('/bookmarks/%E0%A4%A')).not.toThrow();
  });
});

describe('isAggregateSlug', () => {
  it('识别聚合 slug（大小写不敏感）', () => {
    expect(isAggregateSlug('all')).toBe(true);
    expect(isAggregateSlug('ALL')).toBe(true);
    expect(isAggregateSlug('All')).toBe(true);
    expect(AGGREGATE_SLUG).toBe('all');
  });

  it('普通 slug 不算聚合', () => {
    expect(isAggregateSlug('alpha')).toBe(false);
    expect(isAggregateSlug('all-2')).toBe(false);
    expect(isAggregateSlug('')).toBe(false);
    expect(isAggregateSlug(null)).toBe(false);
  });
});

describe('parseSortSpec', () => {
  it('接受全部合法排序', () => {
    for (const value of SORT_VALUES) {
      expect(parseSortSpec(value)).toBe(value);
    }
  });

  it('非法值回退默认', () => {
    expect(parseSortSpec('bogus')).toBe(DEFAULT_SORT);
    expect(parseSortSpec('name')).toBe(DEFAULT_SORT);
    expect(parseSortSpec('')).toBe(DEFAULT_SORT);
    expect(parseSortSpec(null)).toBe(DEFAULT_SORT);
  });

  it('大小写与空白容错', () => {
    expect(parseSortSpec('  NAME-ASC ')).toBe('name-asc');
  });

  it('默认排序是 order-asc', () => {
    expect(DEFAULT_SORT).toBe('order-asc');
  });
});

describe('密度与网格宽度', () => {
  it('未知密度回退默认', () => {
    expect(resolveDensity('nope')).toBe(DEFAULT_DENSITY);
    expect(resolveDensity(undefined)).toBe(DEFAULT_DENSITY);
    expect(DEFAULT_DENSITY).toBe('cozy');
  });

  it('每种密度都有 min/max 列宽区间、图标与容器宽度', () => {
    for (const key of DENSITY_KEYS) {
      const shape = DENSITY[key];
      expect(shape.min).toMatch(/rem$/);
      expect(shape.max).toMatch(/rem$/);
      // max 必须大于 min，否则区间无意义
      expect(parseFloat(shape.max)).toBeGreaterThan(parseFloat(shape.min));
      expect(shape.icon).toMatch(/^h-\d+/);
      expect(shape.shell).toBe('max-w-none');
    }
  });

  it('网格用 auto-fill + 弹性轨道：填满容器、两侧不留空隙', () => {
    // 选「无空隙」而非「宽度绝对一致」：固定轨道除不尽时余数会全堆在右侧。
    for (const key of DENSITY_KEYS) {
      const tmpl = gridStyleForDensity(key).gridTemplateColumns;
      expect(tmpl).toContain('auto-fill');
      expect(tmpl).toBe(`repeat(auto-fill, minmax(${DENSITY[key].min}, 1fr))`);
      // 1fr 必须存在，否则轨道不拉伸、右侧留白
      expect(tmpl).toContain('1fr');
      // 不能出现固定列数（如 repeat(4, ...)）
      expect(tmpl).not.toMatch(/repeat\(\s*\d+\s*,/);
    }
  });

  it('轨道上限不能写成固定 rem —— 会把列数卡死、超宽屏浪费空间', () => {
    for (const key of DENSITY_KEYS) {
      const tmpl = gridStyleForDensity(key).gridTemplateColumns;
      // minmax(Xrem, Yrem) 会把轨道锁在 Yrem，宽屏放不下更多列
      expect(tmpl).not.toMatch(/minmax\([^)]*,\s*[\d.]+rem\s*\)/);
    }
  });

  it('gap 三档差距足够大，切换布局时肉眼可见（曾经的 bug：相邻档只差 2px）', () => {
    const rem = (value) => parseFloat(String(value).replace('rem', ''));
    const compact = rem(DENSITY.compact.gap);
    const cozy = rem(DENSITY.cozy.gap);
    const wide = rem(DENSITY.wide.gap);
    // 严格递增：紧凑 < 标准 < 宽松
    expect(compact).toBeLessThan(cozy);
    expect(cozy).toBeLessThan(wide);
    // 相邻两档至少差 0.25rem（4px），否则视觉上分不出来
    expect(cozy - compact).toBeGreaterThanOrEqual(0.25);
    expect(wide - cozy).toBeGreaterThanOrEqual(0.25);
  });

  it('页面不设宽度上限：超宽屏继续加列而不是留白', () => {
    for (const key of DENSITY_KEYS) {
      expect(shellClassForDensity(key)).toBe('max-w-none');
      expect(DENSITY[key].shell).toBe('max-w-none');
    }
  });

  it('卡片 max-w 由 DENSITY.max 生成，避免列被拉得过长', () => {
    for (const key of DENSITY_KEYS) {
      expect(cardClassForDensity(key)).toBe(`max-w-[${DENSITY[key].max}]`);
    }
    // 未知密度回落到默认档
    expect(cardClassForDensity('unknown')).toBe(`max-w-[${DENSITY[DEFAULT_DENSITY].max}]`);
  });

  it('紧凑密度的卡片更窄（同屏列数更多）', () => {
    const compact = parseFloat(DENSITY.compact.min);
    const cozy = parseFloat(DENSITY.cozy.min);
    const wide = parseFloat(DENSITY.wide.min);
    expect(compact).toBeLessThan(cozy);
    expect(cozy).toBeLessThan(wide);
  });

  it('图片图标按盒子满尺寸显示（回归：曾只有盒子的 64%，看起来偏小）', () => {
    for (const key of DENSITY_KEYS) {
      // 图片尺寸类必须与盒子一致，不再缩在盒子内部留一圈空白
      expect(DENSITY[key].iconInner).toBe(DENSITY[key].icon);
    }
  });

  it('图标盒子尺寸在合理区间（不过小也不过大）', () => {
    for (const key of DENSITY_KEYS) {
      const match = DENSITY[key].icon.match(/h-(\d+)/);
      expect(match, `${key} icon must be a tailwind size class`).not.toBeNull();
      const px = Number(match[1]) * 4; // tailwind 间距 1 = 4px
      // 下限：太小看不清；上限：太大会喧宾夺主（曾调到 56px 显得过大）
      expect(px, `${key} icon too small`).toBeGreaterThanOrEqual(32);
      expect(px, `${key} icon too large`).toBeLessThanOrEqual(48);
    }
  });
});

describe('applyFetchedIcon', () => {
  it('同时写入 icon_type 与 icon_src（回归：只改 src 会导致图标不显示）', () => {
    // 曾经的 bug：拿到地址后分别调两次 setField('icon_type')/setField('icon_src')，
    // 两次都基于同一个陈旧 form 展开 → 后者覆盖前者 → icon_type 仍是 1（文字），
    // 于是「已获取网站图标」提示出现，但卡片上仍显示文字、图片不显示。
    const form = { title: 'API监控', url: 'https://a.com', icon_type: 1, icon_text: 'A', icon_src: '' };
    const next = applyFetchedIcon(form, '/api/bookmarks/favicons/abc.png');
    expect(next.icon_type).toBe(2);
    expect(next.icon_src).toBe('/api/bookmarks/favicons/abc.png');
  });

  it('不改动其它字段', () => {
    const form = { id: 7, title: 'API监控', url: 'https://a.com', description: 'd', icon_type: 3, icon_text: '🔧', icon_bg_color: '#123456' };
    const next = applyFetchedIcon(form, '/x.png');
    expect(next.id).toBe(7);
    expect(next.title).toBe('API监控');
    expect(next.url).toBe('https://a.com');
    expect(next.description).toBe('d');
    expect(next.icon_bg_color).toBe('#123456');
    expect(next.icon_text).toBe('🔧'); // 保留原文字，切回文字类型时还在
  });

  it('空地址时原样返回，不把类型改成 2（避免显示成空占位）', () => {
    const form = { icon_type: 1, icon_text: 'A', icon_src: '' };
    const next = applyFetchedIcon(form, '');
    expect(next.icon_type).toBe(1);
    expect(next.icon_src).toBe('');
  });

  it('地址两端空白会被裁掉', () => {
    const next = applyFetchedIcon({ icon_type: 1 }, '  /a.png  ');
    expect(next.icon_src).toBe('/a.png');
  });

  it('form 为空时不抛错', () => {
    const next = applyFetchedIcon(null, '/a.png');
    expect(next.icon_type).toBe(2);
    expect(next.icon_src).toBe('/a.png');
  });
});

const GROUPS = [
  {
    id: 1,
    title: '开发工具',
    description: '日常开发',
    items: [
      { id: 11, title: 'GitHub', url: 'https://github.com', description: '代码托管' },
      { id: 12, title: 'GitLab', url: 'https://gitlab.com', description: '' },
    ],
  },
  {
    id: 2,
    title: '影音',
    description: '',
    items: [
      { id: 21, title: 'YouTube', url: 'https://youtube.com', description: '视频' },
    ],
  },
];

describe('filterGroups', () => {
  it('空关键词返回全部分组（原样）', () => {
    expect(filterGroups(GROUPS, '')).toBe(GROUPS);
    expect(filterGroups(GROUPS, '   ')).toBe(GROUPS);
  });

  it('按网址标题过滤，只保留命中的条目', () => {
    const result = filterGroups(GROUPS, 'git');
    expect(result).toHaveLength(1);
    expect(result[0].title).toBe('开发工具');
    expect(result[0].items.map(i => i.title)).toEqual(['GitHub', 'GitLab']);
  });

  it('按 URL 过滤', () => {
    const result = filterGroups(GROUPS, 'youtube');
    expect(result).toHaveLength(1);
    expect(result[0].items.map(i => i.title)).toEqual(['YouTube']);
  });

  it('按描述过滤', () => {
    const result = filterGroups(GROUPS, '视频');
    expect(result[0].items.map(i => i.title)).toEqual(['YouTube']);
  });

  it('分组名命中时保留整组条目', () => {
    const result = filterGroups(GROUPS, '影音');
    expect(result).toHaveLength(1);
    expect(result[0].title).toBe('影音');
    expect(result[0].items).toHaveLength(1);
  });

  it('无命中返回空数组', () => {
    expect(filterGroups(GROUPS, 'zzz-no-match')).toEqual([]);
  });

  it('大小写不敏感', () => {
    expect(filterGroups(GROUPS, 'GITHUB')).toHaveLength(1);
    expect(filterGroups(GROUPS, 'github')).toHaveLength(1);
  });

  it('容错：非数组输入与缺失字段不抛错', () => {
    expect(filterGroups(null, 'x')).toEqual([]);
    expect(filterGroups(undefined, 'x')).toEqual([]);
    expect(filterGroups([{ id: 1 }], 'x')).toEqual([]);
    expect(() => filterGroups([{ id: 1, items: [{ id: 2 }] }], 'x')).not.toThrow();
  });

  it('不修改原对象（分组名命中时原样返回同一引用）', () => {
    const before = JSON.stringify(GROUPS);
    filterGroups(GROUPS, 'git');
    filterGroups(GROUPS, '影音');
    expect(JSON.stringify(GROUPS)).toBe(before);
  });
});

describe('countItems', () => {
  it('统计所有分组的条目数', () => {
    expect(countItems(GROUPS)).toBe(3);
    expect(countItems([])).toBe(0);
    expect(countItems(null)).toBe(0);
    expect(countItems([{ items: null }])).toBe(0);
  });
});
