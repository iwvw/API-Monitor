/**
 * 现代化 Toast 通知系统
 * 功能特性:
 * - 多种样式类型(success, error, warning, info)
 * - 可配置的位置(top-right, top-left, bottom-right, bottom-left, top-center, bottom-center)
 * - 自动消失或手动关闭
 * - 进度条显示剩余时间
 * - 支持标题和描述
 * - 支持图标自定义
 * - 流畅的动画效果
 * - 多个toast自动堆叠管理
 * - 响应式设计
 */

class ToastManager {
    constructor() {
        this.toasts = new Map(); // 存储所有活动的toast
        this.idCounter = 0; // toast ID计数器
        this.containers = new Map(); // 不同位置的容器
        this.maxToasts = 3; // 最多同时显示的toast数量
        this.defaultOptions = {
            type: 'info', // success | error | warning | info
            position: 'bottom-right', // top-right | top-left | bottom-right | bottom-left | top-center | bottom-center
            duration: 3000, // 显示时长(毫秒), 0表示不自动关闭
            closable: true, // 是否显示关闭按钮
            progress: true, // 是否显示进度条
            pauseOnHover: true, // 鼠标悬停时暂停自动关闭
            title: '', // 标题
            message: '', // 消息内容
            icon: null, // 自定义图标
            onClick: null, // 点击回调
            onClose: null, // 关闭回调
            isManual: false, // 是否为手动点击触发 (默认为 false，不显示自动提示)
        };

        // 默认图标映射
        this.iconMap = {
            success: 'fas fa-check-circle',
            error: 'fas fa-times-circle',
            warning: 'fas fa-exclamation-triangle',
            info: 'fas fa-info-circle',
        };

        // 初始化样式
        this.injectStyles();
    }

    /**
     * 注入CSS样式
     */
    injectStyles() {
        if (document.getElementById('toast-manager-styles')) return;

        const styles = `
            /* ============ Toast 容器 ============ */
            .toast-manager-container {
                position: fixed;
                z-index: 10000;
                pointer-events: none;
                display: flex;
                flex-direction: column;
                gap: 8px;
                max-width: 360px;
                padding: 0;
            }

            .toast-manager-container.top-right { top: 16px; right: 16px; align-items: flex-end; }
            .toast-manager-container.top-left { top: 16px; left: 16px; align-items: flex-start; }
            .toast-manager-container.bottom-right { bottom: 16px; right: 16px; align-items: flex-end; flex-direction: column-reverse; }
            .toast-manager-container.bottom-left { bottom: 16px; left: 16px; align-items: flex-start; flex-direction: column-reverse; }
            .toast-manager-container.top-center { top: 16px; left: 50%; transform: translateX(-50%); align-items: center; }
            .toast-manager-container.bottom-center { bottom: 16px; left: 50%; transform: translateX(-50%); align-items: center; flex-direction: column-reverse; }

            /* ============ Toast 主体 - 紧凑现代风 ============ */
            .toast-manager-item {
                position: relative;
                display: flex;
                align-items: center;
                gap: 10px;
                min-width: 200px;
                max-width: 320px;
                padding: 10px 14px 10px 12px;
                background: var(--card-bg, #ffffff);
                backdrop-filter: blur(12px) saturate(150%);
                -webkit-backdrop-filter: blur(12px) saturate(150%);
                border: 1px solid var(--border-color, rgba(0,0,0,0.08));
                border-radius: 10px;
                box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08), 0 1px 3px rgba(0, 0, 0, 0.06);
                pointer-events: auto;
                cursor: pointer;
                overflow: hidden;
                transition: transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;
            }

            .toast-manager-item:hover {
                transform: translateY(-2px);
                box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12), 0 2px 6px rgba(0, 0, 0, 0.08);
            }

            .toast-manager-item.paused { transform: scale(1.01); }

            /* ============ 类型边框色 ============ */
            .toast-manager-item.success { border-left: 3px solid #10b981; }
            .toast-manager-item.error { border-left: 3px solid #ef4444; }
            .toast-manager-item.warning { border-left: 3px solid #f59e0b; }
            .toast-manager-item.info { border-left: 3px solid #3b82f6; }

            /* ============ 内容区域 ============ */
            .toast-manager-content {
                display: flex;
                align-items: center;
                gap: 10px;
                flex: 1;
                min-width: 0;
            }

            /* ============ 图标 ============ */
            .toast-manager-icon {
                flex-shrink: 0;
                width: 28px;
                height: 28px;
                display: flex;
                align-items: center;
                justify-content: center;
                font-size: 13px;
                border-radius: 8px;
            }

            .toast-manager-item.success .toast-manager-icon { color: #10b981; background: rgba(16, 185, 129, 0.1); }
            .toast-manager-item.error .toast-manager-icon { color: #ef4444; background: rgba(239, 68, 68, 0.1); }
            .toast-manager-item.warning .toast-manager-icon { color: #f59e0b; background: rgba(245, 158, 11, 0.1); }
            .toast-manager-item.info .toast-manager-icon { color: #3b82f6; background: rgba(59, 130, 246, 0.1); }

            /* ============ 文字 ============ */
            .toast-manager-text { flex: 1; min-width: 0; }

            .toast-manager-title {
                font-size: 12px;
                font-weight: 600;
                color: var(--text-primary);
                line-height: 1.3;
                margin-bottom: 1px;
            }

            .toast-manager-message {
                font-size: 12px;
                color: var(--text-secondary);
                line-height: 1.4;
                white-space: nowrap;
                overflow: hidden;
                text-overflow: ellipsis;
            }

            /* 仅消息无标题时 */
            .toast-manager-text:not(:has(.toast-manager-title)) .toast-manager-message {
                color: var(--text-primary);
                font-weight: 500;
            }

            /* ============ 关闭按钮 ============ */
            .toast-manager-close {
                position: absolute;
                top: 6px;
                right: 6px;
                width: 16px;
                height: 16px;
                display: flex;
                align-items: center;
                justify-content: center;
                background: transparent;
                border: none;
                border-radius: 4px;
                color: var(--text-tertiary);
                cursor: pointer;
                font-size: 9px;
                transition: all 0.15s;
                opacity: 0;
            }

            .toast-manager-item:hover .toast-manager-close { opacity: 0.6; }
            .toast-manager-close:hover { opacity: 1 !important; background: rgba(239, 68, 68, 0.1); color: #ef4444; }

            /* ============ 进度条 ============ */
            .toast-manager-progress {
                position: absolute;
                bottom: 0;
                left: 0;
                height: 2px;
                background: currentColor;
                opacity: 0.4;
                transition: width linear;
                border-radius: 0 0 0 10px;
            }

            .toast-manager-item.success .toast-manager-progress { color: #10b981; }
            .toast-manager-item.error .toast-manager-progress { color: #ef4444; }
            .toast-manager-item.warning .toast-manager-progress { color: #f59e0b; }
            .toast-manager-item.info .toast-manager-progress { color: #3b82f6; }

            /* ============ 动画 ============ */
            @keyframes toast-pop-in {
                0% { transform: translateX(20px) scale(0.95); opacity: 0; }
                100% { transform: translateX(0) scale(1); opacity: 1; }
            }
            @keyframes toast-pop-in-left {
                0% { transform: translateX(-20px) scale(0.95); opacity: 0; }
                100% { transform: translateX(0) scale(1); opacity: 1; }
            }
            @keyframes toast-pop-in-center {
                0% { transform: translateY(-10px) scale(0.95); opacity: 0; }
                100% { transform: translateY(0) scale(1); opacity: 1; }
            }
            @keyframes toast-pop-out {
                0% { transform: translateX(0) scale(1); opacity: 1; }
                100% { transform: translateX(30px) scale(0.9); opacity: 0; }
            }
            @keyframes toast-pop-out-left {
                0% { transform: translateX(0) scale(1); opacity: 1; }
                100% { transform: translateX(-30px) scale(0.9); opacity: 0; }
            }
            @keyframes toast-fade-out {
                0% { transform: scale(1); opacity: 1; }
                100% { transform: scale(0.95); opacity: 0; }
            }
            @keyframes toast-fade-in {
                0% { opacity: 0; transform: scale(0.95); }
                100% { opacity: 1; transform: scale(1); }
            }

            .toast-manager-container.top-right .toast-manager-item,
            .toast-manager-container.bottom-right .toast-manager-item {
                animation: toast-pop-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
            }
            .toast-manager-container.top-left .toast-manager-item,
            .toast-manager-container.bottom-left .toast-manager-item {
                animation: toast-pop-in-left 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
            }
            .toast-manager-container.top-center .toast-manager-item,
            .toast-manager-container.bottom-center .toast-manager-item {
                animation: toast-pop-in-center 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
            }

            .toast-manager-item.removing { pointer-events: none; }

            .toast-manager-container.top-right .toast-manager-item.removing,
            .toast-manager-container.bottom-right .toast-manager-item.removing {
                animation: toast-pop-out 0.2s ease forwards;
            }
            .toast-manager-container.top-left .toast-manager-item.removing,
            .toast-manager-container.bottom-left .toast-manager-item.removing {
                animation: toast-pop-out-left 0.2s ease forwards;
            }
            .toast-manager-container.top-center .toast-manager-item.removing,
            .toast-manager-container.bottom-center .toast-manager-item.removing {
                animation: toast-fade-out 0.2s ease forwards;
            }

            /* ============ 移动端适配 ============ */
            @media (max-width: 768px) {
                .toast-manager-container,
                .toast-manager-container.top-right,
                .toast-manager-container.top-left,
                .toast-manager-container.top-center,
                .toast-manager-container.bottom-right,
                .toast-manager-container.bottom-left,
                .toast-manager-container.bottom-center {
                    padding: 8px;
                    max-width: 280px;
                    left: auto !important;
                    right: 0 !important;
                    top: 0 !important;
                    bottom: auto !important;
                    transform: none !important;
                    align-items: flex-end !important;
                    flex-direction: column !important;
                }

                .toast-manager-container .toast-manager-item,
                .toast-manager-container.top-right .toast-manager-item,
                .toast-manager-container.top-left .toast-manager-item,
                .toast-manager-container.top-center .toast-manager-item,
                .toast-manager-container.bottom-right .toast-manager-item,
                .toast-manager-container.bottom-left .toast-manager-item,
                .toast-manager-container.bottom-center .toast-manager-item {
                    width: auto;
                    max-width: 260px;
                    min-width: 160px;
                    padding: 6px 10px;
                    border-radius: 8px;
                    gap: 8px;
                    animation: toast-fade-in 0.2s ease !important;
                }

                .toast-manager-container .toast-manager-item.removing,
                .toast-manager-container.top-right .toast-manager-item.removing,
                .toast-manager-container.top-left .toast-manager-item.removing,
                .toast-manager-container.top-center .toast-manager-item.removing,
                .toast-manager-container.bottom-right .toast-manager-item.removing,
                .toast-manager-container.bottom-left .toast-manager-item.removing,
                .toast-manager-container.bottom-center .toast-manager-item.removing {
                    animation: toast-fade-out 0.15s ease forwards !important;
                }

                .toast-manager-icon {
                    width: 20px;
                    height: 20px;
                    font-size: 10px;
                    border-radius: 5px;
                }

                .toast-manager-title {
                    font-size: 11px;
                }

                .toast-manager-message {
                    font-size: 10px;
                }

                .toast-manager-close {
                    display: none;
                }

                .toast-manager-progress {
                    height: 1.5px;
                }
            }
        `;

        const styleElement = document.createElement('style');
        styleElement.id = 'toast-manager-styles';
        styleElement.textContent = styles;
        document.head.appendChild(styleElement);
    }

    /**
     * 获取或创建指定位置的容器
     */
    getContainer(position) {
        if (!this.containers.has(position)) {
            const container = document.createElement('div');
            container.className = `toast-manager-container ${position}`;
            document.body.appendChild(container);
            this.containers.set(position, container);
        }
        return this.containers.get(position);
    }

    /**
     * 显示Toast
     */
    show(options) {
        const config = { ...this.defaultOptions, ...options };

        // 核心过滤器: 
        // - 错误提示 (type: 'error') 始终显示
        // - 成功提示 (type: 'success') 始终显示
        // - 警告提示 (type: 'warning') 始终显示
        // - 信息提示 (type: 'info') 只有手动触发 (isManual: true) 才显示
        if (config.type === 'info' && !config.isManual) {
            // info 类型的自动提示将被拦截，不在界面弹出
            return null;
        }

        // 去重逻辑：检查是否有相同类型和内容的 toast 正在显示
        const duplicateKey = `${config.type}:${config.message}`;
        for (const [existingId, existingToast] of this.toasts) {
            const existingKey = `${existingToast.config.type}:${existingToast.config.message}`;
            if (existingKey === duplicateKey) {
                // 相同的 toast 已存在，跳过显示
                return existingId;
            }
        }

        // 检查是否超过最大数量,如果超过则移除最旧的toast
        const positionToasts = Array.from(this.toasts.values())
            .filter(t => t.config.position === config.position);

        if (positionToasts.length >= this.maxToasts) {
            // 移除最旧的toast
            const oldestToast = positionToasts[0];
            if (oldestToast) {
                this.remove(Array.from(this.toasts.entries())
                    .find(([_, t]) => t === oldestToast)?.[0]);
            }
        }

        const id = ++this.idCounter;

        // 创建toast元素
        const toast = this.createToastElement(id, config);
        const container = this.getContainer(config.position);

        // 添加到容器
        if (config.position.includes('bottom')) {
            container.appendChild(toast);
        } else {
            container.insertBefore(toast, container.firstChild);
        }

        // 设置定时器
        const timer = this.setupTimer(id, toast, config);

        // 保存toast信息
        this.toasts.set(id, {
            element: toast,
            config,
            timer,
            startTime: Date.now(),
            remainingTime: config.duration,
        });

        return id;
    }

    /**
     * 创建Toast元素
     */
    createToastElement(id, config) {
        const toast = document.createElement('div');
        toast.className = `toast-manager-item ${config.type}`;
        toast.dataset.toastId = id;

        // 内容容器
        const content = document.createElement('div');
        content.className = 'toast-manager-content';

        // 图标
        const icon = document.createElement('div');
        icon.className = 'toast-manager-icon';
        const iconClass = config.icon || this.iconMap[config.type];
        icon.innerHTML = `<i class="${iconClass}"></i>`;
        content.appendChild(icon);

        // 文本内容
        const textContainer = document.createElement('div');
        textContainer.className = 'toast-manager-text';

        if (config.title) {
            const title = document.createElement('div');
            title.className = 'toast-manager-title';
            title.textContent = config.title;
            textContainer.appendChild(title);
        }

        if (config.message) {
            const message = document.createElement('div');
            message.className = 'toast-manager-message';
            message.textContent = config.message;
            textContainer.appendChild(message);
        }

        content.appendChild(textContainer);
        toast.appendChild(content);

        // 关闭按钮
        if (config.closable) {
            const closeBtn = document.createElement('button');
            closeBtn.className = 'toast-manager-close';
            closeBtn.innerHTML = '<i class="fas fa-times"></i>';
            closeBtn.onclick = (e) => {
                e.stopPropagation();
                this.remove(id);
            };
            toast.appendChild(closeBtn);
        }

        // 进度条
        if (config.progress && config.duration > 0) {
            const progress = document.createElement('div');
            progress.className = 'toast-manager-progress';
            progress.style.width = '100%';
            progress.style.transitionDuration = `${config.duration}ms`;
            toast.appendChild(progress);

            // 触发进度条动画
            requestAnimationFrame(() => {
                progress.style.width = '0%';
            });
        }

        // 鼠标悬停暂停
        if (config.pauseOnHover && config.duration > 0) {
            toast.addEventListener('mouseenter', () => {
                this.pause(id);
            });
            toast.addEventListener('mouseleave', () => {
                this.resume(id);
            });
        }

        // 点击事件
        if (config.onClick) {
            toast.style.cursor = 'pointer';
            toast.addEventListener('click', () => {
                config.onClick(id);
            });
        }

        return toast;
    }

    /**
     * 设置自动关闭定时器
     */
    setupTimer(id, toast, config) {
        if (config.duration <= 0) return null;

        return setTimeout(() => {
            this.remove(id);
        }, config.duration);
    }

    /**
     * 暂停自动关闭
     */
    pause(id) {
        const toastData = this.toasts.get(id);
        if (!toastData || !toastData.timer) return;

        clearTimeout(toastData.timer);
        toastData.remainingTime = toastData.config.duration - (Date.now() - toastData.startTime);
        toastData.element.classList.add('paused');

        // 暂停进度条
        const progress = toastData.element.querySelector('.toast-manager-progress');
        if (progress) {
            const currentWidth = progress.getBoundingClientRect().width;
            const totalWidth = progress.parentElement.getBoundingClientRect().width;
            progress.style.transitionDuration = '0s';
            progress.style.width = `${(currentWidth / totalWidth) * 100}%`;
        }
    }

    /**
     * 恢复自动关闭
     */
    resume(id) {
        const toastData = this.toasts.get(id);
        if (!toastData) return;

        toastData.element.classList.remove('paused');

        if (toastData.config.duration > 0) {
            toastData.startTime = Date.now();
            toastData.timer = setTimeout(() => {
                this.remove(id);
            }, toastData.remainingTime);

            // 恢复进度条
            const progress = toastData.element.querySelector('.toast-manager-progress');
            if (progress) {
                progress.style.transitionDuration = `${toastData.remainingTime}ms`;
                progress.style.width = '0%';
            }
        }
    }

    /**
     * 移除Toast
     */
    remove(id) {
        const toastData = this.toasts.get(id);
        if (!toastData) return;

        const { element, timer, config } = toastData;

        // 清除定时器
        if (timer) {
            clearTimeout(timer);
        }

        // 添加移除动画
        element.classList.add('removing');

        // 动画结束后移除元素
        setTimeout(() => {
            if (element.parentNode) {
                element.parentNode.removeChild(element);
            }

            // 清理数据
            this.toasts.delete(id);

            // 调用关闭回调
            if (config.onClose) {
                config.onClose(id);
            }

            // 如果容器为空,移除容器
            const container = this.containers.get(config.position);
            if (container && container.children.length === 0) {
                container.remove();
                this.containers.delete(config.position);
            }
        }, 300);
    }

    /**
     * 移除所有Toast
     */
    removeAll() {
        this.toasts.forEach((_, id) => {
            this.remove(id);
        });
    }

    /**
     * 快捷方法 - 成功提示 (始终显示)
     */
    success(message, options = {}) {
        return this.show({
            type: 'success',
            message,
            ...options,
        });
    }

    /**
     * 快捷方法 - 错误提示 (始终显示)
     */
    error(message, options = {}) {
        return this.show({
            type: 'error',
            message,
            duration: 4000, // 错误提示默认显示更久
            ...options,
        });
    }

    /**
     * 快捷方法 - 警告提示 (始终显示)
     */
    warning(message, options = {}) {
        return this.show({
            type: 'warning',
            message,
            ...options,
        });
    }

    /**
     * 快捷方法 - 信息提示 (默认不显示，需设置 isManual: true)
     */
    info(message, options = {}) {
        return this.show({
            type: 'info',
            message,
            ...options,
        });
    }
}

// 创建全局单例
const toastManager = new ToastManager();

// 导出实例和快捷函数
export default toastManager;

export const toast = {
    show: (options) => toastManager.show(options),
    success: (message, options) => toastManager.success(message, options),
    error: (message, options) => toastManager.error(message, options),
    warning: (message, options) => toastManager.warning(message, options),
    info: (message, options) => toastManager.info(message, options),
    remove: (id) => toastManager.remove(id),
    removeAll: () => toastManager.removeAll(),
};

// 兼容旧的showToast函数
export function showToast(message, type = 'info') {
    return toastManager[type](message);
}
