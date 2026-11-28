// 由 tools/generate-iconify-subset.mjs 生成，请勿手改。
//
// 只包含源码里实际引用到的图标。用户自定义图标名若不在其中，
// 由调用方回退（见 pages/bookmarks/EngineIconPreview.jsx），
// 不要让 @iconify/react 去请求远端 API —— 内网/离线时那是静默失败。
import collection0 from './logos.subset.json';

export const ICONIFY_COLLECTIONS = [collection0];

const AVAILABLE_ICON_NAMES = new Set([
  ...Object.keys(collection0.icons || {}).map(name => 'logos:' + name),
  ...Object.keys(collection0.aliases || {}).map(name => 'logos:' + name),
]);

/** 该 "集合:图标名" 是否已随构建产物打包。 */
export function hasIcon(name) {
  return AVAILABLE_ICON_NAMES.has(String(name ?? '').trim());
}
