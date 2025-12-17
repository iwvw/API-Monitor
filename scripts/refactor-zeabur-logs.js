/**
 * 批量重构Zeabur router.js的日志输出
 */

const fs = require('fs');
const path = require('path');

const filePath = path.join(__dirname, '../modules/zeabur-api/router.js');
let content = fs.readFileSync(filePath, 'utf8');

// 简单的字符串替换
content = content.replace(/console\.log\('📥 收到项目请求:', accounts\?\.length, '个账号'\);/g,
  "logger.info(`获取项目信息 (\${accounts.length}个账号)`);"
);

content = content.replace(/console\.log\(`🔍 正在获取账号 \[(.+?)\] 的项目\.\.\.\`\);/g,
  ""
);

content = content.replace(/console\.log\(`📦 \[(.+?)\] 找到 \$\{projects\.length\} 个项目`\);/g,
  "logger.groupItem(`\${$1}: \${projects.length} 个项目`);"
);

content = content.replace(/console\.log\(`  - \$\{project\?\.name \|\| pid\}: \$\$\{cost\.toFixed\(2\)\}`\);/g,
  ""
);

content = content.replace(/console\.log\('📤 返回项目结果'\);/g,
  "logger.success(`返回 \${results.length} 个账号的项目信息`);"
);

content = content.replace(/console\.error\('❌ \/api\/temp-projects 未捕获异常:', error\);/g,
  "logger.error('获取项目信息失败', error.message);"
);

content = content.replace(/console\.log\('📥 收到账号请求:', accounts\?\.length, '个账号'\);/g,
  "logger.info(`获取账号信息 (\${accounts.length}个)`);"
);

content = content.replace(/console\.log\(`🔍 正在获取账号 \[(.+?)\] 的数据\.\.\.\`\);/g,
  ""
);

content = content.replace(/console\.log\(`   API 返回的 credit: \$\{user\.credit\}, serviceCosts: \$\$\{serviceCosts\}`\);/g,
  ""
);

content = content.replace(/console\.log\(`💰 \[(.+?)\] 用量: \$\$\{usageData\.totalUsage\.toFixed\(2\)\}, 剩余: \$\$\{usageData\.freeQuotaRemaining\.toFixed\(2\)\}`\);/g,
  "logger.groupItem(`\${$1}: 用量 $\${usageData.totalUsage.toFixed(2)}, 剩余 $\${usageData.freeQuotaRemaining.toFixed(2)}`);"
);

content = content.replace(/console\.log\(`⚠️ \[(.+?)\] 获取用量失败:`, e\.message\);/g,
  "logger.warn(`\${$1}: 获取用量失败 - \${e.message}`);"
);

content = content.replace(/console\.log\('📤 返回结果:', results\.length, '个账号'\);/g,
  "logger.success(`返回 \${results.length} 个账号信息`);"
);

content = content.replace(/console\.error\('❌ \/api\/temp-accounts 未捕获异常:', error\);/g,
  "logger.error('获取账号信息失败', error.message);"
);

content = content.replace(/console\.error\(`❌ \[(.+?)\] 错误:`, error\.message\);/g,
  "logger.error(`\${$1}: \${error.message}`);"
);

content = content.replace(/console\.error\('❌ \/api\/projects 未捕获异常:', error\);/g,
  "logger.error('获取项目失败', error.message);"
);

content = content.replace(/console\.log\(`📋 返回 \$\{allAccounts\.length\} 个账号 \(环境变量: \$\{envAccounts\.length\}, 主机: \$\{serverAccounts\.length\}\)`\);/g,
  "logger.info(`加载 \${allAccounts.length} 个账号 (环境: \${envAccounts.length}, 主机: \${serverAccounts.length})`);"
);

content = content.replace(/console\.log\(`✅ 保存 \$\{accounts\.length\} 个账号到主机`\);/g,
  "logger.success(`保存 \${accounts.length} 个账号`);"
);

content = content.replace(/console\.log\(`🗑️ 删除账号: \$\{removed\[0\]\.name\}`\);/g,
  "logger.info(`删除账号: \${removed[0].name}`);"
);

// 写回文件
fs.writeFileSync(filePath, content, 'utf8');

console.log('✓ Zeabur router.js 日志重构完成');
