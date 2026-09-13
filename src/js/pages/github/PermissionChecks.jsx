import { Badge, Text } from '@cloudflare/kumo';
import { scopeBadgeVariant } from './constants.js';
import { formatDateTime, parseJSON } from './utils.js';

function PermissionChecks({ token }) {
  const permissions = parseJSON(token.permissions_json);
  const checks = Array.isArray(permissions.checks) ? permissions.checks : [];
  const scopes = Array.isArray(permissions.scopes) ? permissions.scopes : [];
  if (checks.length === 0 && scopes.length === 0 && !token.last_test_error) {
    if (token.last_test_status === 'success' && token.last_test_at) {
      return <Text variant="secondary" size="xs">基础认证通过。选择仓库后再次检测可验证 Actions 和 Traffic 权限。检测时间：{formatDateTime(token.last_test_at)}</Text>;
    }
    return <Text variant="secondary" size="xs">点击“检测权限”验证 Token；选择仓库后可同时验证仓库权限。</Text>;
  }
  return (
    <div className="grid gap-2">
      {scopes.length > 0 && (
        <div className="flex min-w-0 flex-wrap items-center gap-1 text-[11px] text-kumo-subtle">
          <span>Classic scopes</span>
          {scopes.map((scope) => <Badge key={scope} variant={scopeBadgeVariant(scope)}>{scope}</Badge>)}
        </div>
      )}
      {checks.length > 0 && (
        <div className="grid gap-1 cq-sm:grid-cols-2">
          {checks.map((check) => (
            <div key={check.key || check.label} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-kumo-line px-2 py-1.5 text-[11px]">
              <span className="min-w-0 truncate text-kumo-strong">{check.label}</span>
              <div className="flex min-w-0 items-center gap-1">
                <span className="hidden max-w-32 truncate text-kumo-subtle cq-md:inline">{check.level}</span>
                <Badge variant={check.status === 'success' ? 'success' : check.status === 'skipped' ? 'neutral' : 'danger'}>
                  {check.status === 'success' ? '通过' : check.status === 'skipped' ? '跳过' : '失败'}
                </Badge>
              </div>
            </div>
          ))}
        </div>
      )}
      {token.last_test_error && <div className="truncate text-xs text-kumo-danger">{token.last_test_error}</div>}
    </div>
  );
}

export default PermissionChecks;