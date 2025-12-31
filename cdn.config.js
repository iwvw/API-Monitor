/**
 * CDN 配置文件
 * 可选择不同的 CDN 源来加载静态资源
 *
 * 启用方式：在 .env 中设置 VITE_USE_CDN=true
 * 选择 CDN 源：设置 VITE_CDN_PROVIDER=unpkg|jsdelivr|cdnjs|bootcdn
 */

// CDN 提供商配置
const cdnProviders = {
  // jsDelivr - 国内速度较好
  jsdelivr: {
    name: 'jsDelivr',
    baseUrl: 'https://cdn.jsdelivr.net/npm',
    format: (pkg, version, file) => `https://cdn.jsdelivr.net/npm/${pkg}@${version}${file}`,
  },
  // npmmirror - 淘宝 NPM 镜像 (中国大陆推荐)
  npmmirror: {
    name: 'npmmirror',
    baseUrl: 'https://registry.npmmirror.com',
    format: (pkg, version, file) => `https://registry.npmmirror.com/${pkg}/${version}/files${file}`,
  },
  // unpkg - 国际通用
  unpkg: {
    name: 'unpkg',
    baseUrl: 'https://unpkg.com',
    format: (pkg, version, file) => `https://unpkg.com/${pkg}@${version}${file}`,
  },
  // BootCDN - 中国大陆加速
  bootcdn: {
    name: 'BootCDN',
    baseUrl: 'https://cdn.bootcdn.net/ajax/libs',
    format: (pkg, version, file) => {
      // BootCDN 使用不同的路径格式
      const pkgMap = {
        vue: `https://cdn.bootcdn.net/ajax/libs/vue/${version}/vue.global.prod.min.js`,
        '@fortawesome/fontawesome-free': `https://cdn.bootcdn.net/ajax/libs/font-awesome/${version}/css/all.min.css`,
      };
      return (
        pkgMap[pkg] ||
        `https://cdn.bootcdn.net/ajax/libs/${pkg.replace('@', '').replace('/', '-')}/${version}${file}`
      );
    },
  },
};

// 需要通过 CDN 加载的依赖及其版本
// 使用全局构建版本 (IIFE) 而非 ESM 版本
const cdnDependencies = {
  vue: {
    version: '3.5.13',
    // 使用全局构建版本，会在 window 上暴露 Vue
    file: '/dist/vue.global.prod.js',
    global: 'Vue',
    css: false,
  },
  'chart.js': {
    version: '4.4.7',
    file: '/dist/chart.umd.js',
    global: 'Chart',
    css: false,
  },
  '@fortawesome/fontawesome-free': {
    version: '7.1.0',
    file: '/css/all.min.css',
    global: null,
    css: true,
  },
  'simple-icons-font': {
    version: '14.15.0',
    file: '/font/simple-icons.min.css',
    global: null,
    css: true,
  },
  jsqr: {
    version: '1.4.0',
    file: '/dist/jsQR.js',
    global: 'jsQR',
    css: false,
  },
  'html5-qrcode': {
    version: '2.3.8',
    file: '/html5-qrcode.min.js',
    global: 'Html5Qrcode',
    css: false,
  },
};

/**
 * 获取 CDN URL
 * @param {string} provider - CDN 提供商名称
 * @param {string} pkg - 包名
 * @param {string} type - 'js' 或 'css'
 * @returns {string|null} CDN URL
 */
function getCdnUrl(provider, pkg, type = 'js') {
  const cdn = cdnProviders[provider] || cdnProviders.jsdelivr;
  const dep = cdnDependencies[pkg];

  if (!dep) return null;

  if (type === 'css') {
    if (!dep.css) return null;
    const cssFile = typeof dep.css === 'string' ? dep.css : dep.file;

    // 特殊处理: npmmirror 不允许加载 simple-icons-font 的静态文件 (FORBIDDEN)
    // 在这种情况下，强制切换到 unpkg 镜像源
    if (pkg === 'simple-icons-font' && provider === 'npmmirror') {
      const fallbackCdn = cdnProviders.unpkg;
      return fallbackCdn.format(pkg, dep.version, cssFile);
    }

    return cdn.format(pkg, dep.version, cssFile);
  }

  if (!dep.global) return null; // 没有 global 的不作为 JS 加载
  return cdn.format(pkg, dep.version, dep.file);
}

/**
 * 获取所有 CDN 资源 URL
 * @param {string} provider - CDN 提供商
 * @returns {{ js: Array<{url: string, global: string}>, css: string[] }}
 */
function getAllCdnUrls(provider) {
  const result = { js: [], css: [] };

  for (const [pkg, dep] of Object.entries(cdnDependencies)) {
    if (dep.global) {
      const jsUrl = getCdnUrl(provider, pkg, 'js');
      if (jsUrl) result.js.push({ url: jsUrl, global: dep.global, pkg });
    }
    if (dep.css) {
      const cssUrl = getCdnUrl(provider, pkg, 'css');
      if (cssUrl) result.css.push(cssUrl);
    }
  }

  return result;
}

/**
 * 获取 Rollup external 配置
 * @returns {string[]}
 */
function getExternals() {
  return Object.keys(cdnDependencies).filter(pkg => cdnDependencies[pkg].global);
}

/**
 * 获取 Rollup globals 配置
 * @returns {Object}
 */
function getGlobals() {
  const globals = {};
  for (const [pkg, dep] of Object.entries(cdnDependencies)) {
    if (dep.global) {
      globals[pkg] = dep.global;
    }
  }
  return globals;
}

module.exports = {
  cdnProviders,
  cdnDependencies,
  getCdnUrl,
  getAllCdnUrls,
  getExternals,
  getGlobals,
};
