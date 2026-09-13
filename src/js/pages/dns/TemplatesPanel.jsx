import React from 'react';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { Toolbar } from '@cloudflare/kumo';
import { Download, Edit, FileText, Plus, Trash, Upload } from '../../components/Icons.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { downloadJson, formatDate } from './utils.jsx';

function TemplatesPanel({
  templates,
  templateColWidths,
  startTemplateResize,
  isArmed,
  openImportModal,
  openTemplateModal,
  applyTemplate,
  deleteTemplate,
}) {
  return (
    <SectionCard
      title="DNS 模板"
      icon={<FileText className="h-4 w-4 text-brand" />}
      actions={(
        <>
        <Toolbar size="sm" aria-label="导出导入模板" className="shrink-0">
          <Toolbar.Button onClick={() => openImportModal('templates')} aria-label="导入模板" title="导入模板" icon={<Download className="h-3.5 w-3.5" />}>
            <span className="hidden cq-sm:inline">导入</span>
          </Toolbar.Button>
          <Toolbar.Button onClick={() => downloadJson(`cloudflare-dns-templates-${Date.now()}.json`, { version: '1.0', templates })} aria-label="导出模板" title="导出模板" icon={<Upload className="h-3.5 w-3.5" />}>
            <span className="hidden cq-sm:inline">导出</span>
          </Toolbar.Button>
        </Toolbar>
        <Button size="sm" onClick={() => openTemplateModal()} icon={<Plus className="h-4 w-4" />}>添加模板</Button>
        </>
      )}
      bodyPadding="none"
      bodyClassName="overflow-x-auto"
    >
      <Table layout="fixed">
        <colgroup>{templateColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(0, e)} onTouchStart={(e) => startTemplateResize(0, e)} /></Table.Head>
            <Table.Head className="relative pr-6">记录数<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(1, e)} onTouchStart={(e) => startTemplateResize(1, e)} /></Table.Head>
            <Table.Head className="relative pr-6">描述<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(2, e)} onTouchStart={(e) => startTemplateResize(2, e)} /></Table.Head>
            <Table.Head className="relative pr-6">更新时间<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(3, e)} onTouchStart={(e) => startTemplateResize(3, e)} /></Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {templates.length === 0 ? (
            <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 DNS 模板。</Table.Cell></Table.Row>
          ) : templates.map((template) => (
            <Table.Row
              key={template.id}
              className="cursor-pointer"
              title="双击编辑模板"
              onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openTemplateModal(template))}
            >
              <Table.Cell className="font-medium text-kumo-strong">{template.name}</Table.Cell>
              <Table.Cell>{template.records?.length || 0}</Table.Cell>
              <Table.Cell><div className="truncate">{template.description || '-'}</div></Table.Cell>
              <Table.Cell>{formatDate(template.updatedAt || template.createdAt)}</Table.Cell>
              <Table.Cell className="text-right">
                <div className="inline-flex gap-2">
                  <Button size="sm" variant="secondary" onClick={() => applyTemplate(template)}>应用</Button>
                  <Button size="sm" shape="square" variant="secondary" onClick={() => openTemplateModal(template)} aria-label={`编辑 ${template.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                  <Button size="sm" shape="square" variant={isArmed(`template:${template.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTemplate(template)} aria-label={`删除 ${template.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                </div>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </SectionCard>
  );
}

export default TemplatesPanel;
