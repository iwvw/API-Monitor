import { TokenPlugin } from './TokenPlugin.jsx';
import { ZCodeBrand } from '../../../components/Icons.jsx';

// ZCodePlugin：ZCode（智谱 / Z.AI 编码代理客户端）订阅登录态转 OpenAI 兼容端点。
//
// 两个凭证字段不能互相替代：accessToken 打推理平面，jwt 打 zcode.z.ai 的领取接口。
// 「每日签到」= 领当天那份赠送套餐。
export function ZCodePlugin() {
  return (
    <TokenPlugin
      config={{ id: 'zcode' }}
      api="/api/zcode"
      brandIcon={ZCodeBrand}
      title="ZCode"
      hasRegion
      hasCheckin
      addHint="从 ZCode 客户端 / 官网登录后复制推理令牌（accessToken）与套餐令牌（jwt）。国内版走 open.bigmodel.cn，国际版走 api.z.ai。"
      credentialFields={[
        { key: 'access_token', label: 'accessToken（必填，推理用）', placeholder: 'eyJhbGci…', required: true },
        { key: 'jwt', label: '套餐 jwt（签到/领取用）', placeholder: 'eyJhbGci…' },
      ]}
      strategyOptions={[
        { value: 'first', label: '固定首个可用' },
        { value: 'round-robin', label: '轮询' },
      ]}
    />
  );
}
