import { describe, expect, it } from 'vitest';
import {
  BG_SIZE_OPTIONS,
  DEFAULT_BG_BLUR,
  DEFAULT_BG_DIM,
  DEFAULT_PAGE_WIDTH,
  PAGE_WIDTH_CLASS,
  PAGE_WIDTH_KEYS,
  backgroundLayerStyle,
  backgroundOverlayStyle,
  buildPublicSettingsPayload,
  getBackgroundConfig,
  getUploadedBackgroundUrl,
  hasCustomBackground,
  normalizeBgBlur,
  normalizeBgColor,
  normalizeBgDim,
  normalizeBgFixed,
  normalizeBgImage,
  normalizeBgSize,
  normalizePageWidth,
  pageWidthClass,
  withBackgroundConfig,
} from './publicPageBackground.js';

describe('normalizeBgImage', () => {
  it('接受 http(s) 地址与本站上传路径', () => {
    expect(normalizeBgImage('https://cdn.example/bg.jpg')).toBe('https://cdn.example/bg.jpg');
    expect(normalizeBgImage('http://cdn.example/bg.jpg')).toBe('http://cdn.example/bg.jpg');
    expect(normalizeBgImage('/site-brand-backgrounds/a.jpg')).toBe('/site-brand-backgrounds/a.jpg');
  });

  it('拒绝会在公开页 404 的相对路径', () => {
    // 公开页不服务 /uploads/，保留它只会得到空白背景
    expect(normalizeBgImage('/uploads/bg.jpg')).toBe('');
    expect(normalizeBgImage('uploads/bg.jpg')).toBe('');
    expect(normalizeBgImage('./bg.jpg')).toBe('');
  });

  it('拒绝危险协议', () => {
    expect(normalizeBgImage('javascript:alert(1)')).toBe('');
    expect(normalizeBgImage('data:image/svg+xml,<svg/>')).toBe('');
    expect(normalizeBgImage('file:///etc/passwd')).toBe('');
  });

  it('空值安全', () => {
    expect(normalizeBgImage('')).toBe('');
    expect(normalizeBgImage(null)).toBe('');
    expect(normalizeBgImage(undefined)).toBe('');
    expect(normalizeBgImage('   ')).toBe('');
  });
});

describe('normalizeBgColor', () => {
  it('接受合法十六进制并补 #', () => {
    expect(normalizeBgColor('#fff')).toBe('#fff');
    expect(normalizeBgColor('#1a2b3c')).toBe('#1a2b3c');
    expect(normalizeBgColor('#1a2b3cff')).toBe('#1a2b3cff');
    expect(normalizeBgColor('1a2b3c')).toBe('#1a2b3c');
  });

  it('拒绝非法颜色', () => {
    expect(normalizeBgColor('rgba(1,2,3,.5)')).toBe('');
    expect(normalizeBgColor('red')).toBe('');
    expect(normalizeBgColor('#12')).toBe('');
    expect(normalizeBgColor('')).toBe('');
  });
});

describe('normalizeBgBlur / normalizeBgDim', () => {
  it('模糊值被限制在 0-40', () => {
    expect(normalizeBgBlur(10)).toBe(10);
    expect(normalizeBgBlur(-5)).toBe(0);
    expect(normalizeBgBlur(999)).toBe(40);
    expect(normalizeBgBlur('abc')).toBe(DEFAULT_BG_BLUR);
  });

  it('压暗值被限制在 0-0.9', () => {
    expect(normalizeBgDim(0.5)).toBe(0.5);
    expect(normalizeBgDim(-1)).toBe(0);
    expect(normalizeBgDim(5)).toBe(0.9);
    expect(normalizeBgDim('abc')).toBe(DEFAULT_BG_DIM);
  });
});

describe('normalizeBgSize / normalizeBgFixed', () => {
  it('只接受预设填充方式', () => {
    for (const size of BG_SIZE_OPTIONS) expect(normalizeBgSize(size)).toBe(size);
    expect(normalizeBgSize('bogus')).toBe('cover');
    expect(normalizeBgSize('')).toBe('cover');
  });

  it('布尔值归一化', () => {
    expect(normalizeBgFixed(true)).toBe(true);
    expect(normalizeBgFixed('true')).toBe(true);
    expect(normalizeBgFixed(1)).toBe(true);
    expect(normalizeBgFixed(false)).toBe(false);
    expect(normalizeBgFixed('false')).toBe(false);
  });
});

describe('getBackgroundConfig', () => {
  it('缺失字段回退默认值', () => {
    const bg = getBackgroundConfig({});
    expect(bg).toEqual({
      bgImage: '', bgColor: '', bgBlur: DEFAULT_BG_BLUR,
      bgDim: DEFAULT_BG_DIM, bgSize: 'cover', bgFixed: false,
    });
  });

  it('容忍非对象输入', () => {
    expect(() => getBackgroundConfig(null)).not.toThrow();
    expect(() => getBackgroundConfig('x')).not.toThrow();
    expect(getBackgroundConfig(null).bgSize).toBe('cover');
  });

  it('读取并归一化已有配置', () => {
    const bg = getBackgroundConfig({
      bgImage: 'https://cdn.example/a.jpg', bgColor: '1a2b3c',
      bgBlur: '12', bgDim: '0.6', bgSize: 'contain', bgFixed: 'true',
    });
    expect(bg.bgImage).toBe('https://cdn.example/a.jpg');
    expect(bg.bgColor).toBe('#1a2b3c');
    expect(bg.bgBlur).toBe(12);
    expect(bg.bgDim).toBe(0.6);
    expect(bg.bgSize).toBe('contain');
    expect(bg.bgFixed).toBe(true);
  });
});

describe('withBackgroundConfig', () => {
  it('写入后能读回', () => {
    const next = withBackgroundConfig({}, { bgImage: 'https://cdn.example/a.jpg', bgColor: '#123456' });
    const bg = getBackgroundConfig(next);
    expect(bg.bgImage).toBe('https://cdn.example/a.jpg');
    expect(bg.bgColor).toBe('#123456');
  });

  it('不破坏同一 config 里的其它键（如 publicIconId）', () => {
    const next = withBackgroundConfig({ publicIconId: 'icon-1' }, { bgColor: '#000000' });
    expect(next.publicIconId).toBe('icon-1');
    expect(next.bgColor).toBe('#000000');
  });

  it('默认值不落库，避免 config 里堆空键', () => {
    const next = withBackgroundConfig({}, {
      bgBlur: DEFAULT_BG_BLUR, bgDim: DEFAULT_BG_DIM, bgSize: 'cover', bgFixed: false, bgImage: '',
    });
    expect(next.bgBlur).toBeUndefined();
    expect(next.bgDim).toBeUndefined();
    expect(next.bgSize).toBeUndefined();
    expect(next.bgFixed).toBeUndefined();
    expect(next.bgImage).toBeUndefined();
  });

  it('清空背景后不残留键', () => {
    const withBg = withBackgroundConfig({}, { bgImage: 'https://cdn.example/a.jpg', bgColor: '#fff' });
    const cleared = withBackgroundConfig(withBg, { bgImage: '', bgColor: '' });
    expect(cleared.bgImage).toBeUndefined();
    expect(cleared.bgColor).toBeUndefined();
    expect(hasCustomBackground(cleared)).toBe(false);
  });

  it('拒绝非法值（不写进 config）', () => {
    const next = withBackgroundConfig({}, { bgImage: 'javascript:alert(1)', bgColor: 'not-a-color' });
    expect(next.bgImage).toBeUndefined();
    expect(next.bgColor).toBeUndefined();
  });
});

describe('buildPublicSettingsPayload', () => {
  it('所有全局字段都显式出现（含默认值），后端才能覆盖旧值', () => {
    // 曾经的 bug：选回 cover 时 withBackgroundConfig 会删掉 bgSize，
    // payload 缺键 → 后端局部更新保留旧的 contain → 界面被覆盖回 contain。
    const payload = buildPublicSettingsPayload({ bgBlur: 10, bgSize: 'cover', bgFixed: false });
    expect(Object.keys(payload).sort()).toEqual(
      ['bgBlur', 'bgColor', 'bgDim', 'bgFixed', 'bgImage', 'bgSize', 'pageWidth', 'searchEngines'].sort()
    );
    expect(payload.bgSize).toBe('cover');
    expect(payload.bgFixed).toBe(false);
    // 未配置引擎时给空数组 = 后端删除该键，公开页回落到内置默认
    expect(payload.searchEngines).toEqual([]);
  });

  it('精简 config 里被删掉的默认值，在 payload 中仍然存在', () => {
    const slim = withBackgroundConfig({}, { bgSize: 'contain' });
    const restored = withBackgroundConfig(slim, { bgSize: 'cover' });
    // 精简态：cover 被删
    expect(restored.bgSize).toBeUndefined();
    // payload 态：仍然是 cover，而不是 undefined
    expect(buildPublicSettingsPayload(restored).bgSize).toBe('cover');
  });

  it('pageWidth 选回默认 full 时也必须出现在 payload 里', () => {
    // 与 bgSize 同一类陷阱：选回默认值若被删键，后端会保留旧的 narrow/normal。
    const slim = withBackgroundConfig({}, { pageWidth: 'narrow' });
    expect(slim.pageWidth).toBe('narrow');
    const backToDefault = withBackgroundConfig(slim, { pageWidth: 'full' });
    expect(backToDefault.pageWidth).toBeUndefined(); // 精简态删掉
    expect(buildPublicSettingsPayload(backToDefault).pageWidth).toBe('full'); // payload 必须带上
  });

  it('空图片/颜色用空字符串表达清除语义', () => {
    const payload = buildPublicSettingsPayload({});
    expect(payload.bgImage).toBe('');
    expect(payload.bgColor).toBe('');
    expect(payload.pageWidth).toBe('full');
  });

  it('保留已设置的值', () => {
    const engines = [{ id: 'kagi', label: 'Kagi', url: 'https://kagi.com/search?q=%s' }];
    const payload = buildPublicSettingsPayload({
      bgImage: 'https://cdn.example/a.jpg', bgColor: '#123456', bgBlur: 12, bgDim: 0.6, bgSize: 'contain', bgFixed: true, pageWidth: 'normal',
      searchEngines: engines,
    });
    expect(payload).toEqual({
      bgImage: 'https://cdn.example/a.jpg', bgColor: '#123456', bgBlur: 12, bgDim: 0.6, bgSize: 'contain', bgFixed: true, pageWidth: 'normal',
      searchEngines: engines,
    });
  });

  it('searchEngines 必须原样出现在 payload（否则自定义引擎存不上）', () => {
    // 与 bgSize 同类陷阱：payload 缺键 → 后端局部更新保留旧值。
    const engines = [{ id: 'a', label: 'A', url: 'https://a.com/?q=%s' }];
    expect(buildPublicSettingsPayload({ searchEngines: engines }).searchEngines).toEqual(engines);
  });
});

describe('pageWidth', () => {
  it('归一化：非法值回落到 full', () => {
    expect(normalizePageWidth('narrow')).toBe('narrow');
    expect(normalizePageWidth('WIDE')).toBe('wide');
    expect(normalizePageWidth('')).toBe(DEFAULT_PAGE_WIDTH);
    expect(normalizePageWidth(undefined)).toBe(DEFAULT_PAGE_WIDTH);
    expect(normalizePageWidth('huge')).toBe(DEFAULT_PAGE_WIDTH);
  });

  it('pageWidthClass 返回对应的 max-w 类，并随内容变化', () => {
    expect(pageWidthClass({ pageWidth: 'narrow' })).toBe('max-w-3xl');
    expect(pageWidthClass({ pageWidth: 'normal' })).toBe('max-w-5xl');
    expect(pageWidthClass({ pageWidth: 'wide' })).toBe('max-w-7xl');
    expect(pageWidthClass({ pageWidth: 'full' })).toBe('max-w-none');
    expect(pageWidthClass({})).toBe('max-w-none');
    // 默认值必须与历史行为一致（铺满），否则升级后所有公开页会突然变窄
    expect(PAGE_WIDTH_CLASS[DEFAULT_PAGE_WIDTH]).toBe('max-w-none');
  });

  it('四个档位的宽度严格递增（narrow < normal < wide < full）', () => {
    const order = { 'max-w-3xl': 3, 'max-w-5xl': 5, 'max-w-7xl': 7, 'max-w-none': Infinity };
    const values = PAGE_WIDTH_KEYS.map(k => order[PAGE_WIDTH_CLASS[k]]);
    for (let i = 1; i < values.length; i += 1) {
      expect(values[i]).toBeGreaterThan(values[i - 1]);
    }
  });
});

describe('hasCustomBackground', () => {
  it('有图或有色即为 true', () => {
    expect(hasCustomBackground({ bgImage: 'https://cdn.example/a.jpg' })).toBe(true);
    expect(hasCustomBackground({ bgColor: '#000' })).toBe(true);
    expect(hasCustomBackground({})).toBe(false);
    expect(hasCustomBackground({ bgBlur: 10 })).toBe(false);
  });
});

describe('backgroundLayerStyle / backgroundOverlayStyle', () => {
  it('未配置背景时返回 null（不渲染背景层）', () => {
    expect(backgroundLayerStyle({})).toBeNull();
    expect(backgroundOverlayStyle({})).toBeNull();
  });

  it('图片背景生成正确的 CSS', () => {
    const style = backgroundLayerStyle({ bgImage: 'https://cdn.example/a.jpg', bgSize: 'contain', bgFixed: true });
    expect(style.backgroundImage).toBe('url("https://cdn.example/a.jpg")');
    expect(style.backgroundSize).toBe('contain');
    expect(style.backgroundAttachment).toBe('fixed');
    expect(style.backgroundRepeat).toBe('no-repeat');
  });

  it('纯色背景不加 backgroundImage', () => {
    const style = backgroundLayerStyle({ bgColor: '#123456' });
    expect(style.backgroundColor).toBe('#123456');
    expect(style.backgroundImage).toBeUndefined();
  });

  it('模糊与压暗走独立覆层（避免把前景一起模糊）', () => {
    const overlay = backgroundOverlayStyle({ bgImage: 'https://cdn.example/a.jpg', bgBlur: 8, bgDim: 0.4 });
    expect(overlay.backdropFilter).toBe('blur(8px)');
    expect(overlay.backgroundColor).toBe('rgba(0, 0, 0, 0.4)');
    // 背景层自身不应带 filter
    expect(backgroundLayerStyle({ bgImage: 'https://cdn.example/a.jpg' }).filter).toBeUndefined();
  });

  it('模糊为 0 时不写 backdropFilter', () => {
    const overlay = backgroundOverlayStyle({ bgColor: '#000', bgBlur: 0, bgDim: 0.2 });
    expect(overlay.backdropFilter).toBeUndefined();
  });
});

describe('getUploadedBackgroundUrl', () => {
  it('生成编码后的上传地址', () => {
    expect(getUploadedBackgroundUrl('a b.jpg')).toBe('/site-brand-backgrounds/a%20b.jpg');
    expect(getUploadedBackgroundUrl('bg.png')).toBe('/site-brand-backgrounds/bg.png');
    expect(getUploadedBackgroundUrl('')).toBe('');
  });
});
