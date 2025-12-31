/**
 * useModal Composable
 * 管理模态框状态的可复用逻辑
 */
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';

/**
 * 创建一个模态框状态管理
 * @param {Object} options - 配置选项
 * @param {boolean} options.lockBodyScroll - 是否在打开时锁定页面滚动
 * @param {Function} options.onOpen - 打开时的回调
 * @param {Function} options.onClose - 关闭时的回调
 * @returns {Object} 模态框状态和方法
 */
export function useModal(options = {}) {
  const { lockBodyScroll = true, onOpen, onClose } = options;

  const isOpen = ref(false);
  const isAnimating = ref(false);

  /**
   * 打开模态框
   * @param {*} data - 可选的初始数据
   */
  function open(data) {
    if (isOpen.value) return;
    isOpen.value = true;
    isAnimating.value = true;

    if (lockBodyScroll) {
      document.body.classList.add('modal-open');
    }

    if (onOpen) {
      onOpen(data);
    }

    // 动画结束后重置状态
    setTimeout(() => {
      isAnimating.value = false;
    }, 300);
  }

  /**
   * 关闭模态框
   */
  function close() {
    if (!isOpen.value) return;
    isAnimating.value = true;

    setTimeout(() => {
      isOpen.value = false;
      isAnimating.value = false;

      if (lockBodyScroll) {
        document.body.classList.remove('modal-open');
      }

      if (onClose) {
        onClose();
      }
    }, 200);
  }

  /**
   * 切换模态框状态
   */
  function toggle() {
    if (isOpen.value) {
      close();
    } else {
      open();
    }
  }

  // ESC 键关闭
  function handleKeydown(event) {
    if (event.key === 'Escape' && isOpen.value) {
      close();
    }
  }

  onMounted(() => {
    document.addEventListener('keydown', handleKeydown);
  });

  onUnmounted(() => {
    document.removeEventListener('keydown', handleKeydown);
    if (isOpen.value && lockBodyScroll) {
      document.body.classList.remove('modal-open');
    }
  });

  return {
    isOpen,
    isAnimating,
    open,
    close,
    toggle,
  };
}

/**
 * 创建多个模态框的管理器
 * 确保同时只有一个模态框打开
 * @param {string[]} modalNames - 模态框名称列表
 * @returns {Object} 模态框状态和方法
 */
export function useModalGroup(modalNames) {
  const modals = {};

  modalNames.forEach(name => {
    modals[name] = ref(false);
  });

  const activeModal = computed(() => {
    for (const name of modalNames) {
      if (modals[name].value) return name;
    }
    return null;
  });

  const isAnyOpen = computed(() => activeModal.value !== null);

  function open(name, closeOthers = true) {
    if (closeOthers) {
      modalNames.forEach(n => {
        modals[n].value = false;
      });
    }
    if (modals[name]) {
      modals[name].value = true;
      document.body.classList.add('modal-open');
    }
  }

  function close(name) {
    if (modals[name]) {
      modals[name].value = false;
    }
    if (!isAnyOpen.value) {
      document.body.classList.remove('modal-open');
    }
  }

  function closeAll() {
    modalNames.forEach(name => {
      modals[name].value = false;
    });
    document.body.classList.remove('modal-open');
  }

  return {
    modals,
    activeModal,
    isAnyOpen,
    open,
    close,
    closeAll,
  };
}

export default useModal;
