const { spawn } = require('child_process');
const os = require('os');
const path = require('path');
const fs = require('fs');

class AntigravityRequester {
    constructor(options = {}) {
        this.binPath = options.binPath;
        this.executablePath = options.executablePath || this._getExecutablePath();
        this.proc = null;
        this.requestId = 0;
        this.pendingRequests = new Map();
        this.buffer = '';
        this.writeQueue = Promise.resolve();
    }

    _getExecutablePath() {
        const platform = os.platform();
        const arch = os.arch();

        let filename;
        if (platform === 'win32') {
            filename = 'antigravity_requester_windows_amd64.exe';
        } else if (platform === 'android') {
            filename = 'antigravity_requester_android_arm64';
        } else if (platform === 'linux') {
            if (arch === 'arm64') {
                filename = 'antigravity_requester_linux_arm64';
            } else {
                filename = 'antigravity_requester_linux_amd64';
            }
        } else {
            throw new Error(`Unsupported platform: ${platform}`);
        }

        const binPath = this.binPath || path.join(__dirname, 'bin');
        const requester_execPath = path.join(binPath, filename);

        // 设置执行权限（非Windows平台）
        if (platform !== 'win32') {
            try {
                if (fs.existsSync(requester_execPath)) {
                    fs.chmodSync(requester_execPath, 0o755);
                }
            } catch (error) {
                console.warn(`Could not set executable permissions: ${error.message}`);
            }
        }
        return requester_execPath;
    }

    _ensureProcess() {
        if (this.proc) return;

        if (!fs.existsSync(this.executablePath)) {
            throw new Error(`Executable not found: ${this.executablePath}`);
        }

        this.proc = spawn(this.executablePath, [], {
            stdio: ['pipe', 'pipe', 'pipe']
        });

        if (this.proc.stdin.setDefaultEncoding) {
            this.proc.stdin.setDefaultEncoding('utf8');
        }

        if (this.proc.stdout.setEncoding) {
            this.proc.stdout.setEncoding('utf8');
        }

        this.proc.stdout.on('data', (data) => {
            this.buffer += data.toString();

            setImmediate(() => {
                const lines = this.buffer.split('\n');
                this.buffer = lines.pop();

                for (const line of lines) {
                    if (!line.trim()) continue;
                    try {
                        const response = JSON.parse(line);
                        const pending = this.pendingRequests.get(response.id);
                        if (!pending) continue;

                        if (pending.streamResponse) {
                            pending.streamResponse._handleChunk(response);
                            if (response.type === 'end' || response.type === 'error') {
                                this.pendingRequests.delete(response.id);
                            }
                        } else {
                            this.pendingRequests.delete(response.id);
                            if (response.ok) {
                                pending.resolve(new AntigravityResponse(response));
                            } else {
                                pending.reject(new Error(response.error || 'Request failed'));
                            }
                        }
                    } catch (e) {
                        console.error('Failed to parse response:', e, 'Line:', line);
                    }
                }
            });
        });

        this.proc.stderr.on('data', (data) => {
            console.error('AntigravityRequester stderr:', data.toString());
        });

        this.proc.on('close', () => {
            this.proc = null;
            for (const [id, pending] of this.pendingRequests) {
                if (pending.reject) {
                    pending.reject(new Error('Process closed'));
                } else if (pending.streamResponse && pending.streamResponse._onError) {
                    pending.streamResponse._onError(new Error('Process closed'));
                }
            }
            this.pendingRequests.clear();
        });
    }

    async antigravity_fetch(url, options = {}) {
        this._ensureProcess();

        const id = `req-${++this.requestId}`;
        const request = {
            id,
            url,
            method: options.method || 'GET',
            headers: options.headers,
            body: options.body,
            timeout_ms: options.timeout || 30000,
            proxy: options.proxy,
            response_format: 'text',
            ...options
        };

        return new Promise((resolve, reject) => {
            this.pendingRequests.set(id, { resolve, reject });
            this._writeRequest(request);
        });
    }

    antigravity_fetchStream(url, options = {}) {
        this._ensureProcess();

        const id = `req-${++this.requestId}`;
        const request = {
            id,
            url,
            method: options.method || 'GET',
            headers: options.headers,
            body: options.body,
            timeout_ms: options.timeout || 30000,
            proxy: options.proxy,
            stream: true,
            ...options
        };

        const streamResponse = new StreamResponse(id);
        this.pendingRequests.set(id, { streamResponse });
        this._writeRequest(request);

        return streamResponse;
    }

    _writeRequest(request) {
        this.writeQueue = this.writeQueue.then(() => {
            return new Promise((resolve, reject) => {
                if (!this.proc || !this.proc.stdin) {
                    return reject(new Error('Process or stdin not available'));
                }
                const data = JSON.stringify(request) + '\n';
                const canWrite = this.proc.stdin.write(data);
                if (canWrite) {
                    resolve();
                } else {
                    this.proc.stdin.once('drain', resolve);
                    this.proc.stdin.once('error', reject);
                }
            });
        }).catch(err => {
            console.error('Write request failed:', err);
        });
    }

    close() {
        if (this.proc) {
            this.proc.stdin.end();
            this.proc = null;
        }
    }
}

class StreamResponse {
    constructor(id) {
        this.id = id;
        this.status = null;
        this.statusText = null;
        this.headers = null;
        this.chunks = [];
        this._onStart = null;
        this._onData = null;
        this._onEnd = null;
        this._onError = null;
        this._ended = false;
        this._error = null;
        this._textPromiseResolve = null;
        this._textPromiseReject = null;
    }

    _handleChunk(chunk) {
        if (chunk.type === 'start') {
            this.status = chunk.status;
            this.headers = new Map(Object.entries(chunk.headers || {}));
            if (this._onStart) this._onStart({ status: chunk.status, headers: this.headers });
        } else if (chunk.type === 'data') {
            const data = chunk.encoding === 'base64'
                ? Buffer.from(chunk.data, 'base64').toString('utf8')
                : chunk.data;
            this.chunks.push(data);
            if (this._onData) this._onData(data);
        } else if (chunk.type === 'end') {
            this._ended = true;
            if (this._textPromiseResolve) this._textPromiseResolve(this.chunks.join(''));
            if (this._onEnd) this._onEnd();
        } else if (chunk.type === 'error') {
            this._ended = true;
            this._error = new Error(chunk.error);
            if (this._textPromiseReject) this._textPromiseReject(this._error);
            if (this._onError) this._onError(this._error);
        }
    }

    onStart(callback) {
        this._onStart = callback;
        return this;
    }

    onData(callback) {
        this._onData = callback;
        return this;
    }

    onEnd(callback) {
        this._onEnd = callback;
        return this;
    }

    onError(callback) {
        this._onError = callback;
        return this;
    }

    async text() {
        if (this._ended) {
            if (this._error) throw this._error;
            return this.chunks.join('');
        }
        return new Promise((resolve, reject) => {
            this._textPromiseResolve = resolve;
            this._textPromiseReject = reject;
        });
    }
}

class AntigravityResponse {
    constructor(response) {
        this._response = response;
        this.ok = response.ok;
        this.status = response.status;
        this.statusText = response.status_text;
        this.url = response.url;
        this.headers = new Map(Object.entries(response.headers || {}));
        this.redirected = response.redirected;
    }

    async text() {
        if (this._response.body_encoding === 'base64') {
            return Buffer.from(this._response.body, 'base64').toString('utf8');
        }
        return this._response.body;
    }

    async json() {
        const text = await this.text();
        return JSON.parse(text);
    }

    async buffer() {
        if (this._response.body_encoding === 'base64') {
            return Buffer.from(this._response.body, 'base64');
        }
        return Buffer.from(this._response.body, 'utf8');
    }
}

module.exports = AntigravityRequester;
