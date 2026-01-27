/**
 * 输入验证中间件
 * 使用 Zod 进行请求数据验证
 */

const { z } = require('zod');
const { BadRequestError, ValidationError } = require('./errorHandler');

/**
 * 创建验证中间件
 * @param {Object} schemas - 验证 schema 对象
 * @param {z.ZodSchema} schemas.body - body 验证 schema
 * @param {z.ZodSchema} schemas.query - query 验证 schema
 * @param {z.ZodSchema} schemas.params - params 验证 schema
 * @returns {Function} Express 中间件
 */
function validate(schemas) {
    return async (req, res, next) => {
        try {
            const errors = [];

            // 验证 body
            if (schemas.body) {
                const result = schemas.body.safeParse(req.body);
                if (!result.success) {
                    const issues = result.error?.errors || [];
                    if (issues.length === 0) {
                        console.error('Validation failed but no errors found:', result.error);
                    }
                    errors.push(
                        ...issues.map((e) => ({
                            field: `body.${e.path.join('.')}`,
                            message: e.message,
                            code: e.code,
                        }))
                    );
                } else {
                    req.body = result.data;
                }
            }

            // 验证 query
            if (schemas.query) {
                const result = schemas.query.safeParse(req.query);
                if (!result.success) {
                    const issues = result.error?.errors || [];
                    errors.push(
                        ...issues.map((e) => ({
                            field: `query.${e.path.join('.')}`,
                            message: e.message,
                            code: e.code,
                        }))
                    );
                } else {
                    req.query = result.data;
                }
            }

            // 验证 params
            if (schemas.params) {
                const result = schemas.params.safeParse(req.params);
                if (!result.success) {
                    const issues = result.error?.errors || [];
                    errors.push(
                        ...issues.map((e) => ({
                            field: `params.${e.path.join('.')}`,
                            message: e.message,
                            code: e.code,
                        }))
                    );
                } else {
                    req.params = result.data;
                }
            }

            if (errors.length > 0) {
                throw new ValidationError('请求参数验证失败', errors);
            }

            next();
        } catch (error) {
            next(error);
        }
    };
}

// ==================== 通用 Schema ====================

/**
 * 分页参数 Schema
 */
const paginationSchema = z.object({
    page: z.coerce.number().int().min(1).default(1),
    pageSize: z.coerce.number().int().min(1).max(100).default(20),
    sort: z.string().optional(),
    order: z.enum(['asc', 'desc']).optional().default('desc'),
});

/**
 * ID 参数 Schema
 */
const idParamSchema = z.object({
    id: z.string().min(1, 'ID 不能为空'),
});

/**
 * UUID 参数 Schema
 */
const uuidParamSchema = z.object({
    id: z.string().uuid('无效的 UUID 格式'),
});

// ==================== 认证相关 Schema ====================

/**
 * 登录请求 Schema
 */
const loginSchema = z.object({
    password: z.string().min(1, '密码不能为空'),
    totpToken: z.string().length(6).optional(),
});

/**
 * 修改密码 Schema
 */
const changePasswordSchema = z.object({
    oldPassword: z.string().min(1, '旧密码不能为空'),
    newPassword: z.string().min(6, '新密码最少 6 个字符').max(128, '密码过长'),
});

// ==================== 服务器管理 Schema ====================

/**
 * 创建服务器 Schema
 */
const createServerSchema = z.object({
    name: z
        .string()
        .min(1, '名称不能为空')
        .max(100, '名称过长')
        .regex(/^[a-zA-Z0-9\u4e00-\u9fa5_-]+$/, '名称包含非法字符'),
    host: z
        .string()
        .min(1, 'IP/域名不能为空')
        .refine(
            (val) => {
                // 简单的 IP 或域名验证
                const ipRegex = /^(\d{1,3}\.){3}\d{1,3}$/;
                const domainRegex = /^[a-zA-Z0-9][-a-zA-Z0-9]*(\.[a-zA-Z0-9][-a-zA-Z0-9]*)*$/;
                return ipRegex.test(val) || domainRegex.test(val);
            },
            { message: '无效的 IP 地址或域名' }
        ),
    port: z.coerce.number().int().min(1).max(65535).default(22),
    username: z.string().min(1, '用户名不能为空').max(64),
    auth_type: z.enum(['password', 'key']).default('password'),
    password: z.string().optional(),
    private_key: z.string().optional(),
    passphrase: z.string().optional(),
    tags: z.array(z.string()).optional(),
    description: z.string().max(500).optional(),
});

/**
 * 更新服务器 Schema
 */
const updateServerSchema = createServerSchema.partial();

// ==================== API 账号 Schema ====================

/**
 * 创建账号 Schema (通用)
 */
const createAccountSchema = z.object({
    name: z.string().min(1, '名称不能为空').max(100),
    api_token: z.string().min(1, 'API Token 不能为空'),
    enable: z.boolean().optional().default(true),
});

/**
 * 批量导入账号 Schema
 */
const batchImportSchema = z.object({
    accounts: z
        .string()
        .min(1, '账号数据不能为空')
        .refine(
            (val) => {
                // 每行格式: name:token 或 name,token
                const lines = val.trim().split('\n');
                return lines.every((line) => {
                    const parts = line.split(/[,:]/);
                    return parts.length >= 2 && parts[0].trim() && parts[1].trim();
                });
            },
            { message: '格式错误，每行应为 "名称:Token" 或 "名称,Token"' }
        ),
});

// ==================== 音乐模块 Schema ====================

/**
 * 搜索歌曲 Schema
 */
const searchMusicSchema = z.object({
    keywords: z.string().min(1, '关键词不能为空').max(200),
    limit: z.coerce.number().int().min(1).max(100).default(30),
    offset: z.coerce.number().int().min(0).default(0),
    type: z.coerce.number().int().min(1).max(1018).optional(),
});

/**
 * 获取歌曲 URL Schema
 */
const getSongUrlSchema = z.object({
    id: z.coerce.number().int().positive('歌曲 ID 必须为正整数'),
    br: z.coerce.number().int().optional(),
    unblock: z.enum(['true', 'false']).optional(),
});

// ==================== OpenAI 代理 Schema ====================

/**
 * Chat Completion Schema
 */
const chatCompletionSchema = z.object({
    model: z.string().min(1, '模型不能为空'),
    messages: z
        .array(
            z.object({
                role: z.enum(['system', 'user', 'assistant', 'tool']),
                content: z.union([z.string(), z.array(z.any())]),
                name: z.string().optional(),
                tool_calls: z.array(z.any()).optional(),
                tool_call_id: z.string().optional(),
            })
        )
        .min(1, '消息列表不能为空'),
    temperature: z.number().min(0).max(2).optional(),
    max_tokens: z.number().int().positive().optional(),
    top_p: z.number().min(0).max(1).optional(),
    stream: z.boolean().optional(),
    tools: z.array(z.any()).optional(),
});

// ==================== 导出 ====================

module.exports = {
    // 验证中间件
    validate,

    // 通用 Schema
    paginationSchema,
    idParamSchema,
    uuidParamSchema,

    // 认证 Schema
    loginSchema,
    changePasswordSchema,

    // 服务器 Schema
    createServerSchema,
    updateServerSchema,

    // 账号 Schema
    createAccountSchema,
    batchImportSchema,

    // 音乐 Schema
    searchMusicSchema,
    getSongUrlSchema,

    // OpenAI Schema
    chatCompletionSchema,

    // 导出 z 以便自定义 schema
    z,
};
