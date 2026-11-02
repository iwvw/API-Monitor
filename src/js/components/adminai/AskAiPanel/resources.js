// 数组元素是否带资源标识（id/_id/appName/name）：区分真正的资源列表与
// 内部数据数组（如主机 info.disk），后者被拒绝，避免任意子值深入时被截胡
export function isResourceArray(arr) {
  return arr.length > 0 && arr.every((el) => {
    if (!el || typeof el !== 'object') return false;
    return ['id', '_id', 'appName', 'name'].some((k) => el[k] !== undefined && el[k] !== null && el[k] !== '');
  });
}

// 数组元素是否带「强资源标识」（id/_id/appName/channelId）：用于判断扁平资源数组
// （如 /api/server/accounts 的 data 主机列表）。isResourceArray 把仅带 name 的包裹对象
// （如 koyeb accounts 元素、flyio account 包裹）也算作资源，会误伤跨元素合并/嵌套穿透，
// 因此扁平判断只用真正可作引用 ID 的强标识。
export function isStrongResourceArray(arr) {
  return arr.length > 0 && arr.every((el) => {
    if (!el || typeof el !== 'object') return false;
    return ['id', '_id', 'appName', 'channelId'].some((k) => el[k] !== undefined && el[k] !== null && el[k] !== '');
  });
}

// 列表响应宽容解析：精确键、信封（data/items/list）、跨元素合并（多账号 apps 嵌套）、
// 包裹穿透（koyeb accounts[].projects[].services）；内部数据数组会被过滤
export function extractResourceList(data, keys) {
  if (!data) return [];
  // 顶层即为扁平资源数组（如 /api/server/accounts 的 data 主机列表，元素带 id）：
  // 直接返回，避免被「任意子值深入」截胡成内部数据数组（如主机 info.gpu 只取到
  // 1 个 GPU）。koyeb 的 accounts 元素仅带 name（无强标识），此处不命中。
  if (Array.isArray(data) && isStrongResourceArray(data)) return data;
  const walk = (v, depth) => {
    if (depth > 4) return null;
    if (Array.isArray(v)) {
      if (v.length === 0 || typeof v[0] !== 'object') return null;
      // 收集所有元素的嵌套资源数组（flyio data[].apps 多账号、koyeb projects[].services 多项目）
      const gathered = [];
      let hit = false;
      for (const el of v) {
        if (!el || typeof el !== 'object') continue;
        for (const k of keys) {
          if (Array.isArray(el[k]) && el[k].length > 0 && typeof el[k][0] === 'object') {
            gathered.push(...el[k]);
            hit = true;
          }
        }
      }
      if (hit) return gathered;
      // 元素可能是包裹对象（account→projects），深入其子值找命中 keys 的数组
      for (const el of v) {
        for (const key of Object.keys(el)) {
          if (el[key] && typeof el[key] === 'object') {
            const r = walk(el[key], depth + 1);
            if (r) return r;
          }
        }
      }
      // 兜底：仅当元素带资源标识时才视为资源列表（拒绝 disk/cpu 等内部数据数组）
      return isResourceArray(v) ? v : null;
    }
    if (v && typeof v === 'object') {
      for (const k of keys) {
        if (Array.isArray(v[k])) return v[k];
      }
      for (const k of ['data', 'items', 'list', 'results']) {
        if (v[k] && typeof v[k] === 'object') {
          const hit = walk(v[k], depth + 1);
          if (hit) return hit;
        }
      }
      // 兜底：任意子值深入（如 koyeb data 形如 {accounts:[{projects:[{services}]}]}，
      // accounts 不在信封键内，必须遍历任意子值才能触达 services）
      for (const k of Object.keys(v)) {
        if (v[k] && typeof v[k] === 'object') {
          const hit = walk(v[k], depth + 1);
          if (hit) return hit;
        }
      }
    }
    return null;
  };
  return walk(data, 0) || [];
}
