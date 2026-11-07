import { TokenPlugin } from './TokenPlugin.jsx';
import { AutoClawBrand } from '../../../components/Icons.jsx';

// AutoClawPlugin：AutoClaw（智谱 autoglm 桌面客户端）账号转 OpenAI 兼容端点。
//
// 上游是无状态 OpenAI 兼容代理，但转发路径有四道闸（max_tokens 下限、
// system 首句身份、X-Harness-Type、metadata），全部在后端落实，前端无需感知。
export function AutoClawPlugin() {
  return (
    <TokenPlugin
      config={{ id: 'autoclaw' }}
      api="/api/autoclaw"
      brandIcon={AutoClawBrand}
      title="AutoClaw"
      hasRegion
      hasCheckin
      addHint="从 AutoClaw 桌面端登录态里复制 token 与 refreshToken（token 是 safeStorage 密文时请先用客户端导出明文）。国内版走 zhipuai.cn，国际版走 autoglm.ai。"
      credentialFields={[
        { key: 'access_token', label: 'accessToken（必填）', placeholder: 'eyJhbGci…', required: true },
        { key: 'refresh_token', label: 'refreshToken（推荐，用于自动续期）', placeholder: '用于 access token 过期后自动续期' },
      ]}
      strategyOptions={[
        { value: 'first', label: '固定首个可用' },
        { value: 'round-robin', label: '轮询' },
      ]}
    />
  );
}
