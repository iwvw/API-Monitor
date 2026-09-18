import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { formatMemoryGb } from './utils.jsx';

export default function CreateInstanceDialog({
  open,
  onOpenChange,
  createForm,
  setCreateForm,
  zones,
  machineTypes,
  images,
  subnetworks,
  loadingCreateOptions,
  loadingMachineTypes,
  submittingCreate,
  onZoneChange,
  onSubmit,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>创建实例</LayerDialog.Title>
        <LayerDialog.Description>GCP 实例创建为异步操作，发票将按所选区与机型计费。</LayerDialog.Description>
        <LayerDialog.Body>
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">名称 *</span>
          <Input size="sm" value={createForm.name} onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })} placeholder="实例名称" />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">可用区 *</span>
          <Select alignItemWithTrigger size="sm" value={createForm.zone} onValueChange={onZoneChange} items={[
              { value: '', label: '选择可用区' },
              ...zones.map((zone) => ({ value: zone.name, label: zone.name })),
            ]} />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">机型 *</span>
          <Select alignItemWithTrigger size="sm" disabled={!createForm.zone || loadingMachineTypes} value={createForm.machineType} onValueChange={(value) => setCreateForm({ ...createForm, machineType: value })} items={[
            { value: '', label: loadingMachineTypes ? '加载中…' : (createForm.zone ? '选择机型' : '请先选择可用区') },
            ...machineTypes.map((mt) => ({ value: mt.name, label: `${mt.name}（${mt.guestCpus} vCPU / ${formatMemoryGb(mt.memoryMb)}）` })),
          ]} />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">镜像</span>
          <Select alignItemWithTrigger size="sm" value={createForm.image} onValueChange={(value) => setCreateForm({ ...createForm, image: value })} items={[
            { value: '', label: '使用默认镜像' },
            ...images.map((image) => ({ value: image.name, label: image.name })),
          ]} />
        </label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">启动盘大小（GB）</span>
            <Input size="sm" type="number" value={createForm.bootDiskSizeGb} onChange={(event) => setCreateForm({ ...createForm, bootDiskSizeGb: event.target.value })} />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">子网</span>
          <Select alignItemWithTrigger size="sm" value={createForm.subnetwork} onValueChange={(value) => setCreateForm({ ...createForm, subnetwork: value })} items={[
            { value: '', label: '使用默认网络' },
            ...subnetworks.map((sub) => ({ value: sub.name, label: `${sub.name}（${sub.ipCidrRange}）` })),
          ]} />
        </label>
        {loadingCreateOptions && <div className="text-xs text-kumo-subtle">正在加载选项…</div>}
      </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" loading={submittingCreate} onClick={onSubmit}>创建</LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
