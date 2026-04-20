const models = [
  'gemini-3.1-pro-preview',
  'gemini-3-pro-preview',
  'gemini-3-flash-preview',
  'gemini-2.5-pro',
  'gemini-2.5-flash',
  'gemini-2.5-flash-lite'
];
const questions = [
  '为什么天空是蓝色的？一句话回答。',
  '9.9和9.11谁大？',
  '用Python写一个Hello World。'
];

async function runTests() {
  const results = [];
  
  for (const model of models) {
    for (let i = 0; i < 3; i++) {
      const question = questions[i];
      const startTime = Date.now();
      let firstTokenTime = null;
      let fullContent = '';
      let fullReasoning = '';
      let usage = null;
      let statusStr = '';
      
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
        
        statusStr = res.status === 200 ? '成功' : '失败(' + res.status + ')';
        
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
          reasoning: fullReasoning.replace(/\n/g, ' ').substring(0, 30) + (fullReasoning.length > 30 ? '...' : ''),
          reply: fullContent.replace(/\n/g, ' ').substring(0, 30) + (fullContent.length > 30 ? '...' : ''),
          ttfb: firstTokenTime ? firstTokenTime : (endTime - startTime),
          totalTime: endTime - startTime,
          tokens: usage ? (usage.totalTokenCount || usage.total_tokens || 0) : 0,
          status: statusStr
        });
      } catch (err) {
        results.push({
          model,
          question,
          reasoning: '',
          reply: err.message,
          ttfb: '-',
          totalTime: Date.now() - startTime,
          tokens: 0,
          status: '出错'
        });
      }
      console.log(`Tested ${model} - Run ${i+1}`);
    }
  }
  
  console.log('\n---TABLE_START---');
  console.log('| 模型 | 提问内容 | 思考内容 | 回复内容摘要 | 首字时间 | 总时间 | 总Token | 状态 |');
  console.log('|---|---|---|---|---|---|---|---|');
  for (const r of results) {
    console.log(`| ${r.model} | ${r.question} | ${r.reasoning || '-'} | ${r.reply || '-'} | ${r.ttfb}ms | ${r.totalTime}ms | ${r.tokens} | ${r.status} |`);
  }
  console.log('---TABLE_END---\n');
}

runTests();