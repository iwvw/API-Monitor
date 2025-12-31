/**
 * 输入验证中间件测试
 * @module test/unit/middleware/validation.test
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock errorHandler
vi.mock('../../../src/middleware/errorHandler', () => ({
    BadRequestError: class BadRequestError extends Error {
        constructor(message) {
            super(message);
            this.statusCode = 400;
            this.code = 'BAD_REQUEST';
        }
    },
    ValidationError: class ValidationError extends Error {
        constructor(message, errors) {
            super(message);
            this.statusCode = 422;
            this.code = 'VALIDATION_ERROR';
            this.errors = errors;
        }
    },
}));

import {
    validate,
    z,
    paginationSchema,
    idParamSchema,
    loginSchema,
    createServerSchema,
    searchMusicSchema,
} from '../../../src/middleware/validation.js';

// 创建模拟的 req/res/next
function createMockReq(overrides = {}) {
    return {
        body: {},
        query: {},
        params: {},
        ...overrides,
    };
}

function createMockRes() {
    return {
        status: vi.fn().mockReturnThis(),
        json: vi.fn().mockReturnThis(),
    };
}

describe('输入验证中间件', () => {
    describe('validate 函数', () => {
        it('应该返回一个中间件函数', () => {
            const middleware = validate({});
            expect(typeof middleware).toBe('function');
        });

        it('没有 schema 时应该直接通过', async () => {
            const middleware = validate({});
            const req = createMockReq();
            const res = createMockRes();
            const next = vi.fn();

            await middleware(req, res, next);

            expect(next).toHaveBeenCalled();
            expect(next).toHaveBeenCalledWith(); // 没有错误
        });

        it('验证通过时应该调用 next', async () => {
            const schema = z.object({ name: z.string() });
            const middleware = validate({ body: schema });
            const req = createMockReq({ body: { name: 'test' } });
            const next = vi.fn();

            await middleware(req, createMockRes(), next);

            expect(next).toHaveBeenCalled();
        });

        it('验证失败时应该调用 next(error)', async () => {
            const schema = z.object({ name: z.string().min(5) });
            const middleware = validate({ body: schema });
            const req = createMockReq({ body: { name: 'ab' } });
            const next = vi.fn();

            await middleware(req, createMockRes(), next);

            expect(next).toHaveBeenCalled();
            // 验证 next 被调用时传入了一个 Error 参数
            const callArg = next.mock.calls[0][0];
            expect(callArg).toBeInstanceOf(Error);
        });

        it('应该验证 query 参数', async () => {
            const schema = z.object({ page: z.coerce.number().min(1) });
            const middleware = validate({ query: schema });
            const req = createMockReq({ query: { page: '2' } });
            const next = vi.fn();

            await middleware(req, createMockRes(), next);

            expect(next).toHaveBeenCalledWith();
            expect(req.query.page).toBe(2); // 应该被转换为数字
        });

        it('应该验证 params 参数', async () => {
            const schema = z.object({ id: z.string().uuid() });
            const middleware = validate({ params: schema });
            const req = createMockReq({ params: { id: 'invalid-uuid' } });
            const next = vi.fn();

            await middleware(req, createMockRes(), next);

            expect(next).toHaveBeenCalled();
            const error = next.mock.calls[0][0];
            expect(error).toBeDefined();
        });
    });

    describe('paginationSchema', () => {
        it('应该有默认值', () => {
            const result = paginationSchema.parse({});

            expect(result.page).toBe(1);
            expect(result.pageSize).toBe(20);
            expect(result.order).toBe('desc');
        });

        it('应该转换字符串为数字', () => {
            const result = paginationSchema.parse({ page: '5', pageSize: '50' });

            expect(result.page).toBe(5);
            expect(result.pageSize).toBe(50);
        });

        it('应该拒绝无效的页码', () => {
            expect(() => paginationSchema.parse({ page: 0 })).toThrow();
            expect(() => paginationSchema.parse({ page: -1 })).toThrow();
        });

        it('应该限制 pageSize 最大值', () => {
            expect(() => paginationSchema.parse({ pageSize: 101 })).toThrow();
        });
    });

    describe('idParamSchema', () => {
        it('应该接受有效的 ID', () => {
            const result = idParamSchema.parse({ id: 'test-id-123' });
            expect(result.id).toBe('test-id-123');
        });

        it('应该拒绝空 ID', () => {
            expect(() => idParamSchema.parse({ id: '' })).toThrow();
        });
    });

    describe('loginSchema', () => {
        it('应该接受有效的密码', () => {
            const result = loginSchema.parse({ password: 'mypassword' });
            expect(result.password).toBe('mypassword');
        });

        it('应该拒绝空密码', () => {
            expect(() => loginSchema.parse({ password: '' })).toThrow();
        });
    });

    describe('createServerSchema', () => {
        it('应该接受有效的服务器配置', () => {
            const result = createServerSchema.parse({
                name: 'my-server',
                host: '192.168.1.100',
                username: 'root',
            });

            expect(result.name).toBe('my-server');
            expect(result.host).toBe('192.168.1.100');
            expect(result.port).toBe(22); // 默认值
            expect(result.auth_type).toBe('password'); // 默认值
        });

        it('应该接受域名', () => {
            const result = createServerSchema.parse({
                name: 'my-server',
                host: 'example.com',
                username: 'admin',
            });

            expect(result.host).toBe('example.com');
        });

        it('应该拒绝无效的主机名', () => {
            expect(() =>
                createServerSchema.parse({
                    name: 'test',
                    host: 'invalid host with spaces!', // 包含空格和特殊字符
                    username: 'root',
                })
            ).toThrow();
        });

        it('应该拒绝无效的端口', () => {
            expect(() =>
                createServerSchema.parse({
                    name: 'test',
                    host: '192.168.1.1',
                    port: 70000,
                    username: 'root',
                })
            ).toThrow();
        });
    });

    describe('searchMusicSchema', () => {
        it('应该接受有效的搜索参数', () => {
            const result = searchMusicSchema.parse({ keywords: '周杰伦' });

            expect(result.keywords).toBe('周杰伦');
            expect(result.limit).toBe(30); // 默认值
            expect(result.offset).toBe(0); // 默认值
        });

        it('应该拒绝空关键词', () => {
            expect(() => searchMusicSchema.parse({ keywords: '' })).toThrow();
        });

        it('应该限制关键词长度', () => {
            expect(() =>
                searchMusicSchema.parse({ keywords: 'a'.repeat(201) })
            ).toThrow();
        });
    });
});

describe('Zod 基础功能', () => {
    it('z 应该被正确导出', () => {
        expect(z).toBeDefined();
        expect(typeof z.string).toBe('function');
        expect(typeof z.number).toBe('function');
        expect(typeof z.object).toBe('function');
    });

    it('应该能创建自定义 schema', () => {
        const customSchema = z.object({
            email: z.string().email(),
            age: z.number().int().positive(),
        });

        const result = customSchema.safeParse({
            email: 'test@example.com',
            age: 25,
        });

        expect(result.success).toBe(true);
    });
});
