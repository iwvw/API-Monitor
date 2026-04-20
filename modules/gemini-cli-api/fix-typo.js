const fs = require('fs');
const file = 'E:/Code/api-monitor/modules/gemini-cli-api/utils/thinking.js';
let code = fs.readFileSync(file, 'utf8');

// Fix: Change reasoning_effort to reasoningEffort to match function signature
code = code.replace(
  "else if (reasoning_effort) {",
  "else if (reasoningEffort) {"
);

code = code.replace(
  "const effort = String(reasoning_effort).toLowerCase().trim();",
  "const effort = String(reasoningEffort).toLowerCase().trim();"
);

fs.writeFileSync(file, code);
console.log('Fixed variable name typo in thinking.js');
