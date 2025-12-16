#!/usr/bin/env node

/**
 * OpenAI API 管理模块测试脚本
 */

const http = require('http');

const BASE_URL = 'http://localhost:3000';
const PASSWORD = 'admin123'; // 默认密码，需要匹配服务器设置

// 发送HTTP请求
function makeRequest(method, path, data = null) {
  return new Promise((resolve, reject) => {
    const url = new URL(BASE_URL + path);
    const options = {
      hostname: url.hostname,
      port: url.port,
      path: url.pathname + url.search,
      method: method,
      headers: {
        'Content-Type': 'application/json',
        'x-admin-password': PASSWORD
      }
    };

    const req = http.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => body += chunk);
      res.on('end', () => {
        try {
          const data = JSON.parse(body);
          resolve({ status: res.statusCode, data });
        } catch (e) {
          resolve({ status: res.statusCode, data: body });
        }
      });
    });

    req.on('error', reject);
    if (data) req.write(JSON.stringify(data));
    req.end();
  });
}

// 测试用例
async function runTests() {
  console.log('=== OpenAI API 管理模块测试 ===\n');

  try {
    // 1. 获取所有 Keys
    console.log('1️⃣ 测试获取所有 API Keys...');
    let res = await makeRequest('GET', '/api/openai/keys');
    console.log(`   状态: ${res.status}`);
    console.log(`   结果: ${JSON.stringify(res.data)}\n`);

    // 2. 添加 API Key
    console.log('2️⃣ 测试添加 API Key...');
    res = await makeRequest('POST', '/api/openai/keys', {
      name: 'Test Key',
      key: 'sk-test1234567890'
    });
    console.log(`   状态: ${res.status}`);
    console.log(`   结果: ${JSON.stringify(res.data)}\n`);

    // 3. 获取所有 URLs
    console.log('3️⃣ 测试获取所有 Base URLs...');
    res = await makeRequest('GET', '/api/openai/urls');
    console.log(`   状态: ${res.status}`);
    console.log(`   结果: ${JSON.stringify(res.data)}\n`);

    // 4. 添加 Base URL
    console.log('4️⃣ 测试添加 Base URL...');
    res = await makeRequest('POST', '/api/openai/urls', {
      name: 'Official API',
      url: 'https://api.openai.com/v1'
    });
    console.log(`   状态: ${res.status}`);
    console.log(`   结果: ${JSON.stringify(res.data)}\n`);

    console.log('✅ 所有测试完成！');

  } catch (error) {
    console.error('❌ 测试错误:', error.message);
    process.exit(1);
  }
}

runTests();
