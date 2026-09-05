// DashboardPage —— 仪表盘首页。概览统计卡已全部移除，直接嵌入卡片式图块看板（TilesBoard）。
// 数据获取、布局保存、拖拽缩放均由 TilesBoard 自包含处理；控制按钮在顶部面包屑栏。
import React from 'react';
import { PageStack } from '../components/ui/AppPrimitives.jsx';
import TilesBoard from '../components/tiles/TilesBoard.jsx';

function DashboardPage() {
  return (
    <PageStack className="gap-3 cq-sm:gap-4">
      <TilesBoard />
    </PageStack>
  );
}

export default DashboardPage;
