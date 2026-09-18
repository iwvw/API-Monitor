import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { AlertTriangle } from '../../components/Icons.jsx';
import {
  DEFAULT_MODEL_HEALTH_CONCURRENCY,
  DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
} from '../../modules/openaiModelHealth.js';

export function HealthCheckDialog({ healthApi }) {
  const {
    healthCheckModal, setHealthCheckModal,
    healthCheckForm, setHealthCheckForm,
    modelHealthBatchLoading,
    startBatchHealthCheck,
  } = healthApi;
  return (
      <LayerDialog.Root open={healthCheckModal} onOpenChange={setHealthCheckModal}>
        <LayerDialog.Content size="sm">
          <LayerDialog.Title>模型健康检测</LayerDialog.Title>
          <LayerDialog.Description>
            按设定并发逐批发送轻量请求，测试每个模型的可用性与延迟。
          </LayerDialog.Description>

          <LayerDialog.Body>
          <div className="space-y-4">
            <div className="bg-kumo-warning/10 border border-kumo-warning/20 text-kumo-warning px-3 py-2 text-sm space-y-1">
              <p className="font-semibold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                警告
              </p>
              <p>批量检测会发送真实请求；并发数越高，越容易触发供应商限流、风控或短时失败。</p>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">检测方式</span>
              <StatusBadge tone="info">后端批量检测</StatusBadge>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">超时限制</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测超时限制"
                  type="number"
                  value={healthCheckForm.timeout}
                  onChange={e =>
                    setHealthCheckForm({ ...healthCheckForm, timeout: Number(e.target.value) })
                  }
                  min={1}
                  max={60}
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">秒</span>
              </div>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">并发数</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测并发数"
                  type="number"
                  value={healthCheckForm.concurrency}
                  onChange={e =>
                    setHealthCheckForm({
                      ...healthCheckForm,
                      concurrency: Number(e.target.value),
                    })
                  }
                  min={1}
                  max={30}
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">个请求</span>
              </div>
            </div>

            <p className="text-xs text-kumo-subtle">
              默认并发 {DEFAULT_MODEL_HEALTH_CONCURRENCY}、超时{' '}
              {DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS} 秒；批量检测全部启用端点上的模型，完成后统一回填结果。
            </p>
          </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              loading={modelHealthBatchLoading}
              onClick={startBatchHealthCheck}
            >
              开始检测
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
