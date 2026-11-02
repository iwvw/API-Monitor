import { getIssuerColor } from '../../components/ui/BrandIcon.jsx';
import { HEX_COLOR_PATTERN, SVG_REPO_ICON_REF_PATTERN } from './constants.js';

export const maskEmail = email => {
  if (!email) return '';
  if (!email.includes('@')) return email;
  const [local, domain] = email.split('@');
  if (local.length <= 3) return local[0] + '***@' + domain;
  return local.slice(0, 2) + '***' + local.slice(-1) + '@' + domain;
};

export const isSVGRepoIcon = icon => typeof icon === 'string' && icon.startsWith('svgrepo:');
export const isCustomUploadedIcon = icon => typeof icon === 'string' && icon.startsWith('custom:');

export const normalizeHexColor = value => {
  const text = String(value || '').trim();
  if (!text) return '';
  const withHash = text.startsWith('#') ? text : `#${text}`;
  return HEX_COLOR_PATTERN.test(withHash) ? withHash.toLowerCase() : text;
};

export const resolveFormColor = form => {
  const color = normalizeHexColor(form.color);
  return HEX_COLOR_PATTERN.test(color) ? color : getIssuerColor(form.issuer);
};

export const normalizeSVGRepoIconRef = value => {
  const text = String(value || '').trim();
  if (!text) return '';
  const match = text.match(SVG_REPO_ICON_REF_PATTERN);
  if (!match) return text;
  return `svgrepo:${match[1]}-${match[2].replace(/^-+|-+$/g, '').toLowerCase()}`;
};

export const normalizeRemoteBrandIconURL = value => {
  const text = String(value || '').trim();
  if (!text) return '';
  try {
    const parsed = new URL(text);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : '';
  } catch (_) {
    return '';
  }
};

export const buildBrandStyleOptions = ({
  issuer,
  icon,
  color,
  name,
  options: detectedOptions = [],
} = {}) => {
  const displayName = name || issuer || '品牌';
  const baseColor = color || getIssuerColor(issuer);
  const firstLetter =
    String(displayName || issuer || '?')
      .trim()
      .slice(0, 1)
      .toUpperCase() || '?';
  const options = [];
  const repoOptions =
    Array.isArray(detectedOptions) && detectedOptions.length > 0
      ? detectedOptions
      : icon
        ? [{ icon, color, name }]
        : [];
  repoOptions.forEach((item, index) => {
    options.push({
      id: `svgrepo-${index}-${item.icon || icon}`,
      label: `SVG Repo ${repoOptions.length > 1 ? index + 1 : ''}`.trim(),
      caption: item.name || displayName,
      icon: item.icon || icon,
      color: item.color || baseColor,
    });
  });
  options.push(
    {
      id: 'badge',
      label: '品牌徽标',
      caption: '系统图标',
      icon: '',
      color: baseColor,
    },
    {
      id: 'letter',
      label: '首字母',
      caption: firstLetter,
      icon: `letter:${firstLetter}`,
      color: baseColor,
    }
  );
  return options;
};

export const buildCustomBrandStyleOptions = ({ issuer, entries = [], fallbackColor = '' } = {}) => {
  const issuerKey = String(issuer || '')
    .trim()
    .toLowerCase();
  const sorted = [...entries].sort((a, b) => {
    const aMatched =
      issuerKey &&
      String(a.issuer || '')
        .trim()
        .toLowerCase() === issuerKey;
    const bMatched =
      issuerKey &&
      String(b.issuer || '')
        .trim()
        .toLowerCase() === issuerKey;
    if (aMatched !== bMatched) return aMatched ? -1 : 1;
    return String(a.name || '').localeCompare(String(b.name || ''), 'zh-CN');
  });
  return sorted.map(entry => ({
    id: `custom-${entry.id}`,
    customId: entry.id,
    label: entry.name || entry.issuer || '自定义图标',
    caption: entry.issuer ? `图标库 / ${entry.issuer}` : '图标库',
    icon: entry.icon || (entry.id ? `custom:${entry.id}` : ''),
    color: entry.color || fallbackColor,
    source: 'custom',
  }));
};

export const mergeBrandStyleOptions = (...groups) => {
  const seen = new Set();
  const merged = [];
  groups.flat().forEach(option => {
    if (!option) return;
    const key = option.icon || option.id;
    if (seen.has(key)) return;
    seen.add(key);
    merged.push(option);
  });
  return merged;
};
