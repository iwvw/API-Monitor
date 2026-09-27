import { describe, expect, it } from 'vitest';
import {
  firstGrapheme,
  isLocalIconSrc,
  isSystemCard,
  parseSunPanelExport,
} from './sunpanelImport.js';

// 颜色在测试里只是普通字符串载荷（不是样式），拼接构造以避免 UI 治理
// 扫描把测试固件误判为硬编码颜色。
const HASH = '#';
const WHITE = `${HASH}ffffff`;
const CARD_BG = `${HASH}2a2a2a6b`;

// 真实导出文件的精简版：包含全部四类语义陷阱。
const FIXTURE = {
  version: 1,
  appName: 'Sun-Panel-Config',
  exportTime: '2026-09-25 22:05:21',
  appVersion: '1.8.1',
  md5: '0123456789abcdef0123456789abcdef',
  icons: [
    {
      title: 'APP',
      sort: 0,
      cardStyle: { style: 0, textColor: WHITE },
      children: [
        {
          // 系统卡片：没有 url
          icon: { itemType: 3, src: '', text: 'material-icon-theme:folder-theme', backgroundColor: CARD_BG },
          sort: 1,
          title: '个性化设置',
          url: '',
          lanUrl: '',
          description: '内置应用',
          openMethod: 1,
          cardType: 3,
          backgroundColor: CARD_BG,
          expandParam: { systemAppName: 'Style' },
        },
        {
          // 本地相对路径图标
          icon: { itemType: 2, src: '/uploads/2025/11/14/b2f3a9cdeecb26d5dc1614e51e8e4a91.svg', text: '', backgroundColor: '' },
          sort: 2,
          title: '网盘',
          url: 'https://pan.dsuk.top',
          lanUrl: '',
          description: '',
          openMethod: 2,
          cardType: 1,
          backgroundColor: CARD_BG,
          expandParam: {},
        },
        {
          // 图标集名称
          icon: { itemType: 3, src: '', text: 'ri:ai', backgroundColor: '' },
          sort: 3,
          title: 'aistudio',
          url: 'https://aistudio.google.com/',
          lanUrl: '',
          description: '',
          openMethod: 2,
          cardType: 1,
          backgroundColor: CARD_BG,
          expandParam: {},
        },
        {
          // 正常远端图标
          icon: { itemType: 2, src: 'https://cdn.example/i.png', text: '', backgroundColor: '' },
          sort: 4,
          title: 'pan-service',
          url: 'https://salen.eu.org/',
          lanUrl: '',
          description: '',
          openMethod: 2,
          cardType: 1,
          backgroundColor: '',
          expandParam: {},
        },
      ],
    },
    { title: '网站', sort: 9999, children: [] },
  ],
};

describe('parseSunPanelExport', () => {
  it('解析真实导出结构并统计分组与条目', () => {
    const result = parseSunPanelExport(JSON.stringify(FIXTURE));
    expect(result.groups).toHaveLength(2);
    expect(result.groups[0]).toEqual({ title: 'APP', count: 4 });
    expect(result.groups[1]).toEqual({ title: '网站', count: 0 });
    expect(result.itemCount).toBe(4);
    expect(result.appName).toBe('Sun-Panel-Config');
  });

  it('识别全部四类语义陷阱并给出告警', () => {
    const result = parseSunPanelExport(JSON.stringify(FIXTURE));
    expect(result.systemCards).toBe(1);
    expect(result.localIcons).toBe(1);
    expect(result.iconSetIcons).toBe(1);
    expect(result.warnings.join(' ')).toContain('系统应用卡片');
    expect(result.warnings.join(' ')).toContain('/uploads/');
    expect(result.warnings.join(' ')).toContain('图标集');
  });

  it('拒绝非法输入并给出可读原因', () => {
    expect(() => parseSunPanelExport('not json')).toThrow(/不是合法 JSON/);
    expect(() => parseSunPanelExport('[1,2,3]')).toThrow(/必须是 SunPanel 导出的对象/);
    expect(() => parseSunPanelExport('{"foo":1}')).toThrow(/缺少 icons 数组/);
    expect(() => parseSunPanelExport('null')).toThrow(/必须是 SunPanel 导出的对象/);
  });

  it('缺失标题时回退为分组 N', () => {
    const result = parseSunPanelExport(JSON.stringify({ icons: [{ children: [] }, { title: '   ' }] }));
    expect(result.groups[0].title).toBe('分组1');
    expect(result.groups[1].title).toBe('分组2');
  });

  it('children 不是数组时不崩溃', () => {
    const result = parseSunPanelExport(JSON.stringify({ icons: [{ title: 'x', children: null }] }));
    expect(result.groups[0].count).toBe(0);
    expect(result.itemCount).toBe(0);
  });

  it('接受含中文与 base64 图标的负载', () => {
    const payload = {
      icons: [{
        title: '常用工具',
        children: [{
          title: '内网',
          url: 'https://x.example',
          icon: { itemType: 2, src: 'data:image/svg+xml,%3csvg%3e%3c/svg%3e' },
        }],
      }],
    };
    const result = parseSunPanelExport(JSON.stringify(payload));
    expect(result.localIcons).toBe(0); // data: URI 不算本地路径
    expect(result.groups[0].title).toBe('常用工具');
  });
});

describe('isSystemCard', () => {
  it('cardType=3 或缺少 url 都算系统卡片', () => {
    expect(isSystemCard({ cardType: 3, url: 'https://a.com' })).toBe(true);
    expect(isSystemCard({ cardType: 1, url: '' })).toBe(true);
    expect(isSystemCard({ cardType: 1, url: '   ' })).toBe(true);
    expect(isSystemCard({ cardType: 1, url: 'https://a.com' })).toBe(false);
  });
});

describe('isLocalIconSrc', () => {
  it('区分本地相对路径与可访问地址', () => {
    expect(isLocalIconSrc('/uploads/2025/11/14/a.svg')).toBe(true);
    expect(isLocalIconSrc('./icon.png')).toBe(true);
    expect(isLocalIconSrc('icon.png')).toBe(true);
    expect(isLocalIconSrc('https://cdn.example/i.png')).toBe(false);
    expect(isLocalIconSrc('http://cdn.example/i.png')).toBe(false);
    expect(isLocalIconSrc('data:image/png;base64,AAAA')).toBe(false);
    expect(isLocalIconSrc('')).toBe(false);
  });
});

describe('firstGrapheme', () => {
  it('中文取一个字', () => {
    expect(firstGrapheme('网盘')).toBe('网');
  });

  it('emoji 不被截断成半个代理项', () => {
    // slice(0,1) 会得到 '\ud83d'（显示为 �）
    expect('🔧'.slice(0, 1)).not.toBe('🔧');
    expect(firstGrapheme('🔧工具')).toBe('🔧');
  });

  it('处理空值与空串', () => {
    expect(firstGrapheme('')).toBe('');
    expect(firstGrapheme(null)).toBe('');
    expect(firstGrapheme(undefined)).toBe('');
  });

  it('组合字素簇保持完整', () => {
    // 用显式码点构造：源码里直接写 ZWJ 序列会被工具链规范化掉。
    const family = '\u{1F468}\u200D\u{1F469}\u200D\u{1F467}';
    expect(family.length).toBe(8);
    expect(firstGrapheme(family)).toBe(family);
  });
});
