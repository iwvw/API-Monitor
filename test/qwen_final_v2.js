const axios = require('axios');

const API_CONFIG = {
    baseURL: 'http://localhost:5173/v1',
    headers: {
        'Authorization': 'Bearer ssln5014.',
        'Content-Type': 'application/json'
    }
};

async function testThinking() {
    console.log('\n--- 🧪 用例 1: 深度思考流分流测试 ---');
    try {
        const res = await axios.post(`${API_CONFIG.baseURL}/chat/completions`, {
            model: 'qwen3.6-plus',
            messages: [{ role: 'user', content: '请深度论述什么是存在主义？' }],
            stream: true
        }, { ...API_CONFIG, responseType: 'stream' });

        return new Promise((resolve) => {
            let hasReasoning = false;
            let hasContent = false;
            res.data.on('data', chunk => {
                const text = chunk.toString();
                if (text.includes('"reasoning_content"')) hasReasoning = true;
                if (text.includes('"content"') && !text.includes('"reasoning_content"')) hasContent = true;
            });
            res.data.on('end', () => {
                console.log('    • 结果:', (hasReasoning && hasContent) ? '✅ 成功 (思维链与正文已完美分离)' : '❌ 失败 (未捕捉到思维链或正文)');
                resolve();
            });
        });
    } catch (e) {
        console.error('    • 错误:', e.message);
    }
}

async function testMemory() {
    console.log('\n--- 🧪 用例 2: 多轮对话记忆测试 ---');
    try {
        // 这一步模拟了网关的注入逻辑
        const res = await axios.post(`${API_CONFIG.baseURL}/chat/completions`, {
            model: 'qwen3.6-plus',
            messages: [
                { role: 'user', content: '我现在的密码是 123456，请记住。' },
                { role: 'assistant', content: '好的，我已经记住了您的密码是 123456。' },
                { role: 'user', content: '我刚才告诉你的密码是多少？' }
            ],
            stream: false
        }, API_CONFIG);
        const content = res.data.choices[0].message.content;
        console.log('    • 模型回答:', content);
        console.log('    • 结果:', content.includes('123456') ? '✅ 成功 (记忆召回顺利)' : '❌ 失败 (内容里没找到 123456)');
    } catch (e) {
        console.error('    • 错误:', e.message);
    }
}

async function testT2I() {
    console.log('\n--- 🧪 用例 3: 通义万相生图测试 ---');
    try {
        const res = await axios.post(`${API_CONFIG.baseURL}/images/generations`, {
            prompt: '一只赛博朋克风格的赛博小猫',
            model: 'qwen3.6-plus'
        }, API_CONFIG);
        const url = res.data.data?.[0]?.url;
        console.log('    • 图片 URL:', url || '未获取到');
        console.log('    • 结果:', url && url.startsWith('http') ? '✅ 成功' : '❌ 失败');
    } catch (e) {
        console.error('    • 错误:', e.message);
    }
}

async function start() {
    console.log('🚀 开始 Qwen AI 网关终极回归测试...');
    await testThinking();
    await testMemory();
    await testT2I();
    console.log('\n🎉 所有测试执行完毕。');
}

start();
