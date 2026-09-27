// SunPanel（.sun-panel.json）导入的纯解析逻辑，与 UI 解耦以便单测。
//
// SunPanel 导出结构：
//   { version, appName, exportTime, appVersion, md5,
//     icons: [ { title, sort, cardStyle, children: [ {...item} ] } ] }
//
// 本站 bookmarks 模型与它存在四处语义错配，解析阶段就要暴露出来：
//   1. cardType=3 的系统卡片没有 url，而本站 url 必填。
//   2. icon.itemType=3 在 SunPanel 是「图标集名」（如 ri:ai），本站 icon_type=3 是 Emoji。
//   3. icon.src 常为 /uploads/... 相对路径，指向 SunPanel 站点，本站必然 404。
//   4. sort 值在同组内可重复且不连续。

export const SUNPANEL_GROUP_MODE_OPTIONS = [
  { value: 'new', label: '每个分组单独创建' },
  { value: 'merge', label: '并入同名分组' },
  { value: 'single', label: '全部并入指定分组' },
];

/** 判断是否为 SunPanel 内置系统应用卡片（无链接）。 */
export function isSystemCard(item) {
  return Number(item?.cardType) === 3 || !String(item?.url || '').trim();
}

/** 是否为指向 SunPanel 自身的本地/相对图标路径。 */
export function isLocalIconSrc(src) {
  const value = String(src || '').trim();
  if (!value) return false;
  if (value.startsWith('data:')) return false;
  if (value.startsWith('http://') || value.startsWith('https://')) return false;
  return true;
}

/**
 * 解析 SunPanel 导出文本，返回分组/条目统计与逐项告警。
 * 抛错表示文件不可用；warnings 表示可用但有需要注意的条目。
 */
export function parseSunPanelExport(text) {
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    throw new Error(`文件不是合法 JSON：${error.message}`, { cause: error });
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('文件内容必须是 SunPanel 导出的对象');
  }
  if (!Array.isArray(parsed.icons)) {
    throw new Error('缺少 icons 数组，这可能不是 SunPanel 导出的配置文件');
  }

  let itemCount = 0;
  let systemCards = 0;
  let localIcons = 0;
  let iconSetIcons = 0;
  const warnings = [];

  const groups = parsed.icons.map((group, index) => {
    const title = String(group?.title || '').trim() || `分组${index + 1}`;
    const children = Array.isArray(group?.children) ? group.children : [];
    for (const item of children) {
      itemCount += 1;
      if (isSystemCard(item)) {
        systemCards += 1;
        continue;
      }
      if (Number(item?.icon?.itemType) === 3) {
        iconSetIcons += 1;
      }
      if (isLocalIconSrc(item?.icon?.src)) {
        localIcons += 1;
      }
    }
    return { title, count: children.length };
  });

  if (systemCards > 0) {
    warnings.push(`${systemCards} 个系统应用卡片没有网址，默认跳过`);
  }
  if (localIcons > 0) {
    warnings.push(`${localIcons} 个图标是 SunPanel 本地路径（/uploads/…），本站无法加载，将改为标题首字`);
  }
  if (iconSetIcons > 0) {
    warnings.push(`${iconSetIcons} 个图标使用图标集名称（如 ri:ai），将降级为标题首字`);
  }

  return {
    groups,
    itemCount,
    systemCards,
    localIcons,
    iconSetIcons,
    warnings,
    appName: parsed.appName || '',
    exportTime: parsed.exportTime || '',
    appVersion: parsed.appVersion || '',
  };
}

/** 取首个字素簇：slice(0,1) 会把 emoji 截成孤立代理项。 */
export function firstGrapheme(value) {
  const text = String(value ?? '');
  if (!text) return '';
  try {
    if (typeof Intl !== 'undefined' && Intl.Segmenter) {
      const segments = new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(text);
      const first = segments[Symbol.iterator]().next();
      if (!first.done) return first.value.segment;
    }
  } catch {
    // 回退到码点级截取
  }
  return Array.from(text)[0] || '';
}
