/**
 * Unified Thinking Configuration Module
 * Aligned with CLIProxyAPI's thinking pipeline:
 *   ParseSuffix → extractConfig → validate → apply
 *
 * Supports both legacy suffixes (-maxthinking/-nothinking) and
 * the new bracket suffix format: model(value)
 */

const { createLogger } = require('../../../src/utils/logger');
const logger = createLogger('GCLI-Thinking');

// ============== Thinking Modes ==============

const ThinkingMode = Object.freeze({
  BUDGET: 'budget',   // Numeric budget (e.g., 8192)
  LEVEL: 'level',     // Discrete level (e.g., "high")
  NONE: 'none',       // Thinking disabled
  AUTO: 'auto',       // Automatic/dynamic thinking
});

// ============== Level ↔ Budget Conversion ==============

/** Standard Level → Budget mapping (aligned with CLIProxyAPI) */
const LEVEL_TO_BUDGET = {
  none: 0,
  auto: -1,
  minimal: 512,
  low: 1024,
  medium: 8192,
  high: 24576,
  xhigh: 32768,
  max: 128000,
};

/** Budget → Level threshold-based mapping */
function budgetToLevel(budget) {
  if (budget === -1) return 'auto';
  if (budget === 0) return 'none';
  if (budget <= 512) return 'minimal';
  if (budget <= 1024) return 'low';
  if (budget <= 8192) return 'medium';
  if (budget <= 24576) return 'high';
  return 'xhigh';
}

// ============== Default ThinkingSupport per model family ==============

/**
 * Static model thinking capabilities (fallback when matrix doesn't define them).
 * Aligned with CLIProxyAPI's registry.
 */
const STATIC_THINKING_SUPPORT = {
  // Gemini 2.5 series: budget-only
  'gemini-2.5-pro': { min: 128, max: 65536, zeroAllowed: true, dynamicAllowed: true, levels: [] },
  'gemini-2.5-flash': { min: 1, max: 24576, zeroAllowed: true, dynamicAllowed: true, levels: [] },
  'gemini-2.5-flash-lite': null, // No thinking support

  // Gemini 3 series: level-based (hybrid with budget range)
  'gemini-3-pro-preview': { min: 0, max: 0, zeroAllowed: false, dynamicAllowed: false, levels: ['low', 'medium', 'high'] },
  'gemini-3-flash-preview': { min: 0, max: 0, zeroAllowed: false, dynamicAllowed: false, levels: ['low', 'medium', 'high'] },
  'gemini-3.1-pro-preview': { min: 0, max: 0, zeroAllowed: false, dynamicAllowed: false, levels: ['low', 'medium', 'high'] },
  'gemini-3.1-flash-preview': { min: 0, max: 0, zeroAllowed: false, dynamicAllowed: false, levels: ['low', 'medium', 'high'] },
  'gemini-3.1-flash-lite-preview': null, // No thinking support
};

/**
 * Get ThinkingSupport config for a model.
 * Priority: matrix config > static defaults > family prefix match
 */
function getThinkingSupport(baseModel, matrixConfig) {
  // 1. Check matrix config
  if (matrixConfig && matrixConfig[baseModel]?.thinking) {
    return matrixConfig[baseModel].thinking;
  }

  // 2. Check exact static match
  if (STATIC_THINKING_SUPPORT.hasOwnProperty(baseModel)) {
    return STATIC_THINKING_SUPPORT[baseModel];
  }

  // 3. Family prefix match (for preview/dated variants)
  for (const [key, support] of Object.entries(STATIC_THINKING_SUPPORT)) {
    if (baseModel.startsWith(key)) {
      return support;
    }
  }

  // 4. Heuristic: if model contains "gemini-3", assume level-based
  if (baseModel.includes('gemini-3')) {
    return { min: 0, max: 0, zeroAllowed: false, dynamicAllowed: false, levels: ['low', 'medium', 'high'] };
  }

  // 5. If model is "gemini-2.5-*" but not matched above, assume budget-based
  if (baseModel.includes('gemini-2.5')) {
    return { min: 1, max: 65536, zeroAllowed: true, dynamicAllowed: true, levels: [] };
  }

  // Unknown model - no thinking support
  return null;
}

// ============== Suffix Parsing ==============

/**
 * Parse model name for bracket-style thinking suffix: model(value)
 * Also handles legacy suffixes: -maxthinking, -nothinking
 *
 * @param {string} model - Full model name
 * @returns {{ modelName: string, hasSuffix: boolean, rawSuffix: string, legacySuffix: string|null }}
 */
function parseSuffix(model) {
  if (!model) return { modelName: model, hasSuffix: false, rawSuffix: '', legacySuffix: null };

  // 1. Check bracket suffix: model(value)
  const lastOpen = model.lastIndexOf('(');
  if (lastOpen !== -1 && model.endsWith(')')) {
    return {
      modelName: model.substring(0, lastOpen),
      hasSuffix: true,
      rawSuffix: model.substring(lastOpen + 1, model.length - 1),
      legacySuffix: null,
    };
  }

  // 2. Check legacy suffixes (backward compatibility)
  const legacySuffixes = [
    '-maxthinking-search',
    '-nothinking-search',
    '-maxthinking',
    '-nothinking',
  ];
  for (const suffix of legacySuffixes) {
    if (model.endsWith(suffix)) {
      return {
        modelName: model.substring(0, model.length - suffix.length),
        hasSuffix: true,
        rawSuffix: '',
        legacySuffix: suffix,
      };
    }
  }

  return { modelName: model, hasSuffix: false, rawSuffix: '', legacySuffix: null };
}

/**
 * Parse raw suffix content into a ThinkingConfig object.
 *
 * Priority: special values → level names → numeric budget
 *
 * @param {string} rawSuffix - Content inside parentheses
 * @returns {{ mode: string, budget: number, level: string }|null}
 */
function parseSuffixToConfig(rawSuffix) {
  if (!rawSuffix) return null;
  const val = rawSuffix.toLowerCase().trim();

  // Special values
  if (val === 'none') return { mode: ThinkingMode.NONE, budget: 0, level: '' };
  if (val === 'auto' || val === '-1') return { mode: ThinkingMode.AUTO, budget: -1, level: '' };

  // Level names
  const validLevels = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
  if (validLevels.includes(val)) {
    return { mode: ThinkingMode.LEVEL, budget: 0, level: val };
  }

  // Numeric budget
  const numeric = parseInt(val);
  if (!isNaN(numeric) && numeric >= 0) {
    if (numeric === 0) return { mode: ThinkingMode.NONE, budget: 0, level: '' };
    return { mode: ThinkingMode.BUDGET, budget: numeric, level: '' };
  }

  // Unknown format
  logger.debug(`Unknown suffix format: "${rawSuffix}", treating as no config`);
  return null;
}

/**
 * Convert legacy suffix to ThinkingConfig.
 *
 * @param {string} legacySuffix - Legacy suffix string (e.g., "-maxthinking")
 * @param {string} baseModel - Base model name for capability detection
 * @returns {{ mode: string, budget: number, level: string }|null}
 */
function legacySuffixToConfig(legacySuffix, baseModel) {
  if (!legacySuffix) return null;

  if (legacySuffix.includes('-nothinking')) {
    return { mode: ThinkingMode.NONE, budget: 0, level: '' };
  }

  if (legacySuffix.includes('-maxthinking')) {
    // For Gemini 3: use level-based max
    if (baseModel.includes('gemini-3')) {
      return { mode: ThinkingMode.LEVEL, budget: 0, level: 'high' };
    }
    // For Flash models: use moderate budget
    if (baseModel.includes('flash')) {
      return { mode: ThinkingMode.BUDGET, budget: 24576, level: '' };
    }
    // For Pro models: use large budget
    return { mode: ThinkingMode.BUDGET, budget: 65536, level: '' };
  }

  return null;
}

// ============== Validation & Normalization ==============

/**
 * Validate and normalize a ThinkingConfig against model capabilities.
 *
 * Aligned with CLIProxyAPI's ValidateConfig:
 * - Auto-converts between Budget and Level based on model capability
 * - Clamps budget values to model's allowed range
 * - Handles Auto mode fallback when dynamic not allowed
 *
 * @param {{ mode: string, budget: number, level: string }} config
 * @param {object|null} support - ThinkingSupport from model capabilities
 * @returns {{ mode: string, budget: number, level: string }}
 */
function validateConfig(config, support) {
  if (!config) return null;
  if (!support) {
    // Model doesn't support thinking
    if (config.mode !== ThinkingMode.NONE) {
      logger.debug('Model does not support thinking, stripping config');
      return null;
    }
    return config;
  }

  // Detect model capability
  const hasBudget = support.min > 0 || support.max > 0;
  const hasLevels = support.levels && support.levels.length > 0;

  let result = { ...config };

  // Auto-convert Budget ↔ Level based on model capability
  if (!hasBudget && hasLevels && result.mode === ThinkingMode.BUDGET) {
    // Level-only model but received budget → convert to level
    const level = budgetToLevel(result.budget);
    if (level === 'none') {
      result.mode = ThinkingMode.NONE;
      result.budget = 0;
      result.level = '';
    } else if (level === 'auto') {
      result.mode = ThinkingMode.AUTO;
      result.budget = -1;
      result.level = '';
    } else {
      result.mode = ThinkingMode.LEVEL;
      result.level = clampLevel(level, support.levels);
      result.budget = 0;
    }
    logger.debug(`Converted budget ${config.budget} to level ${result.level}`);
  }

  if (hasBudget && !hasLevels && result.mode === ThinkingMode.LEVEL) {
    // Budget-only model but received level → convert to budget
    const budget = LEVEL_TO_BUDGET[result.level.toLowerCase()];
    if (budget !== undefined) {
      if (budget === 0) {
        result.mode = ThinkingMode.NONE;
        result.budget = 0;
      } else if (budget === -1) {
        result.mode = ThinkingMode.AUTO;
        result.budget = -1;
      } else {
        result.mode = ThinkingMode.BUDGET;
        result.budget = budget;
      }
      result.level = '';
      logger.debug(`Converted level ${config.level} to budget ${result.budget}`);
    }
  }

  // Normalize special modes
  if (result.mode === ThinkingMode.LEVEL && result.level === 'none') {
    result.mode = ThinkingMode.NONE;
    result.budget = 0;
    result.level = '';
  }
  if (result.mode === ThinkingMode.LEVEL && result.level === 'auto') {
    result.mode = ThinkingMode.AUTO;
    result.budget = -1;
    result.level = '';
  }
  if (result.mode === ThinkingMode.BUDGET && result.budget === 0) {
    result.mode = ThinkingMode.NONE;
    result.level = '';
  }

  // Validate level against supported levels
  if (hasLevels && result.mode === ThinkingMode.LEVEL) {
    if (!support.levels.includes(result.level.toLowerCase())) {
      result.level = clampLevel(result.level, support.levels);
    }
  }

  // Clamp budget to model's range
  if (result.mode === ThinkingMode.BUDGET && hasBudget) {
    result.budget = clampBudget(result.budget, support);
  }

  // Handle Auto when dynamic not allowed
  if (result.mode === ThinkingMode.AUTO && !support.dynamicAllowed) {
    if (hasLevels) {
      result.mode = ThinkingMode.LEVEL;
      result.level = 'medium';
      result.budget = 0;
      logger.debug('Auto mode not supported, using medium level');
    } else if (hasBudget) {
      result.mode = ThinkingMode.BUDGET;
      result.budget = Math.floor((support.min + support.max) / 2);
      logger.debug(`Auto mode not supported, using mid-range budget ${result.budget}`);
    }
  }

  return result;
}

/**
 * Clamp a level to the nearest supported level.
 */
function clampLevel(level, supported) {
  const standardOrder = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
  const lower = level.toLowerCase();
  if (supported.map(s => s.toLowerCase()).includes(lower)) return lower;

  const pos = standardOrder.indexOf(lower);
  if (pos === -1) return supported[Math.floor(supported.length / 2)] || 'medium';

  let bestLevel = supported[0];
  let bestDist = Infinity;
  for (const s of supported) {
    const idx = standardOrder.indexOf(s.toLowerCase());
    if (idx !== -1) {
      const dist = Math.abs(pos - idx);
      if (dist < bestDist || (dist === bestDist && idx < standardOrder.indexOf(bestLevel))) {
        bestLevel = s.toLowerCase();
        bestDist = dist;
      }
    }
  }
  logger.debug(`Level "${level}" clamped to "${bestLevel}"`);
  return bestLevel;
}

/**
 * Clamp budget to model's allowed range.
 */
function clampBudget(budget, support) {
  if (budget === -1) return budget; // Auto passes through
  if (budget === 0 && !support.zeroAllowed) {
    logger.debug(`Budget 0 not allowed, clamping to min ${support.min}`);
    return support.min;
  }
  if (support.min === 0 && support.max === 0) return budget; // Level-only model
  if (budget < support.min) {
    if (budget === 0 && support.zeroAllowed) return 0;
    logger.debug(`Budget ${budget} below min ${support.min}, clamping`);
    return support.min;
  }
  if (budget > support.max) {
    logger.debug(`Budget ${budget} above max ${support.max}, clamping`);
    return support.max;
  }
  return budget;
}

// ============== Main Apply Entry Point ==============

/**
 * Apply thinking configuration to a Gemini request.
 *
 * This is the unified entry point, aligned with CLIProxyAPI's ApplyThinking.
 * Processing order: suffix extraction → config parsing → validation → Gemini format output
 *
 * @param {string} model - Full model name (may include suffix)
 * @param {string} [reasoningEffort] - OpenAI reasoning_effort parameter from body
 * @param {object} [matrixConfig] - Matrix configuration for model capability lookup
 * @returns {{ thinkingConfig: object|null, baseModel: string, searchMode: boolean }}
 */
function applyThinking(model, reasoningEffort, matrixConfig) {
  // 1. Strip prefixes
  const prefixes = ['假流/', '流抗/'];
  let stripped = model;
  for (const prefix of prefixes) {
    if (stripped.startsWith(prefix)) {
      stripped = stripped.substring(prefix.length);
      break;
    }
  }

  // 2. Check and strip -search suffix
  let searchMode = false;
  if (stripped.endsWith('-search')) {
    searchMode = true;
    stripped = stripped.substring(0, stripped.length - '-search'.length);
  }

  // 3. Parse suffix (bracket or legacy)
  const suffixResult = parseSuffix(stripped);
  const baseModel = suffixResult.modelName;

  // 4. Get model's ThinkingSupport capabilities
  const support = getThinkingSupport(baseModel, matrixConfig);

  // 5. Extract thinking config: suffix priority over body
  let config = null;

  if (suffixResult.hasSuffix) {
    if (suffixResult.legacySuffix) {
      config = legacySuffixToConfig(suffixResult.legacySuffix, baseModel);
    } else {
      config = parseSuffixToConfig(suffixResult.rawSuffix);
    }
    if (config) {
      logger.debug(`Thinking config from suffix: mode=${config.mode}, budget=${config.budget}, level=${config.level}`);
    }
  } else if (reasoningEffort) {
    // Extract from reasoning_effort body parameter
    const effort = String(reasoningEffort).toLowerCase().trim();
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
  }

  // 6. No config found → passthrough (let backend use defaults)
  if (!config) {
    return { thinkingConfig: null, baseModel, searchMode };
  }

  // 7. Validate and normalize
  const validated = validateConfig(config, support);
  if (!validated) {
    return { thinkingConfig: null, baseModel, searchMode };
  }

  // 8. Convert to Gemini thinkingConfig format
  const geminiConfig = toGeminiFormat(validated);
  return { thinkingConfig: geminiConfig, baseModel, searchMode };
}

/**
 * Convert validated ThinkingConfig to Gemini API format.
 *
 * @param {{ mode: string, budget: number, level: string }} config
 * @returns {object} Gemini thinkingConfig object
 */
function toGeminiFormat(config) {
  switch (config.mode) {
    case ThinkingMode.NONE:
      // Return null to omit thinkingConfig entirely instead of sending budget: 0
      // which causes 400 errors from Google APIs.
      return null;

    case ThinkingMode.AUTO:
      return { thinkingBudget: -1, includeThoughts: true };

    case ThinkingMode.LEVEL:
      return { thinkingLevel: config.level.toUpperCase(), includeThoughts: true };

    case ThinkingMode.BUDGET:
      return { thinkingBudget: config.budget, includeThoughts: true };

    default:
      return null;
  }
}

// ============== Exports ==============

module.exports = {
  ThinkingMode,
  LEVEL_TO_BUDGET,
  STATIC_THINKING_SUPPORT,
  parseSuffix,
  parseSuffixToConfig,
  legacySuffixToConfig,
  validateConfig,
  getThinkingSupport,
  applyThinking,
  toGeminiFormat,
  clampBudget,
  clampLevel,
  budgetToLevel,
};
