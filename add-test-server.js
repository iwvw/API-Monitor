/**
 * 添加真实测试服务器
 * 服务器: 8.148.83.42:22
 * 用户名: root
 * 密码: ssln5014
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
    const loginRes = await request('POST', '/api/login', { password: '123456' });
    if (!loginRes.body.success) {
      console.error('✗ 登录失败');
      process.exit(1);
    }
    console.log('✓ 登录成功\n');

    // 2. 测试连接
    console.log('2. 测试服务器连接...');
    const testRes = await request('POST', '/api/server/test-connection', {
      host: '8.148.83.42',
      port: 22,
      username: 'root',
      auth_type: 'password',
      password: 'ssln5014.'
    });

    console.log(`   连接结果: ${testRes.body.success ? '成功 ✓' : '失败 ✗'}`);
    if (!testRes.body.success) {
      console.error('   错误:', testRes.body.error);
      console.error('\n无法连接到服务器,请检查网络和凭据');
      process.exit(1);
    }
    console.log('');

    // 3. 添加服务器
    console.log('3. 添加服务器到系统...');
    const createRes = await request('POST', '/api/server/accounts', {
      name: '阿里云测试服务器',
      host: '8.148.83.42',
      port: 22,
      username: 'root',
      auth_type: 'password',
      password: 'ssln5014.',
      tags: ['阿里云', '生产'],
      description: '用于API监控系统测试'
    });

    if (!createRes.body.success) {
      console.error('✗ 添加失败:', createRes.body.error);
      process.exit(1);
    }

    const serverId = createRes.body.data.id;
    console.log('✓ 添加成功');
    console.log(`   服务器ID: ${serverId}`);
    console.log(`   名称: ${createRes.body.data.name}`);
    console.log(`   地址: ${createRes.body.data.host}:${createRes.body.data.port}`);
    console.log('');

    // 4. 获取服务器详细信息
    console.log('4. 获取服务器详细信息...');
    const infoRes = await request('POST', '/api/server/info', {
      id: serverId
    });

    if (infoRes.body.success) {
      const info = infoRes.body.data;
      console.log('✓ 系统信息:');
      console.log(`   操作系统: ${info.os.distro} ${info.os.release}`);
      console.log(`   内核: ${info.os.kernel}`);
      console.log(`   运行时间: ${info.os.uptime}`);
      console.log(`   CPU: ${info.cpu.model} (${info.cpu.cores}核)`);
      console.log(`   内存: ${info.memory.used}/${info.memory.total} (${info.memory.usedPercent}%)`);
      console.log(`   磁盘: ${info.disk.used}/${info.disk.total} (${info.disk.usedPercent}%)`);

      if (info.docker) {
        console.log(`   Docker: ${info.docker.installed ? '已安装' : '未安装'}`);
        if (info.docker.installed && info.docker.containers) {
          console.log(`   容器数: ${info.docker.containers.length}`);
        }
      }
    } else {
      console.log('✗ 无法获取详细信息:', infoRes.body.error);
    }
    console.log('');

    console.log('========================================');
    console.log('✓ 服务器添加并测试完成!');
    console.log('========================================');

  } catch (error) {
    console.error('操作失败:', error.message);
    process.exit(1);
  }
}

main();
