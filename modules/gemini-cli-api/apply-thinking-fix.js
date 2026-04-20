const fs = require('fs');
const file = 'E:/Code/api-monitor/modules/gemini-cli-api/utils/thinking.js';
let code = fs.readFileSync(file, 'utf8');

const startMarker = '  // 5. Extract thinking config: suffix priority over body';
const endMarker = '  // 6. No config found';

const startIdx = code.indexOf(startMarker);
const endIdx = code.indexOf(endMarker);

const newBlock = `  // 5. Extract thinking config: suffix priority over body
  let config = null;

  if (suffixResult.hasSuffix) {
    if (suffixResult.legacySuffix) {
      config = legacySuffixToConfig(suffixResult.legacySuffix, baseModel);
    } else {
      config = parseSuffixToConfig(suffixResult.rawSuffix);
    }
  } else if (reasoning_effort) {
    // Extract from reasoning_effort body parameter
    const effort = String(reasoning_effort).toLowerCase().trim();
    if (effort === 'none') {
      config = { mode: ThinkingMode.NONE, budget: 0, level: '' };
    } else if (effort === 'auto' || effort === '-1') {
      config = { mode: ThinkingMode.AUTO, budget: -1, level: '' };
    } else {
      const validLevels = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
      if (validLevels.includes(effort)) {
        config = { mode: ThinkingMode.LEVEL, budget: 0, level: effort };
      } else {
        const numeric = parseInt(effort);
        if (!isNaN(numeric) && numeric > 0) {
          config = { mode: ThinkingMode.BUDGET, budget: numeric, level: '' };
        }
      }
    }
  } else if (support && !model.includes('-nothinking')) {
    // --- 核心修复：参考 CLIProxyAPI 的智能默认值 ---
    // 对于 Pro 等支持推理的模型，如果不带后缀也不传参数，默认开启智能推理 (AUTO)
    // 这能让直接请求基础模型 ID (如 gemini-2.5-pro) 时也吐出思考过程
    config = { mode: ThinkingMode.AUTO, budget: -1, level: '' };
    logger.debug(\`Applying smart default thinking (AUTO) for base model: \${baseModel}\`);
  }

`;

if (startIdx !== -1 && endIdx > startIdx) {
  code = code.substring(0, startIdx) + newBlock + code.substring(endIdx);
  fs.writeFileSync(file, code);
  console.log('Thinking logic updated successfully');
} else {
  console.log('Markers not found');
}
