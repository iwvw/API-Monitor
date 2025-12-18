#!/usr/bin/env node
/**
 * 模块自动创建脚本
 * 
 * 使用方法：
 *   node create-module.js <模块名>
 * 
 * 示例：
 *   node create-module.js weather-api
 */

const fs = require('fs');
const path = require('path');

// 获取模块名
const moduleName = process.argv[2];

if (!moduleName) {
    console.error('❌ 请提供模块名');
    console.log('用法: node create-module.js <模块名>');
    console.log('示例: node create-module.js weather-api');
    process.exit(1);
}

// 验证模块名格式（kebab-case）
if (!/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/.test(moduleName)) {
    console.error('❌ 模块名必须使用 kebab-case 格式（如 my-feature）');
    process.exit(1);
}

// 转换命名格式
function toCamelCase(str) {
    return str.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
}

function toPascalCase(str) {
    const camel = toCamelCase(str);
    return camel.charAt(0).toUpperCase() + camel.slice(1);
}

function toSnakeCase(str) {
    return str.replace(/-/g, '_');
}

const camelName = toCamelCase(moduleName);           // myFeature
const pascalName = toPascalCase(moduleName);         // MyFeature
const snakeName = toSnakeCase(moduleName);           // my_feature
const prefix = moduleName.split('-')[0].slice(0, 3); // my (前3个字母作为ID前缀)

console.log(`\n🚀 创建模块: ${moduleName}`);
console.log(`   camelCase: ${camelName}`);
console.log(`   PascalCase: ${pascalName}`);
console.log(`   snake_case: ${snakeName}`);
console.log('');

// 路径配置
const rootDir = path.resolve(__dirname, '..');
const templatesDir = __dirname;
const backendDir = path.join(rootDir, 'modules', moduleName);
const cssFile = path.join(rootDir, 'public', 'css', `${moduleName}.css`);
const jsFile = path.join(rootDir, 'public', 'js', 'modules', `${moduleName}.js`);

// 检查模块是否已存在
if (fs.existsSync(backendDir)) {
    console.error(`❌ 模块目录已存在: ${backendDir}`);
    process.exit(1);
}

// 替换模板中的占位符
function processTemplate(content) {
    return content
        .replace(/\{\{MODULE_NAME\}\}/g, moduleName)
        .replace(/\{\{module\}\}/g, moduleName)
        .replace(/\{\{moduleName\}\}/g, camelName)
        .replace(/\{\{ModuleName\}\}/g, pascalName)
        .replace(/\{\{ModelName\}\}/g, pascalName + 'Item')
        .replace(/\{\{table_name\}\}/g, snakeName + '_items')
        .replace(/\{\{prefix\}\}/g, prefix)
        .replace(/\{\{API_PREFIX\}\}/g, `/api/${moduleName}`);
}

// 创建目录
console.log('📁 创建目录...');
fs.mkdirSync(backendDir, { recursive: true });

// 复制后端模板
console.log('📄 创建后端文件...');
const backendTemplates = [
    { src: 'backend/router.template.js', dest: 'router.js' },
    { src: 'backend/storage.template.js', dest: 'storage.js' },
    { src: 'backend/service.template.js', dest: 'service.js' },
    { src: 'backend/schema.template.sql', dest: 'schema.sql' }
];

backendTemplates.forEach(({ src, dest }) => {
    const srcPath = path.join(templatesDir, src);
    const destPath = path.join(backendDir, dest);

    if (fs.existsSync(srcPath)) {
        const content = fs.readFileSync(srcPath, 'utf8');
        const processed = processTemplate(content);
        fs.writeFileSync(destPath, processed);
        console.log(`   ✓ modules/${moduleName}/${dest}`);
    }
});

// 复制前端模板
console.log('📄 创建前端文件...');

// CSS
const cssTemplate = path.join(templatesDir, 'frontend/module.template.css');
if (fs.existsSync(cssTemplate)) {
    const content = fs.readFileSync(cssTemplate, 'utf8');
    const processed = processTemplate(content);
    fs.writeFileSync(cssFile, processed);
    console.log(`   ✓ public/css/${moduleName}.css`);
}

// JS
const jsTemplate = path.join(templatesDir, 'frontend/module.template.js');
if (fs.existsSync(jsTemplate)) {
    const content = fs.readFileSync(jsTemplate, 'utf8');
    const processed = processTemplate(content);
    fs.writeFileSync(jsFile, processed);
    console.log(`   ✓ public/js/modules/${moduleName}.js`);
}

// 输出后续步骤
console.log('\n✅ 模块创建完成!\n');
console.log('📝 后续步骤:');
console.log('');
console.log('1. 在 server.js 中注册路由:');
console.log(`   const ${camelName}Router = require('./modules/${moduleName}/router');`);
console.log(`   app.use('/api/${moduleName}', ${camelName}Router);`);
console.log('');
console.log('2. 在 src/db/models.js 中添加模型类');
console.log('');
console.log('3. 将 schema.sql 内容添加到 src/db/schema.sql');
console.log('');
console.log('4. 在 index.html 中引入 CSS:');
console.log(`   <link rel="stylesheet" href="css/${moduleName}.css">`);
console.log('');
console.log('5. 在 main.js 中导入模块方法');
console.log('');
console.log('6. 在 store.js 中添加模块状态');
console.log('');
console.log(`📖 详细说明请参考: .templates/MODULE_GUIDE.md`);
console.log('');
