const fs = require('fs');
const file = 'E:/Code/api-monitor/modules/gemini-cli-api/gemini-client.js';
let code = fs.readFileSync(file, 'utf8');

// 1. 替换 _parseImageUrl 使用原生 fetch 提高兼容性，并增加错误边界
const newImageMethod = `  /**
   * 解析图像 URL 并转换为 Gemini inlineData 格式
   * 已升级：支持网络图片自动下载，且使用原生 fetch 避免依赖报错
   */
  async _parseImageUrl(imageUrl) {
    if (!imageUrl) return null;

    // 处理 Base64 Data URI
    const base64Match = imageUrl.match(/^data:image\\/(\\w+);base64,(.+)$/);
    if (base64Match) {
      return {
        inlineData: {
          mimeType: \`image/\${base64Match[1]}\`,
          data: base64Match[2],
        },
      };
    }

    // 处理本地文件路径 (/uploads/...)
    if (imageUrl.startsWith('/uploads/')) {
      try {
        const fs = require('fs');
        const path = require('path');
        const relativePath = imageUrl.startsWith('/') ? imageUrl.slice(1) : imageUrl;
        const filePath = path.join(process.cwd(), 'data', relativePath);

        if (fs.existsSync(filePath)) {
          const fileBuffer = fs.readFileSync(filePath);
          const base64Data = fileBuffer.toString('base64');
          const ext = path.extname(filePath).toLowerCase();
          let mimeType = 'image/jpeg';
          if (ext === '.png') mimeType = 'image/png';
          else if (ext === '.webp') mimeType = 'image/webp';
          return { inlineData: { mimeType, data: base64Data } };
        }
      } catch (e) {
        logger.error(\`[Gemini-Client] Local image error: \${e.message}\`);
      }
    }

    // 处理 HTTP/HTTPS URL (核心优化：参考 CLIProxyAPI 的自动转码)
    if (imageUrl.startsWith('http')) {
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 15000);
        
        const response = await fetch(imageUrl, { signal: controller.signal });
        clearTimeout(timeoutId);
        
        if (!response.ok) throw new Error(\`HTTP \${response.status}\`);
        
        const arrayBuffer = await response.arrayBuffer();
        const mimeType = response.headers.get('content-type') || 'image/jpeg';
        const base64Data = Buffer.from(arrayBuffer).toString('base64');
        
        return {
          inlineData: {
            mimeType: mimeType,
            data: base64Data
          }
        };
      } catch (e) {
        logger.warn(\`Failed to download image: \${imageUrl.substring(0, 60)} - \${e.message}\`);
        return null;
      }
    }

    return null;
  }`;

// 2. 增强工具挂载逻辑：自动识别模型后缀挂载 Search/Code
const newToolsLogic = `    // 处理 tools (参考 CLIProxyAPI 的自动挂载逻辑)
    const geminiTools = [];
    
    // A. 自动挂载：如果模型 ID 包含 -search，强制开启谷歌搜索
    if (model.includes('-search')) {
      geminiTools.push({ googleSearch: {} });
    }

    // B. 手动挂载：解析请求中的 tools
    if (tools && Array.isArray(tools) && tools.length > 0) {
      const functionDeclarations = [];
      for (const t of tools) {
        if (t.type === 'function' && t.function) {
          const fn = t.function;
          const fnDecl = { name: fn.name, description: fn.description || '' };
          if (fn.parameters) {
            fnDecl.parametersJsonSchema = fn.parameters;
            if (!fnDecl.parametersJsonSchema.type) fnDecl.parametersJsonSchema.type = 'object';
          } else {
            fnDecl.parametersJsonSchema = { type: 'object', properties: {} };
          }
          functionDeclarations.push(fnDecl);
        }
        // 防止重复挂载 googleSearch
        if (t.google_search && !model.includes('-search')) {
          geminiTools.push({ googleSearch: t.google_search });
        }
        if (t.code_execution) geminiTools.push({ codeExecution: t.code_execution });
      }
      if (functionDeclarations.length > 0) {
        geminiTools.push({ functionDeclarations });
      }
    }

    if (geminiTools.length > 0) {
      payload.tools = geminiTools;
    }`;

// 执行代码替换逻辑
const startIdx = code.indexOf('  async _parseImageUrl(imageUrl) {');
const endIdx = code.indexOf('    return null;\n  }', startIdx);
if (startIdx !== -1 && endIdx !== -1) {
  code = code.substring(0, startIdx) + newImageMethod + code.substring(endIdx + 17);
}

const toolsMarker = '// 处理 tools (Function Calling / Search 等)';
const toolsEndMarker = 'payload.tools = geminiTools;';
const tStart = code.indexOf(toolsMarker);
const tEnd = code.indexOf(toolsEndMarker, tStart);
if (tStart !== -1 && tEnd !== -1) {
    code = code.substring(0, tStart) + newToolsLogic + code.substring(tEnd + toolsEndMarker.length);
}

fs.writeFileSync(file, code);
console.log('Client logic refined with native fetch and auto-tools.');
