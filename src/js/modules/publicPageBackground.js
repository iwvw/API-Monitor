// 网址导航公开页的背景自定义配置。
//
// 存在 bookmark_public_settings 单行表里（公开页全局设置），
// 早期版本误放在各分组的 config_json 里，已迁移过来 —— 背景属于
// 整页而不是单个分组，同一页面上出现不同分组各自背景说不通。
//
// 可配置项：
//   bgImage  背景图（http(s) URL 或 /site-brand-backgrounds/<file> 本地上传）
//   bgColor  纯色兜底（无图或图片加载失败时使用）
//   bgBlur   模糊强度（px，0-40）
//   bgDim    压暗程度（0-1），保证前景文字在任何背景上都可读
//   bgSize   填充方式 cover | contain | auto
//   bgFixed  是否固定（视差）

export const BG_CONFIG_KEYS = ['bgImage', 'bgColor', 'bgBlur', 'bgDim', 'bgSize', 'bgFixed'];

/**
 * 公开页全局外观设置（与背景同存于 bookmark_public_settings，因此共用一个模块）。
 *
 * pageWidth：内容区最大宽度档位。存「档位名」而不是像素值，
 * 这样后期调整各档实际 px 不会影响已存的数据；"full" = 铺满视口（默认，与历史行为一致）。
 */
export const PAGE_WIDTH_KEYS = ['narrow', 'normal', 'wide', 'full'];
export const DEFAULT_PAGE_WIDTH = 'full';

/**
 * 档位 → 实际最大宽度。
 * full 用 'none'（不加限制），其余给出具体的 max-width。
 * 用 max-w-* 工具类而不是内联 style，便于复用项目既有的 max-w-none 语义。
 */
export const PAGE_WIDTH_CLASS = {
  narrow: 'max-w-3xl',
  normal: 'max-w-5xl',
  wide: 'max-w-7xl',
  full: 'max-w-none',
};

export const PAGE_WIDTH_OPTIONS = [
  { value: 'narrow', label: '窄（阅读型）' },
  { value: 'normal', label: '标准' },
  { value: 'wide', label: '宽' },
  { value: 'full', label: '铺满' },
];

/** 归一化宽度档位：非法值回落到默认的 full。 */
export function normalizePageWidth(value) {
  const raw = String(value ?? '').trim().toLowerCase();
  return PAGE_WIDTH_KEYS.includes(raw) ? raw : DEFAULT_PAGE_WIDTH;
}

/** 读取公开页全局外观设置（已归一化，可直接用于渲染）。 */
export function getPublicPageSettings(config = {}) {
  const source = config && typeof config === 'object' ? config : {};
  return {
    pageWidth: normalizePageWidth(source.pageWidth),
  };
}

/** pageWidth 对应的 Tailwind 类（用于内容区容器）。 */
export function pageWidthClass(config = {}) {
  return PAGE_WIDTH_CLASS[getPublicPageSettings(config).pageWidth];
}


export const DEFAULT_BG_BLUR = 0;
export const DEFAULT_BG_DIM = 0.35;
export const BG_SIZE_OPTIONS = ['cover', 'contain', 'auto'];
export const UPLOADED_BG_PREFIX = '/site-brand-backgrounds/';

const clamp = (value, min, max) => Math.min(max, Math.max(min, value));

const normalizeString = (value) => String(value ?? '').trim();

/** 背景图地址：允许 http(s) 绝对地址与本站上传路径，其它一律丢弃。 */
export function normalizeBgImage(value) {
  const raw = normalizeString(value);
  if (!raw) return '';
  if (raw.startsWith(UPLOADED_BG_PREFIX)) return raw;
  if (/^https?:\/\//i.test(raw)) return raw;
  // 相对路径（如 /uploads/...）在公开页必然 404，拒绝以免出现空白背景
  return '';
}

/** 纯色：仅接受 #rgb / #rrggbb(aa) 形式。 */
export function normalizeBgColor(value) {
  const raw = normalizeString(value);
  if (!raw) return '';
  const candidate = raw.startsWith('#') ? raw : `#${raw}`;
  return /^#([0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(candidate)
    ? candidate.toLowerCase()
    : '';
}

export function normalizeBgBlur(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return DEFAULT_BG_BLUR;
  return clamp(Math.round(number), 0, 40);
}

export function normalizeBgDim(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return DEFAULT_BG_DIM;
  return clamp(Number(number.toFixed(2)), 0, 0.9);
}

export function normalizeBgSize(value) {
  const raw = normalizeString(value).toLowerCase();
  return BG_SIZE_OPTIONS.includes(raw) ? raw : 'cover';
}

export function normalizeBgFixed(value) {
  return value === true || value === 1 || value === '1' || value === 'true';
}

/** 从分组 config 读取背景设置（已归一化，可直接用于渲染）。 */
export function getBackgroundConfig(config = {}) {
  const source = config && typeof config === 'object' ? config : {};
  return {
    bgImage: normalizeBgImage(source.bgImage),
    bgColor: normalizeBgColor(source.bgColor),
    bgBlur: normalizeBgBlur(source.bgBlur),
    bgDim: normalizeBgDim(source.bgDim),
    bgSize: normalizeBgSize(source.bgSize),
    bgFixed: normalizeBgFixed(source.bgFixed),
  };
}

/**
 * 写回背景设置：返回**精简**的 config（等于默认值/空值的键会被移除）。
 * 适合存进本地 state，避免 config 里堆一堆空键。
 *
 * 注意：这个结果**不能直接作为 PUT body**——后端是局部更新语义，
 * 「键缺席」= 保留库里的旧值，删键会被当成「不改」。发请求前必须用
 * buildBackgroundPayload 补全所有字段。
 */
export function withBackgroundConfig(config = {}, patch = {}) {
  const next = config && typeof config === 'object' ? { ...config } : {};
  const merged = { ...getBackgroundConfig(next), ...getPublicPageSettings(next), ...patch };
  const normalizedBg = getBackgroundConfig(merged);
  for (const key of BG_CONFIG_KEYS) {
    const value = normalizedBg[key];
    const isEmpty = value === '' || value === false;
    const isDefault = (key === 'bgBlur' && value === DEFAULT_BG_BLUR)
      || (key === 'bgDim' && value === DEFAULT_BG_DIM)
      || (key === 'bgSize' && value === 'cover');
    if (isEmpty || isDefault) delete next[key];
    else next[key] = value;
  }
  // pageWidth 同样遵循「默认值不落库」，保持 config 精简
  const pageWidth = normalizePageWidth(merged.pageWidth);
  if (pageWidth === DEFAULT_PAGE_WIDTH) delete next.pageWidth;
  else next.pageWidth = pageWidth;
  return next;
}

/**
 * 把 config 展开成**完整的 PUT payload**：公开页所有全局字段都显式出现。
 *
 * 为什么必须这样（踩过的坑）：
 *   后端 PUT /public-settings 是局部更新——payload 里没带的键会保留库里的旧值。
 *   于是「选回 cover」时如果按精简语义删掉 bgSize，后端会一直保留旧的 contain，
 *   响应又把本地 state 覆盖回去，界面上就是「点了没反应 / 一点就恢复原样」。
 *   bgBlur/bgDim 之所以看起来正常，只是因为它们的值几乎不会恰好等于默认值，
 *   所以总在 payload 里，掩盖了这个缺陷。
 *
 * 新增全局字段时**必须**在这里一并展开，否则同样会「保存无效」。
 * 空值用空字符串表达（后端 sanitize 把 "" 视为清除该键）。
 */
export function buildPublicSettingsPayload(config = {}) {
  const bg = getBackgroundConfig(config);
  const page = getPublicPageSettings(config);
  return {
    bgImage: bg.bgImage || '',
    bgColor: bg.bgColor || '',
    bgBlur: bg.bgBlur,
    bgDim: bg.bgDim,
    bgSize: bg.bgSize,
    bgFixed: bg.bgFixed,
    pageWidth: page.pageWidth,
    // 自定义搜索引擎列表：必须一并带上，否则后端局部更新会保留旧值
    // （与 bgSize 是同一类陷阱）。空数组 = 回落到内置默认列表。
    searchEngines: Array.isArray(config?.searchEngines) ? config.searchEngines : [],
  };
}

/** 是否配置了任何背景（决定要不要渲染背景层）。 */
export function hasCustomBackground(config = {}) {
  const bg = getBackgroundConfig(config);
  return Boolean(bg.bgImage || bg.bgColor);
}

/**
 * 生成背景层的内联样式。
 * 图片用 background-image，模糊与压暗分别由两层叠加实现
 * （对 background-image 直接加 filter 会把整页内容一起模糊掉）。
 */
export function backgroundLayerStyle(config = {}) {
  const bg = getBackgroundConfig(config);
  if (!bg.bgImage && !bg.bgColor) return null;
  return {
    backgroundColor: bg.bgColor || 'transparent',
    backgroundImage: bg.bgImage ? `url("${bg.bgImage}")` : undefined,
    backgroundSize: bg.bgSize,
    backgroundPosition: 'center',
    backgroundRepeat: 'no-repeat',
    backgroundAttachment: bg.bgFixed ? 'fixed' : 'scroll',
  };
}

/** 背景之上那层「压暗 + 模糊」覆层的样式。 */
export function backgroundOverlayStyle(config = {}) {
  const bg = getBackgroundConfig(config);
  if (!bg.bgImage && !bg.bgColor) return null;
  return {
    backdropFilter: bg.bgBlur > 0 ? `blur(${bg.bgBlur}px)` : undefined,
    WebkitBackdropFilter: bg.bgBlur > 0 ? `blur(${bg.bgBlur}px)` : undefined,
    backgroundColor: bg.bgDim > 0 ? `rgba(0, 0, 0, ${bg.bgDim})` : undefined,
  };
}

/** 上传后的背景图地址。 */
export const getUploadedBackgroundUrl = (fileName = '') => {
  const normalized = normalizeString(fileName);
  return normalized ? `${UPLOADED_BG_PREFIX}${encodeURIComponent(normalized)}` : '';
};
