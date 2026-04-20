const modelsWithThinking = [
  'gemini-2.5-pro(8192)',
  'gemini-2.5-flash(4096)',
  'gemini-3.1-pro-preview(low)'
];
const questions = [
  '如果我是我妈妈的唯一的女儿的妈妈，那我是谁？',
  '比较 0.9 和 0.11 的大小，详细解释理由。',
  '解释一下什么是递归。'
];

async function runThinkingTests() {
  const results = [];
  
  for (const model of modelsWithThinking) {
    console.log(`Starting tests for ${model}...`);
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
                  if (data._firstTokenTime && firstTokenTime === null) {
                    firstTokenTime = data._firstTokenTime;
                  }
                  if (data.usage) {
                    usage = data.usage;
                  }
                } catch(e) {}
              }
            }
          }
        }
        const endTime = Date.now();
        results.push({
          model,
          question,
          reasoning: fullReasoning.trim().replace(/\n/g, ' ').substring(0, 50) + (fullReasoning.length > 50 ? '...' : ''),
          reply: fullContent.trim().replace(/\n/g, ' ').substring(0, 50) + (fullContent.length > 50 ? '...' : ''),
          ttfb: firstTokenTime ? firstTokenTime : (endTime - startTime),
          totalTime: endTime - startTime,
          tokens: usage ? (usage.totalTokenCount || 0) : 0,
          status: res.status === 200 ? '成功' : '失败(' + res.status + ')'
        });
      } catch (err) {
        results.push({ model, question, reasoning: '-', reply: err.message, ttfb: '-', totalTime: 0, tokens: 0, status: '出错' });
      }
      console.log(`  Finished Run ${i+1}`);
    }
  }
  
  console.log('\n---THINKING_TABLE---');
  console.log('| 模型 | 提问内容 | 思考内容摘要 | 最终回复摘要 | 首字时间 | 总时间 | 总Token |');
  console.log('|---|---|---|---|---|---|---|');
  for (const r of results) {
    console.log(`| ${r.model} | ${r.question} | ${r.reasoning || '-'} | ${r.reply || '-'} | ${r.ttfb}ms | ${r.totalTime}ms | ${r.tokens} |`);
  }
}

runThinkingTests();