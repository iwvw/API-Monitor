import { FlyIoBrand, KoyebBrand, Settings } from '../../components/Icons.jsx';

export const tabs = [
  { value: 'fly', label: <span className="inline-flex items-center gap-1.5"><FlyIoBrand className="size-3.5 text-brand" />Fly.io</span> },
  { value: 'koyeb', label: <span className="inline-flex items-center gap-1.5"><KoyebBrand className="size-3.5 text-kumo-info" />Koyeb</span> },
  { value: 'config', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4 text-kumo-success" />配置</span> },
];
