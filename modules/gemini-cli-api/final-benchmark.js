const baseModels = [
  'gemini-3.1-pro-preview',
  'gemini-2.5-pro',
  'gemini-2.5-flash'
];
const questions = [
  '9.9和9.11谁大？',
  '为什么鲁迅要打周树人？',
  '解释一下什么是递归。'
];

async function runFinalBenchmark() {
  const results = [];
  console.log('开始基础模型性能测试 (默认开启思考)...');
  
  for (const model of baseModels) {
    for (let i = 0; i < 3; i++) {
      const question = questions[i];
      const startTime = Date.now();
      let firstTokenTime = null;
      let fullContent = '';
      let fullReasoning = '';
      let usage = null;
      
      try {
        const res = await fetch('http://localhost:5173/v1/chat/completions', {
          method: 'POST',
          headers: {
            'Authorization': 'Bearer ssln5014.',
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            model: model,
            stream: true,
            messages: [{ role: 'user', content: question }]
          })
        });
        
        if (res.status === 200) {
          const decoder = new TextDecoder();
          for await (const chunk of res.body) {
            const text = decoder.decode(chunk);
            const lines = text.split('\n');
            for (const line of lines) {
              if (line.startsWith('data: ') && !line.includes('[DONE]')) {
                try {
                  const data = JSON.parse(line.substring(6));
                  const delta = data.choices?.[0]?.delta;
                  if (delta?.content) {
                    fullContent += delta.content;
                    if (firstTokenTime === null) firstTokenTime = Date.now() - startTime;
                  }
                  if (delta?.reasoning_content) {
                    fullReasoning += delta.reasoning_content;
                    if (firstTokenTime === null) firstTokenTime = Date.now() - startTime;
                  }
                  if (data.usage) usage = data.usage;
                } catch(e) {}
              }
            }
          }
        }
        const endTime = Date.now();
        results.push({
          model,
          question,
          reasoning: fullReasoning.trim().replace(/\n/g, ' ').substring(0, 40) + (fullReasoning.length > 40 ? '...' : ''),
          reply: fullContent.trim().replace(/\n/g, ' ').substring(0, 40) + (fullContent.length > 40 ? '...' : ''),
          ttfb: firstTokenTime || (endTime - startTime),
          totalTime: endTime - startTime,
          tokens: usage ? (usage.totalTokenCount || 0) : 0,
          status: res.status === 200 ? '✅' : '❌'
        });
      } catch (err) {
        results.push({ model, question, reasoning: '-', reply: 'Error', ttfb: '-', totalTime: 0, tokens: 0, status: '🚨' });
      }
      console.log(`  完成: ${model} - 轮次 ${i+1}`);
    }
  }
  
  console.log('\n---FINAL_REPORT_START---');
  console.log('| 模型 ID | 提问内容 | 思考内容(Reasoning) | 最终回复摘要 | 首字时间 | 总耗时 | 总Token |');
  console.log('|:---|:---|:---|:---|:---|:---|:---|');
  for (const r of results) {
    console.log(`| ${r.model} | ${r.question} | ${r.reasoning || '-'} | ${r.reply} | ${r.ttfb}ms | ${r.totalTime}ms | ${r.tokens} |`);
  }
  console.log('---FINAL_REPORT_END---\n');
}

runFinalBenchmark();