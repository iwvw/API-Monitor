#!/usr/bin/env node
/**
 * 刷新 @cloudflare/kumo 组件注册表快照（docs/reference/）。
 *
 *   node tools/kumo-registry-snapshot.mjs           # 写入 docs/reference/
 *   node tools/kumo-registry-snapshot.mjs --check   # 只比对，不写文件
 *
 * 产物：
 *   docs/reference/kumo-component-registry.json  直接复制包内的 ai/component-registry.json
 *   docs/reference/kumo-component-registry.md    由上面的 JSON 生成
 *
 * Markdown 生成逻辑逐行移植自上游 `scripts/component-registry/markdown-generator.ts`
 * 的 `generateAIContext()`。上游是 TS 且仓库没有 TS 运行时，所以这里用纯 ESM 复刻；
 * 升级 kumo 后如果上游改了生成规则，需要同步这个文件（`npm run kumo:registry` 会暴露 diff）。
 */
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const kumoRoot = path.join(root, 'node_modules/@cloudflare/kumo');
const sourceJson = path.join(kumoRoot, 'ai/component-registry.json');
const targetJson = path.join(root, 'docs/reference/kumo-component-registry.json');
const targetMd = path.join(root, 'docs/reference/kumo-component-registry.md');

if (!fs.existsSync(sourceJson)) {
  console.error(`找不到 ${path.relative(root, sourceJson)}，先 npm install。`);
  process.exit(1);
}

// --- 与上游 example-cleanup.ts 对齐 -----------------------------------------

const UNDEFINED_COMPONENTS = ['RefreshButton', 'LinkButton', 'DefaultMenuBar', 'ToastTriggerButton'];
const UNDEFINED_VARS = ['args.placeholder', 'args.inputSide', 'botList', 'INITIAL_BOT_LIST'];
const seenExampleSignatures = new Map();

function cleanupExample(example) {
  let cleaned = example;
  cleaned = cleaned.replace(/(\w+)="(\(\)\s*=>\s*\{[^}]*\})"/g, '$1={$2}');
  cleaned = cleaned.replace(/(\w+)=\{`(\[[\s\S]*?\])`\}/g, '$1={$2}');
  cleaned = cleaned.replace(/\\`/g, '`');
  cleaned = cleaned.replace(/\{``/g, '{`');
  cleaned = cleaned.replace(/``\}/g, '`}');
  for (const prop of ['label']) {
    const pattern = new RegExp(`(${prop})=\\{([A-Z][a-z]+)\\}(?![\\w.])`, 'g');
    cleaned = cleaned.replace(pattern, '$1="$2"');
  }
  return cleaned;
}

function shouldIncludeExample(example, componentName) {
  for (const comp of UNDEFINED_COMPONENTS) {
    if (example.includes(`<${comp}`)) return false;
  }
  if (example.trim().length < 10) return false;
  if (new RegExp(`^<${componentName}\\s*/>$`).test(example.trim())) return false;
  for (const varName of UNDEFINED_VARS) {
    if (example.includes(varName)) return false;
  }
  return true;
}

function getExampleSignature(example) {
  const propPattern = /(\w+)=["'{]/g;
  const props = [];
  let match;
  while ((match = propPattern.exec(example)) !== null) props.push(match[1]);
  return props.sort().join(',');
}

function isNearDuplicateExample(example, componentName) {
  const signature = getExampleSignature(example);
  let signatures = seenExampleSignatures.get(componentName);
  if (!signatures) {
    signatures = new Set();
    seenExampleSignatures.set(componentName, signatures);
  }
  if (signatures.has(signature)) return true;
  signatures.add(signature);
  return false;
}

// --- 与上游 markdown-generator.ts 对齐 --------------------------------------

function generateAIContext(registry) {
  seenExampleSignatures.clear();
  let context = `# Kumo Component Registry

> Auto-generated component metadata for AI/agent consumption.

`;

  for (const [name, comp] of Object.entries(registry.components)) {
    context += `---\n\n`;
    context += `### ${name}\n\n`;
    context += `${comp.description}\n\n`;
    context += `**Type:** ${comp.type}\n\n`;
    context += `**Import:** \`import { ${name} } from "${comp.importPath}";\`\n\n`;
    context += `**Category:** ${comp.category}\n\n`;

    context += `**Props:**\n\n`;
    for (const [propName, prop] of Object.entries(comp.props)) {
      const required = prop.required ? ' (required)' : '';
      const defaultVal = prop.default ? ` [default: ${prop.default}]` : '';
      context += `- \`${propName}\`: ${prop.type}${required}${defaultVal}\n`;

      if (prop.values && prop.descriptions) {
        for (const val of prop.values) {
          const desc = prop.descriptions[val];
          if (desc) context += `  - \`"${val}"\`: ${desc}\n`;
        }
      } else if (prop.description) {
        context += `  ${prop.description}\n`;
      }

      if (prop.stateClasses && Object.keys(prop.stateClasses).length > 0) {
        context += `\n  **State Classes:**\n`;
        for (const [variantValue, states] of Object.entries(prop.stateClasses)) {
          context += `  - \`"${variantValue}"\`:\n`;
          for (const [stateName, stateClass] of Object.entries(states)) {
            context += `    - \`${stateName}\`: \`${stateClass}\`\n`;
          }
        }
      }
    }

    if (comp.colors.length > 0) {
      context += `\n**Colors (kumo tokens used):**\n\n`;
      context += `\`${comp.colors.join('`, `')}\`\n`;
    }

    if (comp.styling) {
      context += `\n**Styling:**\n\n`;
      if (comp.styling.dimensions) context += `- **Dimensions:** \`${comp.styling.dimensions}\`\n`;
      if (comp.styling.borderRadius)
        context += `- **Border Radius:** \`${comp.styling.borderRadius}\`\n`;
      if (comp.styling.baseTokens && comp.styling.baseTokens.length > 0) {
        context += `- **Base Tokens:** \`${comp.styling.baseTokens.join('`, `')}\`\n`;
      }
      if (comp.styling.states && Object.keys(comp.styling.states).length > 0) {
        context += `- **States:**\n`;
        for (const [stateName, tokens] of Object.entries(comp.styling.states)) {
          context += `  - \`${stateName}\`: \`${tokens.join('`, `')}\`\n`;
        }
      }
      if (comp.styling.icons && comp.styling.icons.length > 0) {
        context += `- **Icons:**\n`;
        for (const icon of comp.styling.icons) {
          const stateInfo = icon.state ? ` (${icon.state})` : '';
          const sizeInfo = icon.size ? ` size ${icon.size}` : '';
          context += `  - \`${icon.name}\`${stateInfo}${sizeInfo}\n`;
        }
      }
      if (comp.styling.inputStyles) {
        context += `- **Input Styles:**\n`;
        if (comp.styling.inputStyles.base) {
          context += `  - Base: \`${comp.styling.inputStyles.base}\`\n`;
        }
        if (
          comp.styling.inputStyles.sizes &&
          Object.keys(comp.styling.inputStyles.sizes).length > 0
        ) {
          context += `  - Sizes:\n`;
          for (const [sizeName, classes] of Object.entries(comp.styling.inputStyles.sizes)) {
            context += `    - \`${sizeName}\`: \`${classes}\`\n`;
          }
        }
      }
      if (comp.styling.sizeVariants && Object.keys(comp.styling.sizeVariants).length > 0) {
        context += `- **Size Variants:**\n`;
        for (const [sizeName, sizeData] of Object.entries(comp.styling.sizeVariants)) {
          context += `  - \`${sizeName}\`:\n`;
          if (sizeData.height) context += `    - Height: ${sizeData.height}px\n`;
          if (sizeData.classes) context += `    - Classes: \`${sizeData.classes}\`\n`;
          if (sizeData.buttonSize) context += `    - Button Size: \`${sizeData.buttonSize}\`\n`;
          if (sizeData.dimensions) {
            context += `    - Dimensions:\n`;
            for (const [key, value] of Object.entries(sizeData.dimensions)) {
              context += `      - ${key}: ${value}\n`;
            }
          }
        }
      }
    }

    if (comp.subComponents && Object.keys(comp.subComponents).length > 0) {
      context += `\n**Sub-Components:**\n\n`;
      context += `This is a compound component. Use these sub-components:\n\n`;
      for (const [subName, subComp] of Object.entries(comp.subComponents)) {
        context += `#### ${name}.${subName}\n\n`;
        context += `${subComp.description}`;
        if (subComp.renderElement) context += ` Renders a \`${subComp.renderElement}\` element.`;
        context += '\n\n';
        if (Object.keys(subComp.props).length > 0) {
          context += `Props:\n`;
          for (const [propName, prop] of Object.entries(subComp.props)) {
            const required = prop.required ? ' (required)' : '';
            const defaultVal = prop.default ? ` [default: ${prop.default}]` : '';
            context += `- \`${propName}\`: ${prop.type}${required}${defaultVal}`;
            if (prop.description) context += ` - ${prop.description}`;
            context += '\n';
          }
          context += '\n';
        }
        if (subComp.usageExamples && subComp.usageExamples.length > 0) {
          context += `Usage:\n`;
          for (const example of subComp.usageExamples) {
            context += `\`\`\`tsx\n${example}\n\`\`\`\n`;
          }
          context += '\n';
        }
      }
    }

    if (comp.examples.length > 0) {
      context += `\n**Examples:**\n\n`;
      for (const example of comp.examples) {
        if (!shouldIncludeExample(example, name)) continue;
        if (isNearDuplicateExample(example, name)) continue;
        const cleanedExample = cleanupExample(example);
        context += `\`\`\`tsx\n${cleanedExample}\n\`\`\`\n\n`;
      }
    }
    context += '\n';
  }

  context += `## Quick Reference

**Components by Category:**
`;
  for (const [category, names] of Object.entries(registry.search.byCategory)) {
    context += `- **${category}:** ${names.join(', ')}\n`;
  }
  return context;
}

// --- 写盘 / 校验 --------------------------------------------------------------

const checkOnly = process.argv.includes('--check');
const sourceRaw = fs.readFileSync(sourceJson, 'utf8');
const registry = JSON.parse(sourceRaw);
const md = generateAIContext(registry);
const kumoPkg = JSON.parse(fs.readFileSync(path.join(kumoRoot, 'package.json'), 'utf8'));

if (checkOnly) {
  const problems = [];
  const currentJson = fs.existsSync(targetJson) ? fs.readFileSync(targetJson, 'utf8') : null;
  const currentMd = fs.existsSync(targetMd) ? fs.readFileSync(targetMd, 'utf8') : null;
  if (currentJson !== sourceRaw) {
    problems.push('kumo-component-registry.json 与包内 ai/component-registry.json 不一致');
  }
  if (currentMd !== md) {
    problems.push('kumo-component-registry.md 与生成结果不一致');
  }
  if (problems.length) {
    console.error('Kumo registry snapshot is stale:');
    for (const p of problems) console.error(`  - ${p}`);
    console.error('运行 `npm run kumo:registry` 刷新。');
    process.exit(1);
  }
  console.log(
    `Kumo registry snapshot is up to date (kumo ${kumoPkg.version}, ` +
      `${Object.keys(registry.components).length} components).`
  );
} else {
  // 原样复制，保持与包内文件逐字节一致（方便下游直接 diff 上游产物）。
  fs.writeFileSync(targetJson, sourceRaw);
  fs.writeFileSync(targetMd, md);
  console.log(
    `Kumo registry snapshot refreshed from kumo ${kumoPkg.version}: ` +
      `${Object.keys(registry.components).length} components, ` +
      `${Object.keys(registry.blocks || {}).length} blocks.`
  );
}
