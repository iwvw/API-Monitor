/**
 * 敏感数据存储模块测试
 * @module test/unit/utils/secure-storage.test
 */

import { describe, it, expect, vi, beforeAll } from 'vitest';

// Mock logger
vi.mock('../../../src/utils/logger', () => ({
    createLogger: vi.fn(() => ({
        info: vi.fn(),
        warn: vi.fn(),
        error: vi.fn(),
        debug: vi.fn(),
    })),
}));

let secureStorage;

beforeAll(async () => {
    // 设置环境变量
    process.env.ENCRYPTION_KEY = 'test-encryption-key-for-secure-storage';
    secureStorage = await import('../../../src/utils/secure-storage.js');
});

describe('敏感数据存储模块', () => {
    describe('isEncrypted', () => {
        it('应该识别加密格式的字符串', () => {
            // 标准加密格式: iv:authTag:data (all hex)
            const encrypted = 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4:a1b2c3d4';
            expect(secureStorage.isEncrypted(encrypted)).toBe(true);
        });

        it('应该返回 false 对于非加密格式', () => {
            expect(secureStorage.isEncrypted('plain text')).toBe(false);
            expect(secureStorage.isEncrypted('only:two')).toBe(false);
            expect(secureStorage.isEncrypted('')).toBe(false);
            expect(secureStorage.isEncrypted(null)).toBe(false);
            expect(secureStorage.isEncrypted(undefined)).toBe(false);
        });
    });

    describe('secureEncrypt', () => {
        it('应该加密明文字符串', () => {
            const plaintext = 'my-secret-password';
            const encrypted = secureStorage.secureEncrypt(plaintext);

            expect(encrypted).not.toBe(plaintext);
            expect(secureStorage.isEncrypted(encrypted)).toBe(true);
        });

        it('不应重复加密已加密的字符串', () => {
            const plaintext = 'my-secret';
            const encrypted1 = secureStorage.secureEncrypt(plaintext);
            const encrypted2 = secureStorage.secureEncrypt(encrypted1);

            // 第二次加密应返回相同的值（不重复加密）
            expect(encrypted2).toBe(encrypted1);
        });

        it('应该返回空值不变', () => {
            expect(secureStorage.secureEncrypt('')).toBe('');
            expect(secureStorage.secureEncrypt(null)).toBe(null);
            expect(secureStorage.secureEncrypt(undefined)).toBe(undefined);
        });
    });

    describe('secureDecrypt', () => {
        it('应该解密加密的字符串', () => {
            const plaintext = 'my-secret-data';
            const encrypted = secureStorage.secureEncrypt(plaintext);
            const decrypted = secureStorage.secureDecrypt(encrypted);

            expect(decrypted).toBe(plaintext);
        });

        it('应该返回非加密格式的字符串不变', () => {
            const plaintext = 'not-encrypted';
            const result = secureStorage.secureDecrypt(plaintext);

            expect(result).toBe(plaintext);
        });

        it('应该返回空值不变', () => {
            expect(secureStorage.secureDecrypt('')).toBe('');
            expect(secureStorage.secureDecrypt(null)).toBe(null);
        });
    });

    describe('encryptFields', () => {
        it('应该加密指定的字段', () => {
            const obj = {
                name: 'test-account',
                password: 'secret123',
                token: 'my-token',
            };

            const result = secureStorage.encryptFields(obj, ['password', 'token']);

            expect(result.name).toBe('test-account'); // 未加密
            expect(result.password).not.toBe('secret123'); // 已加密
            expect(result.token).not.toBe('my-token'); // 已加密
            expect(secureStorage.isEncrypted(result.password)).toBe(true);
        });

        it('应该跳过不存在的字段', () => {
            const obj = { name: 'test' };
            const result = secureStorage.encryptFields(obj, ['password']);

            expect(result.name).toBe('test');
            expect(result.password).toBeUndefined();
        });
    });

    describe('decryptFields', () => {
        it('应该解密指定的字段', () => {
            const original = {
                name: 'test',
                password: 'secret123',
            };

            const encrypted = secureStorage.encryptFields(original, ['password']);
            const decrypted = secureStorage.decryptFields(encrypted, ['password']);

            expect(decrypted.password).toBe('secret123');
        });
    });

    describe('createSecureWrapper', () => {
        it('应该创建服务器安全包装器', () => {
            const wrapper = secureStorage.createSecureWrapper('server');

            expect(wrapper.encrypt).toBeDefined();
            expect(wrapper.decrypt).toBeDefined();
            expect(wrapper.encryptMany).toBeDefined();
            expect(wrapper.decryptMany).toBeDefined();
        });

        it('serverSecure 应该正确加密服务器敏感字段', () => {
            const server = {
                name: 'my-server',
                host: '192.168.1.1',
                password: 'root123',
                private_key: 'ssh-rsa AAAA...',
            };

            const encrypted = secureStorage.serverSecure.encrypt(server);

            expect(encrypted.name).toBe('my-server');
            expect(encrypted.host).toBe('192.168.1.1');
            expect(encrypted.password).not.toBe('root123');
            expect(encrypted.private_key).not.toBe('ssh-rsa AAAA...');
        });

        it('accountSecure 应该处理 API token', () => {
            const account = {
                name: 'test-account',
                api_token: 'sk-xxxx',
            };

            const encrypted = secureStorage.accountSecure.encrypt(account);
            expect(encrypted.api_token).not.toBe('sk-xxxx');

            const decrypted = secureStorage.accountSecure.decrypt(encrypted);
            expect(decrypted.api_token).toBe('sk-xxxx');
        });
    });

    describe('maskSensitive', () => {
        it('应该遮蔽中间部分', () => {
            const result = secureStorage.maskSensitive('password123456', 4, 4);
            expect(result).toMatch(/^pass\*+3456$/);
        });

        it('短字符串应该全部遮蔽', () => {
            const result = secureStorage.maskSensitive('abc', 4, 4);
            expect(result).toBe('***');
        });

        it('空值应该返回 ***', () => {
            expect(secureStorage.maskSensitive('')).toBe('***');
            expect(secureStorage.maskSensitive(null)).toBe('***');
        });
    });

    describe('removeSensitiveFields', () => {
        it('应该移除敏感字段', () => {
            const obj = {
                id: 1,
                name: 'test',
                password: 'secret',
                token: 'abc123',
            };

            const result = secureStorage.removeSensitiveFields(obj, ['password', 'token']);

            expect(result.id).toBe(1);
            expect(result.name).toBe('test');
            expect(result.password).toBeUndefined();
            expect(result.token).toBeUndefined();
        });
    });

    describe('maskSensitiveFields', () => {
        it('应该遮蔽敏感字段的值', () => {
            const obj = {
                name: 'test',
                password: 'verylongpassword',
            };

            const result = secureStorage.maskSensitiveFields(obj, ['password']);

            expect(result.name).toBe('test');
            expect(result.password).toContain('*');
            expect(result.password).not.toBe('verylongpassword');
        });
    });
});
