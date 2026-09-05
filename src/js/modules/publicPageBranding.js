import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { Activity, Bookmark, Server } from '../components/Icons.jsx';

export const PUBLIC_PAGE_ICON_CONFIG_KEY = 'publicIconId';

const DEFAULT_ICON_COLOR = '#f48120';
const GITHUB_ICON_VIEWBOX = '0 0 1230 1200';
const GITHUB_ICON_PATH = 'M615 0Q490 0 376 48Q265 95 180 180Q95 265 48 376Q0 490 0 615Q0 749 55 869Q108 986 203.5 1072Q299 1158 421 1199Q442 1203 453 1192Q463 1184 463 1169L462 1065Q414 1075 375 1070Q341 1066 315 1052Q294 1040 278 1022Q267 1009 260 994L255 982Q245 958 232 938Q222 923 210 911Q201 902 193 896L186 892Q167 879 163.5 871Q160 863 168 859Q173 856 182 855H191Q229 858 261 888Q277 903 285 918Q321 980 382 985Q421 989 464 969Q468 941 479 919Q489 900 503 887Q423 878 370 854Q301 823 265 763Q223 694 223 583Q223 487 286 418Q276 395 274 363Q270 310 291 255L301 254Q315 253 332 257Q356 262 385 275Q420 291 461 318Q534 298 614.5 298Q695 298 768 318Q789 304 808 293Q827 282 843 275Q859 268 872 263.5Q885 259 895.5 257Q906 255 913.5 254.5Q921 254 927 254L934 255Q937 255 937 255Q958 310 954 363Q952 394 943 418Q1006 487 1006 583Q1006 694 964 763Q927 823 859 854Q805 878 725 887Q743 902 754 929Q767 960 767 1000L766 1169Q766 1183 775 1192Q787 1202 808 1198Q931 1158 1027 1072Q1123 986 1176 869Q1230 749 1230 615Q1230 490 1182 376Q1135 265 1050 180Q965 95 855 48Q740 0 615 0Z';

const svgToDataUrl = (markup) => `data:image/svg+xml,${encodeURIComponent(markup)}`;

const GitHubGlyphIcon = ({ className = '', ...props }) => (
  React.createElement(
    'svg',
    {
      ...props,
      viewBox: GITHUB_ICON_VIEWBOX,
      fill: 'currentColor',
      className,
      'aria-hidden': props['aria-label'] ? undefined : true,
      focusable: 'false',
    },
    React.createElement('path', { d: GITHUB_ICON_PATH }),
  )
);

const GITHUB_FAVICON_HREF = svgToDataUrl(
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${GITHUB_ICON_VIEWBOX}" fill="${DEFAULT_ICON_COLOR}"><path d="${GITHUB_ICON_PATH}"/></svg>`,
);

const DEFAULT_ICON_DEFS = {
  uptime: {
    renderIcon: (props = {}) => React.createElement(Activity, props),
    faviconHref: svgToDataUrl(renderToStaticMarkup(React.createElement(Activity, { size: 64, color: DEFAULT_ICON_COLOR }))),
  },
  server: {
    renderIcon: (props = {}) => React.createElement(Server, props),
    faviconHref: svgToDataUrl(renderToStaticMarkup(React.createElement(Server, { size: 64, color: DEFAULT_ICON_COLOR }))),
  },
  github: {
    renderIcon: (props = {}) => React.createElement(GitHubGlyphIcon, props),
    faviconHref: GITHUB_FAVICON_HREF,
  },
  bookmarks: {
    renderIcon: (props = {}) => React.createElement(Bookmark, props),
    faviconHref: svgToDataUrl(renderToStaticMarkup(React.createElement(Bookmark, { size: 64, color: DEFAULT_ICON_COLOR }))),
  },
};

const ensureLink = (selector, rel) => {
  let node = document.head.querySelector(selector);
  if (!node) {
    node = document.createElement('link');
    node.rel = rel;
    document.head.appendChild(node);
  }
  return node;
};

const normalizeIconId = (value) => String(value || '').trim();

export const getPublicPageUploadedIconUrl = (iconId = '') => {
  const normalized = normalizeIconId(iconId);
  return normalized ? `/site-brand-icons/${encodeURIComponent(normalized)}` : '';
};

export const getPublicPageIconId = (config = {}) => normalizeIconId(config?.[PUBLIC_PAGE_ICON_CONFIG_KEY]);

export const withPublicPageIconId = (config = {}, iconId = '') => {
  const nextConfig = config && typeof config === 'object' ? { ...config } : {};
  const normalized = normalizeIconId(iconId);
  if (normalized) nextConfig[PUBLIC_PAGE_ICON_CONFIG_KEY] = normalized;
  else delete nextConfig[PUBLIC_PAGE_ICON_CONFIG_KEY];
  return nextConfig;
};

export const renderPublicPageDefaultIcon = (pageKind, props = {}) => {
  const definition = DEFAULT_ICON_DEFS[pageKind] || DEFAULT_ICON_DEFS.uptime;
  return definition.renderIcon(props);
};

export const getPublicPageFaviconHref = (pageKind, config = {}) => {
  const iconHref = getPublicPageUploadedIconUrl(getPublicPageIconId(config));
  if (iconHref) return iconHref;
  return (DEFAULT_ICON_DEFS[pageKind] || DEFAULT_ICON_DEFS.uptime).faviconHref;
};

export const swapPublicPageFavicon = (href) => {
  if (typeof document === 'undefined' || !href) return () => {};
  const iconLink = ensureLink('link[rel="icon"]', 'icon');
  const shortcutLink = ensureLink('link[rel="shortcut icon"]', 'shortcut icon');
  const previousIconHref = iconLink.getAttribute('href') || '';
  const previousShortcutHref = shortcutLink.getAttribute('href') || '';
  iconLink.href = href;
  shortcutLink.href = href;
  return () => {
    if (previousIconHref) iconLink.href = previousIconHref;
    else iconLink.removeAttribute('href');
    if (previousShortcutHref) shortcutLink.href = previousShortcutHref;
    else shortcutLink.removeAttribute('href');
  };
};

export const listPublicPageIcons = async () => {
  const response = await fetch('/api/settings/site-brand/icons', { cache: 'no-store' });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.error || '加载图标失败');
  }
  const items = Array.isArray(result.data) ? result.data : [];
  return items.map((item) => ({
    ...item,
    publicUrl: getPublicPageUploadedIconUrl(item.id),
  }));
};

export const uploadPublicPageIcon = async (file) => {
  const formData = new FormData();
  formData.append('file', file);
  formData.append('name', file.name);
  const response = await fetch('/api/settings/site-brand/icons', {
    method: 'POST',
    body: formData,
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.error || '上传图标失败');
  }
  const item = result.data || result;
  return {
    ...item,
    publicUrl: getPublicPageUploadedIconUrl(item.id),
  };
};

export const deletePublicPageIcon = async (iconId) => {
  const normalized = normalizeIconId(iconId);
  if (!normalized) return;
  const response = await fetch(`/api/settings/site-brand/icons/${encodeURIComponent(normalized)}`, {
    method: 'DELETE',
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.error || '删除图标失败');
  }
};
