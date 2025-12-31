/**
 * useAppModals Composable
 * 集中管理应用中所有模态框的状态
 */
import { reactive, computed } from 'vue';

/**
 * 应用模态框状态管理
 * @returns {Object} 模态框状态和方法
 */
export function useAppModals() {
  // 所有模态框状态集中管理
  const modals = reactive({
    // 设置相关
    settings: false,
    logViewer: false,

    // Zeabur
    addZeaburAccount: false,

    // Koyeb
    addKoyebAccount: false,

    // Fly.io
    addFlyAccount: false,

    // DNS/Cloudflare
    addDnsAccount: false,
    editDnsAccount: false,
    dnsRecord: false,
    dnsTemplate: false,
    addZone: false,
    newWorker: false,
    workerRoutes: false,
    workerDomains: false,
    pagesDeployments: false,
    pagesDomains: false,

    // OpenAI
    openaiEndpoint: false,

    // Antigravity
    antigravityAccount: false,
    antigravityManual: false,
    antigravityLogDetail: false,

    // Gemini CLI
    geminiCliAccount: false,
    geminiCliLogDetail: false,

    // Server/SSH
    server: false,
    importServer: false,
    docker: false,
    sshTerminal: false,
    addSession: false,
    addCredential: false,
    snippet: false,

    // 图片预览
    imagePreview: false,

    // TOTP
    totp: false,
    totpImport: false,

    // 自定义对话框
    customDialog: false,
  });

  // 判断是否有任何模态框打开
  const isAnyOpen = computed(() => {
    return Object.values(modals).some(v => v === true);
  });

  // 当前打开的模态框名称
  const activeModal = computed(() => {
    for (const [name, isOpen] of Object.entries(modals)) {
      if (isOpen) return name;
    }
    return null;
  });

  /**
   * 打开指定模态框
   * @param {string} name - 模态框名称
   */
  function open(name) {
    if (name in modals) {
      modals[name] = true;
      document.body.classList.add('modal-open');
    }
  }

  /**
   * 关闭指定模态框
   * @param {string} name - 模态框名称
   */
  function close(name) {
    if (name in modals) {
      modals[name] = false;
      // 如果没有其他模态框打开，移除 body class
      if (!isAnyOpen.value) {
        document.body.classList.remove('modal-open');
      }
    }
  }

  /**
   * 切换模态框状态
   * @param {string} name - 模态框名称
   */
  function toggle(name) {
    if (modals[name]) {
      close(name);
    } else {
      open(name);
    }
  }

  /**
   * 关闭所有模态框
   */
  function closeAll() {
    for (const name in modals) {
      modals[name] = false;
    }
    document.body.classList.remove('modal-open');
  }

  return {
    modals,
    isAnyOpen,
    activeModal,
    open,
    close,
    toggle,
    closeAll,
  };
}

export default useAppModals;
