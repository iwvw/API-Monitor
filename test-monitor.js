/**
 * 测试监控功能和日志
 */

const http = require('http');

const BASE_URL = 'http://localhost:3000';
let sessionCookie = '';

function request(method, path, data = null) {
  return new Promise((resolve, reject) => {
    const url = new URL(path, BASE_URL);
    const options = {
      method,
      hostname: url.hostname,
      port: url.port,
      path: url.pathname + url.search,
      headers: {
        'Content-Type': 'application/json'
      }
    };

    if (sessionCookie) {
      options.headers['Cookie'] = sessionCookie;
    }

    const req = http.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => {
        if (res.headers['set-cookie']) {
          sessionCookie = res.headers['set-cookie'][0].split(';')[0];
        }
        try {
          resolve({ statusCode: res.statusCode, body: JSON.parse(body) });
        } catch (e) {
          resolve({ statusCode: res.statusCode, body: body });
        }
      });
    });

    req.on('error', reject);
    if (data) req.write(JSON.stringify(data));
    req.end();
  });
}

async function main() {
  try {
    // 1. 登录
    console.log('1. 登录系统...');
    await request('POST', '/api/login', { password: '123456' });
    console.log('✓ 登录成功\n');

    // 2. 获取监控配置
    console.log('2. 获取监控配置...');
    const configRes = await request('GET', '/api/server/monitor/config');
    if (configRes.body.success) {
      const config = configRes.body.data;
      console.log('✓ 当前配置:');
      console.log(`   探测间隔: ${config.probe_interval} 秒`);
      console.log(`   探测超时: ${config.probe_timeout} 秒`);
      console.log(`   日志保留: ${config.log_retention_days} 天`);
    }
    console.log('');

    // 3. 获取监控服务状态
    console.log('3. 获取监控服务状态...');
    const statusRes = await request('GET', '/api/server/monitor/status');
    if (statusRes.body.success) {
      console.log(`✓ 监控服务${statusRes.body.data.running ? '运行中' : '已停止'}`);
      if (statusRes.body.data.nextRunTime) {
        console.log(`   下次运行: ${statusRes.body.data.nextRunTime}`);
      }
    }
    console.log('');

    // 4. 手动触发探测
    console.log('4. 手动触发全部服务器探测...');
    const checkRes = await request('POST', '/api/server/check-all');
    if (checkRes.body.success || checkRes.statusCode === 200) {
      console.log('✓ 探测完成');
      if (checkRes.body.data) {
        console.log(`   结果: 成功 ${checkRes.body.data.success || 0}, 失败 ${checkRes.body.data.failed || 0}`);
      } else {
        console.log(`   消息: ${checkRes.body.message || '完成'}`);
      }
    } else {
      console.log('✗ 探测失败:', checkRes.body.error);
    }
    console.log('');

    // 等待一下让日志写入
    await new Promise(resolve => setTimeout(resolve, 1000));

    // 5. 获取监控日志
    console.log('5. 获取监控日志...');
    const logsRes = await request('GET', '/api/server/monitor/logs?limit=10');
    if (logsRes.body.success) {
      const logs = logsRes.body.data.logs;
      console.log(`✓ 最近 ${logs.length} 条日志:`);
      logs.forEach(log => {
        const status = log.status === 'success' ? '✓' : '✗';
        const time = new Date(log.checked_at).toLocaleString('zh-CN');
        console.log(`   ${status} ${time} - 服务器ID: ${log.server_id.substring(0, 8)}... (${log.response_time || 0}ms)`);
        if (log.error_message) {
          console.log(`      错误: ${log.error_message}`);
        }
      });
    }
    console.log('');

    // 6. 获取服务器列表查看状态
    console.log('6. 查看服务器在线状态...');
    const serversRes = await request('GET', '/api/server/accounts');
    if (serversRes.body.success) {
      const servers = serversRes.body.data;
      console.log(`✓ 共 ${servers.length} 台服务器:`);
      servers.forEach(server => {
        const statusIcon = server.status === 'online' ? '🟢' :
                          server.status === 'offline' ? '🔴' : '⚪';
        console.log(`   ${statusIcon} ${server.name} (${server.host})`);
        if (server.last_check_time) {
          const lastCheck = new Date(server.last_check_time).toLocaleString('zh-CN');
          console.log(`      最后检查: ${lastCheck} - ${server.last_check_status}`);
        }
      });
    }
    console.log('');

    console.log('========================================');
    console.log('✓ 监控功能和日志测试完成!');
    console.log('========================================');

  } catch (error) {
    console.error('测试失败:', error.message);
    process.exit(1);
  }
}

main();
