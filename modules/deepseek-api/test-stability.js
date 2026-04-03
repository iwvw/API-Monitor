/**
 * DeepSeek 模型稳定性测试脚本
 * 测试所有 4 个模型 + 别名映射
 */

const http = require('http');

const BASE_URL = 'http://localhost:5173';
const API_KEY = 'ssln5014.';

function request(model, message, stream = false) {
    return new Promise((resolve, reject) => {
        const body = JSON.stringify({
            model,
            messages: [{ role: 'user', content: message }],
            stream,
        });

        const options = {
            hostname: 'localhost',
            port: 5173,
            path: '/v1/chat/completions',
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${API_KEY}`,
                'Content-Length': Buffer.byteLength(body),
            },
        };

        const startTime = Date.now();
        const req = http.request(options, (res) => {
            let data = '';
            res.setEncoding('utf8');
            res.on('data', chunk => { data += chunk; });
            res.on('end', () => {
                const elapsed = Date.now() - startTime;
                resolve({ status: res.statusCode, data, elapsed, headers: res.headers });
            });
        });

        req.on('error', (e) => reject(e));
        req.setTimeout(120000, () => {
            req.destroy(new Error('timeout'));
        });
        req.write(body);
        req.end();
    });
}

async function testModel(model, label, stream = false) {
    const prefix = stream ? '[流式]' : '[非流式]';
    console.log(`\n${'='.repeat(60)}`);
    console.log(`${prefix} 测试模型: ${model} (${label})`);
    console.log('='.repeat(60));

    try {
        const prompt = stream
            ? '用一句话介绍你自己'
            : '回复：TEST_OK';

        const result = await request(model, prompt, stream);

        if (result.status === 200) {
            if (stream) {
                // 解析 SSE 数据
                const lines = result.data.split('\n').filter(l => l.startsWith('data: '));
                let content = '';
                let reasoning = '';
                let hasUsage = false;
                for (const line of lines) {
                    const dataStr = line.slice(6).trim();
                    if (dataStr === '[DONE]') continue;
                    try {
                        const chunk = JSON.parse(dataStr);
                        const delta = chunk.choices?.[0]?.delta || {};
                        if (delta.content) content += delta.content;
                        if (delta.reasoning_content) reasoning += delta.reasoning_content;
                        if (chunk.usage) hasUsage = true;
                    } catch (_) { }
                }
                console.log(`✅ 成功 | ${result.elapsed}ms`);
                if (reasoning) console.log(`💭 推理: ${reasoning.slice(0, 100)}...`);
                console.log(`📝 回复: ${content.slice(0, 200)}`);
                console.log(`📊 SSE chunks: ${lines.length}, usage: ${hasUsage ? '✅' : '❌'}`);
            } else {
                const json = JSON.parse(result.data);
                const msg = json.choices?.[0]?.message || {};
                console.log(`✅ 成功 | ${result.elapsed}ms`);
                if (msg.reasoning_content) {
                    console.log(`💭 推理: ${msg.reasoning_content.slice(0, 100)}...`);
                }
                console.log(`📝 回复: ${(msg.content || '').slice(0, 200)}`);
                console.log(`📊 用量: prompt=${json.usage?.prompt_tokens}, completion=${json.usage?.completion_tokens}, total=${json.usage?.total_tokens}`);
            }
        } else {
            console.log(`❌ 失败 | HTTP ${result.status} | ${result.elapsed}ms`);
            console.log(`   错误: ${result.data.slice(0, 300)}`);
        }
    } catch (e) {
        console.log(`❌ 异常: ${e.message}`);
    }
}

async function main() {
    console.log('🚀 DeepSeek 模型稳定性测试');
    console.log(`🔗 端点: ${BASE_URL}/v1/chat/completions`);
    console.log(`🔑 API Key: ${API_KEY.slice(0, 4)}****`);
    console.log(`⏰ 时间: ${new Date().toLocaleString()}`);

    // 1. 基础模型 - 非流式
    await testModel('deepseek-chat', '基础对话', false);

    // 2. 基础模型 - 流式
    await testModel('deepseek-chat', '基础对话-流式', true);

    // 3. 推理模型 - 流式
    await testModel('deepseek-reasoner', '深度思考', true);

    // 4. 搜索模型 - 流式
    await testModel('deepseek-chat-search', '联网搜索', true);

    // 5. 模型别名测试 — gpt-4o → deepseek-chat
    await testModel('gpt-4o', '别名映射 gpt-4o→chat', true);

    // 6. 模型别名测试 — o3 → deepseek-reasoner  
    await testModel('o3', '别名映射 o3→reasoner', true);

    console.log('\n' + '='.repeat(60));
    console.log('🏁 测试完成');
    console.log('='.repeat(60));
}

main().catch(e => {
    console.error('测试脚本异常:', e);
    process.exit(1);
});
