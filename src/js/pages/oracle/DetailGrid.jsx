import React from 'react';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { formatInstanceMetric, getOciStatusTone } from './utils.js';

export default function DetailGrid({ instance }) {
  const rows = [
    ['实例名称', instance.name],
    ['状态', instance.state],
    ['规格', instance.shape],
    ['OCPU', formatInstanceMetric(instance.ocpuCount)],
    ['内存', instance.memoryGb ? `${formatInstanceMetric(instance.memoryGb)} GB` : '-'],
    ['可用域', instance.availabilityDomain],
    ['故障域', instance.faultDomain],
    ['公网 IP', instance.primaryPublicIp],
    ['私网 IP', instance.primaryPrivateIp],
    ['镜像 ID', instance.imageId],
    ['创建时间', instance.timeCreated],
  ];
  return (
    <div className="min-h-0 flex-1 overflow-auto scrollbar-thin">
      <div className="divide-y divide-kumo-line/80">
        {rows.map(([label, value]) => (
          <div key={label} className="grid grid-cols-[108px_minmax(0,1fr)] items-center gap-3 px-4 py-2.5">
            <div className="whitespace-nowrap text-sm text-kumo-subtle">{label}</div>
            <div className="min-w-0">
              {label === '状态' ? (
                <StatusBadge tone={getOciStatusTone(value)}>{value || '-'}</StatusBadge>
              ) : (
                <div className="truncate text-sm text-kumo-strong" title={value || '-'}>
                  {value || '-'}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
