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
            /* Toast容器 - 不同位置 */
            .toast-manager-container {
                position: fixed;
                z-index: 9999;
                pointer-events: none;
                display: flex;
                flex-direction: column;
                gap: 12px;
                max-width: 420px;
                padding: 16px;
            }

            .toast-manager-container.top-right {
                top: 0;
                right: 0;
                align-items: flex-end;
            }

            .toast-manager-container.top-left {
                top: 0;
                left: 0;
                align-items: flex-start;
            }

            .toast-manager-container.bottom-right {
                bottom: 0;
                right: 0;
                align-items: flex-end;
                flex-direction: column-reverse;
            }

            .toast-manager-container.bottom-left {
                bottom: 0;
                left: 0;
                align-items: flex-start;
                flex-direction: column-reverse;
            }

            .toast-manager-container.top-center {
                top: 0;
                left: 50%;
                transform: translateX(-50%);
                align-items: center;
            }

            .toast-manager-container.bottom-center {
                bottom: 0;
                left: 50%;
                transform: translateX(-50%);
                align-items: center;
                flex-direction: column-reverse;
            }

            /* Toast主体 */
            .toast-manager-item {
                position: relative;
                min-width: 240px;
                max-width: 320px;
                padding: 12px 14px;
                background: var(--card-bg, #ffffff);
                border-radius: 8px;
                box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12),
                            0 0 0 1px rgba(0, 0, 0, 0.04);
                pointer-events: auto;
                cursor: pointer;
                overflow: hidden;
                transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            }

            .toast-manager-item:hover {
                transform: translateY(-2px);
                box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2),
                            0 0 0 1px rgba(0, 0, 0, 0.1);
            }

            .toast-manager-item.paused {
                transform: scale(1.02);
            }

            /* Toast内容区域 */
            .toast-manager-content {
                display: flex;
                align-items: flex-start;
                gap: 12px;
            }

            /* 图标样式 */
            .toast-manager-icon {
                flex-shrink: 0;
                width: 20px;
                height: 20px;
                display: flex;
                align-items: center;
                justify-content: center;
                font-size: 16px;
                border-radius: 50%;
                padding: 3px;
            }

            .toast-manager-item.success .toast-manager-icon {
                color: #10b981;
                background: rgba(16, 185, 129, 0.1);
            }

            .toast-manager-item.error .toast-manager-icon {
                color: #ef4444;
                background: rgba(239, 68, 68, 0.1);
            }

            .toast-manager-item.warning .toast-manager-icon {
                color: #f59e0b;
                background: rgba(245, 158, 11, 0.1);
            }

            .toast-manager-item.info .toast-manager-icon {
                color: #3b82f6;
                background: rgba(59, 130, 246, 0.1);
            }

            /* 文本内容 */
            .toast-manager-text {
                flex: 1;
                min-width: 0;
            }

            .toast-manager-title {
                font-size: 13px;
                font-weight: 600;
                color: var(--text-primary, #1f2937);
                margin-bottom: 3px;
                line-height: 1.3;
            }

            .toast-manager-message {
                font-size: 12px;
                color: var(--text-secondary, #6b7280);
                line-height: 1.4;
                word-wrap: break-word;
            }

            /* 仅有消息无标题时的样式 */
            .toast-manager-text:not(:has(.toast-manager-title)) .toast-manager-message {
                color: var(--text-primary, #1f2937);
                font-weight: 500;
            }

            /* 关闭按钮 */
            .toast-manager-close {
                position: absolute;
                top: 10px;
                right: 10px;
                width: 18px;
                height: 18px;
                display: flex;
                align-items: center;
                justify-content: center;
                background: transparent;
                border: none;
                border-radius: 3px;
                color: var(--text-tertiary, #9ca3af);
                cursor: pointer;
                font-size: 12px;
                transition: all 0.2s;
                opacity: 0;
            }

            .toast-manager-item:hover .toast-manager-close {
                opacity: 1;
            }

            .toast-manager-close:hover {
                background: var(--bg-secondary, #f3f4f6);
                color: var(--text-primary, #1f2937);
            }

            /* 进度条 */
            .toast-manager-progress {
                position: absolute;
                bottom: 0;
                left: 0;
                height: 2px;
                background: currentColor;
                opacity: 0.3;
                transition: width linear;
            }

            .toast-manager-item.success .toast-manager-progress {
                color: #10b981;
            }

            .toast-manager-item.error .toast-manager-progress {
                color: #ef4444;
            }

            .toast-manager-item.warning .toast-manager-progress {
                color: #f59e0b;
            }

            .toast-manager-item.info .toast-manager-progress {
                color: #3b82f6;
            }

            /* 进入动画 */
            @keyframes toast-slide-in-right {
                from {
                    transform: translateX(calc(100% + 16px));
                    opacity: 0;
                }
                to {
                    transform: translateX(0);
                    opacity: 1;
                }
            }

            @keyframes toast-slide-in-left {
                from {
                    transform: translateX(calc(-100% - 16px));
                    opacity: 0;
                }
                to {
                    transform: translateX(0);
                    opacity: 1;
                }
            }

            @keyframes toast-slide-in-center {
                from {
                    transform: translateY(-20px) scale(0.95);
                    opacity: 0;
                }
                to {
                    transform: translateY(0) scale(1);
                    opacity: 1;
                }
            }

            .toast-manager-container.top-right .toast-manager-item,
            .toast-manager-container.bottom-right .toast-manager-item {
                animation: toast-slide-in-right 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            }

            .toast-manager-container.top-left .toast-manager-item,
            .toast-manager-container.bottom-left .toast-manager-item {
                animation: toast-slide-in-left 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            }

            .toast-manager-container.top-center .toast-manager-item,
            .toast-manager-container.bottom-center .toast-manager-item {
                animation: toast-slide-in-center 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            }

            /* 退出动画 */
            @keyframes toast-slide-out-right {
                from {
                    transform: translateX(0);
                    opacity: 1;
                }
                to {
                    transform: translateX(calc(100% + 16px));
                    opacity: 0;
                }
            }

            @keyframes toast-slide-out-left {
                from {
                    transform: translateX(0);
                    opacity: 1;
                }
                to {
                    transform: translateX(calc(-100% - 16px));
                    opacity: 0;
                }
            }

            @keyframes toast-fade-out {
                from {
                    transform: scale(1);
                    opacity: 1;
                }
                to {
                    transform: scale(0.95);
                    opacity: 0;
                }
            }

            .toast-manager-item.removing {
                pointer-events: none;
            }

            .toast-manager-container.top-right .toast-manager-item.removing,
            .toast-manager-container.bottom-right .toast-manager-item.removing {
                animation: toast-slide-out-right 0.3s cubic-bezier(0.4, 0, 0.2, 1) forwards;
            }

            .toast-manager-container.top-left .toast-manager-item.removing,
            .toast-manager-container.bottom-left .toast-manager-item.removing {
                animation: toast-slide-out-left 0.3s cubic-bezier(0.4, 0, 0.2, 1) forwards;
            }

            .toast-manager-container.top-center .toast-manager-item.removing,
            .toast-manager-container.bottom-center .toast-manager-item.removing {
                animation: toast-fade-out 0.3s cubic-bezier(0.4, 0, 0.2, 1) forwards;
            }

            /* 响应式设计 */
            @media (max-width: 640px) {
                .toast-manager-container {
                    max-width: calc(100vw - 24px);
                    padding: 8px;
                }

                .toast-manager-item {
                    min-width: 220px;
                    max-width: calc(100vw - 24px);
                    padding: 10px 12px;
                }

                .toast-manager-title {
                    font-size: 12px;
                }

                .toast-manager-message {
                    font-size: 11px;
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
     * 快捷方法 - 成功提示
     */
    success(message, options = {}) {
        return this.show({
            type: 'success',
            message,
            ...options,
        });
    }

    /**
     * 快捷方法 - 错误提示
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
     * 快捷方法 - 警告提示
     */
    warning(message, options = {}) {
        return this.show({
            type: 'warning',
            message,
            ...options,
        });
    }

    /**
     * 快捷方法 - 信息提示
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
