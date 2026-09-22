import { Box, HardDrive, Layers, Plug } from '../../components/Icons.jsx';

export const assetTabs = [
  { value: 'overview', label: <span className="inline-flex items-center gap-1.5"><Layers className="w-3.5 h-3.5" />总览</span> },
  { value: 'physical', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="w-3.5 h-3.5" />实体资产</span> },
  { value: 'virtual', label: <span className="inline-flex items-center gap-1.5"><Box className="w-3.5 h-3.5" />虚拟资产</span> },
  { value: 'sources', label: <span className="inline-flex items-center gap-1.5"><Plug className="w-3.5 h-3.5" />纳管来源</span> },
];
