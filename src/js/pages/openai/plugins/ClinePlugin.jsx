import { TokenPlugin } from './TokenPlugin.jsx';
import { ClineBrand } from '../../../components/Icons.jsx';

// ClinePlugin：Cline（cline.bot）账号转 OpenAI 兼容端点。
//
// 上游是 WorkOS OAuth 令牌（`workos:` 前缀），免费池与 ClinePass 订阅池是
// 同一个账号上的两个额度池。凭证通过「粘贴 token」接入。
export function ClinePlugin() {
  return (
    <TokenPlugin
      config={{ id: 'cline' }}
      api="/api/cline"
      brandIcon={ClineBrand}
      title="Cline"
      hasRegion={false}
      hasCheckin={false}
      addHint="从 Cline 桌面端 ~/.cline/data/settings/providers.json 的 providers.cline.settings.auth 复制 accessToken（含 workos: 前缀）与 refreshToken。"
      credentialFields={[
        { key: 'access_token', label: 'accessToken（必填，含 workos: 前缀）', placeholder: 'workos:eyJhbGci…', required: true },
        { key: 'refresh_token', label: 'refreshToken（推荐，用于自动续期）', placeholder: 'N5EUfPqyERlpRhiF4WUXDPunz' },
      ]}
      strategyOptions={[
        { value: 'first', label: '固定首个可用' },
        { value: 'round-robin', label: '轮询' },
      ]}
    />
  );
}
