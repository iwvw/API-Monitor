/**
 * 邮箱验证码收件箱 Worker（无外部依赖，由面板注入部署）
 *
 * 用途：把发往已接入 Cloudflare Email Routing 域名的邮件原文与元数据回调给面板，
 * 由面板用标准库 MIME 解析与提取（面板侧解析能力远强于 Worker 单文件）；
 * 若配置了 FORWARD_TO，则把邮件继续转发到该地址，保持原有邮件流不中断。
 *
 * 绑定（由面板部署时注入）：
 *   PANEL_BASE_URL        面板公网地址，如 https://panel.example.com
 *   PANEL_WORKER_SECRET   与面板共享的握手密钥
 *   FORWARD_TO            可选，邮件继续转发的目标地址（需为已激活的目的地地址）
 */

const MAX_RAW_BYTES = 512 * 1024;

export default {
	async email(message, env, ctx) {
		if (env.PANEL_BASE_URL && env.PANEL_WORKER_SECRET) {
			ctx.waitUntil(ingest(message, env).catch((e) => console.error('ingest failed', e)));
		}
		if (env.FORWARD_TO) {
			try {
				await message.forward(env.FORWARD_TO);
			} catch (e) {
				console.error('forward failed', e);
			}
		}
	},
};

// ingest 把邮件原文与元数据投递到面板。
// 只做「搬运」：不在 Worker 内做提取，避免单文件启发式带来的误判。
async function ingest(message, env) {
	let raw = '';
	try {
		raw = await streamToText(message.raw);
	} catch (e) {
		console.error('read raw failed', e);
	}
	if (raw.length > MAX_RAW_BYTES) {
		raw = raw.slice(0, MAX_RAW_BYTES);
	}
	const res = await fetch(env.PANEL_BASE_URL + '/api/emailcode/ingest', {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json',
			'X-Worker-Secret': env.PANEL_WORKER_SECRET,
		},
		body: JSON.stringify({
			mailbox: message.to || '',
			sender: message.from || '',
			subject: message.headers.get('subject') || '',
			messageId: message.headers.get('message-id') || '',
			receivedAt: new Date().toISOString(),
			raw,
		}),
	});
	console.log('panel responded', res.status);
}

async function streamToText(stream) {
	const reader = stream.getReader();
	const decoder = new TextDecoder();
	let out = '';
	for (;;) {
		const { done, value } = await reader.read();
		if (done) break;
		out += decoder.decode(value, { stream: true });
	}
	out += decoder.decode();
	return out;
}
