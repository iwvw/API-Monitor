// headerToolsContext —— 让页面内容组件（如 TilesBoard）把控制按钮 portal 到顶部面包屑栏
// 右侧的工具区（MainLayout 的 AppPageHeader children 位置）。无 Provider（如独立 demo 页）
// 时值为 null，组件回退为内联渲染自身控制栏。
import { createContext } from 'react';

export const HeaderToolsContext = createContext(null);
