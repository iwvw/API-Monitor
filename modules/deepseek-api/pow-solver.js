/**
 * DeepSeek PoW (Proof of Work) WASM 求解器
 * 移植自 ds2api/internal/deepseek/pow.go
 *
 * 使用 Node.js 内置 WebAssembly API 加载 DeepSeek 的 SHA3 WASM 模块
 */

const fs = require('fs');
const path = require('path');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('DS-PoW');

class PowSolver {
    constructor() {
        this.instance = null;
        this.memory = null;
        this.initialized = false;
    }

    /**
     * 初始化 WASM 模块
     */
    async init() {
        if (this.initialized) return;

        const wasmPath = path.join(__dirname, 'sha3_wasm_bg.wasm');
        if (!fs.existsSync(wasmPath)) {
            throw new Error(`WASM file not found: ${wasmPath}`);
        }

        const wasmBuffer = fs.readFileSync(wasmPath);
        const wasmModule = await WebAssembly.compile(wasmBuffer);
        this.instance = await WebAssembly.instantiate(wasmModule, {
            // DeepSeek WASM 不需要外部 imports (wbg 接口都是内部的)
            wbg: {
                // 占位，某些版本的 wasm 可能需要
                __wbindgen_throw: (ptr, len) => {
                    const buf = Buffer.from(this.memory.buffer, ptr, len);
                    throw new Error(buf.toString());
                },
            },
        });

        this.memory = this.instance.exports.memory;
        this.initialized = true;
        logger.info('WASM 模块已加载');
    }

    /**
     * 计算 PoW 答案
     * @param {Object} challenge - DeepSeek 返回的 PoW 挑战
     * @returns {number} 答案
     */
    async compute(challenge) {
        await this.init();

        const algorithm = challenge.algorithm;
        if (algorithm !== 'DeepSeekHashV1') {
            throw new Error(`Unsupported algorithm: ${algorithm}`);
        }

        const challengeStr = challenge.challenge || '';
        const salt = challenge.salt || '';
        const difficulty = Number(challenge.difficulty) || 144000;
        const expireAt = Number(challenge.expire_at) || 1680000000;
        const prefix = `${salt}_${expireAt}_`;

        const exports = this.instance.exports;

        // 获取 stack pointer 操作函数
        const stackFn = exports.__wbindgen_add_to_stack_pointer;
        const allocFn = exports.__wbindgen_export_0;
        const solveFn = exports.wasm_solve;
        const freeFn = exports.__wbindgen_export_2;

        if (!stackFn || !allocFn || !solveFn) {
            throw new Error('Required WASM exports missing');
        }

        // 分配返回值空间 (stack alloc -16)
        const retptr = stackFn(-16);

        try {
            // 写入 challenge 字符串
            const [chPtr, chLen] = this._writeUTF8(allocFn, challengeStr);
            // 写入 prefix 字符串
            const [prefixPtr, prefixLen] = this._writeUTF8(allocFn, prefix);

            try {
                // 调用 wasm_solve
                solveFn(retptr, chPtr, chLen, prefixPtr, prefixLen, difficulty);

                // 读取结果
                const mem = new DataView(this.memory.buffer);
                const status = mem.getInt32(retptr, true);
                const value = mem.getFloat64(retptr + 8, true);

                if (status === 0) {
                    throw new Error('PoW solve failed');
                }

                return Math.floor(value);
            } finally {
                // 释放字符串内存
                if (freeFn) {
                    try { freeFn(chPtr, chLen, 1); } catch (_) { }
                    try { freeFn(prefixPtr, prefixLen, 1); } catch (_) { }
                }
            }
        } finally {
            // 恢复 stack pointer
            stackFn(16);
        }
    }

    /**
     * 写入 UTF-8 字符串到 WASM 内存
     */
    _writeUTF8(allocFn, text) {
        const encoder = new TextEncoder();
        const data = encoder.encode(text);
        const ptr = allocFn(data.length, 1);
        const mem = new Uint8Array(this.memory.buffer);
        mem.set(data, ptr);
        return [ptr, data.length];
    }

    /**
     * 构建 PoW Header (Base64 编码的 JSON)
     */
    static buildPowHeader(challenge, answer) {
        const payload = {
            algorithm: challenge.algorithm,
            challenge: challenge.challenge,
            salt: challenge.salt,
            answer: answer,
            signature: challenge.signature,
            target_path: challenge.target_path,
        };
        return Buffer.from(JSON.stringify(payload)).toString('base64');
    }
}

// 单例
let solverInstance = null;

function getSolver() {
    if (!solverInstance) {
        solverInstance = new PowSolver();
    }
    return solverInstance;
}

module.exports = { PowSolver, getSolver };
