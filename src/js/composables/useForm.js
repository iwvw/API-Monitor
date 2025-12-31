/**
 * useForm Composable
 * 管理表单状态、验证和提交的可复用逻辑
 */
import { ref, reactive, computed } from 'vue';

/**
 * 创建表单状态管理
 * @param {Object} initialValues - 表单初始值
 * @param {Object} options - 配置选项
 * @param {Function} options.onSubmit - 提交处理函数
 * @param {Function} options.validate - 验证函数
 * @returns {Object} 表单状态和方法
 */
export function useForm(initialValues = {}, options = {}) {
  const { onSubmit, validate } = options;

  // 表单数据
  const formData = reactive({ ...initialValues });

  // 状态
  const isSubmitting = ref(false);
  const error = ref('');
  const success = ref('');

  // 字段错误
  const fieldErrors = reactive({});

  // 是否有错误
  const hasErrors = computed(() => {
    return !!error.value || Object.keys(fieldErrors).some(key => !!fieldErrors[key]);
  });

  // 是否已修改
  const isDirty = computed(() => {
    return Object.keys(initialValues).some(key => formData[key] !== initialValues[key]);
  });

  /**
   * 重置表单到初始状态
   */
  function reset() {
    Object.keys(initialValues).forEach(key => {
      formData[key] = initialValues[key];
    });
    error.value = '';
    success.value = '';
    Object.keys(fieldErrors).forEach(key => {
      delete fieldErrors[key];
    });
  }

  /**
   * 设置表单值
   * @param {Object} values - 新值
   */
  function setValues(values) {
    Object.keys(values).forEach(key => {
      if (key in formData) {
        formData[key] = values[key];
      }
    });
  }

  /**
   * 清除所有错误
   */
  function clearErrors() {
    error.value = '';
    Object.keys(fieldErrors).forEach(key => {
      delete fieldErrors[key];
    });
  }

  /**
   * 设置字段错误
   * @param {string} field - 字段名
   * @param {string} message - 错误消息
   */
  function setFieldError(field, message) {
    fieldErrors[field] = message;
  }

  /**
   * 验证表单
   * @returns {boolean} 是否通过验证
   */
  function validateForm() {
    clearErrors();

    if (validate) {
      const result = validate(formData);
      if (result !== true) {
        if (typeof result === 'string') {
          error.value = result;
        } else if (typeof result === 'object') {
          Object.assign(fieldErrors, result);
        }
        return false;
      }
    }

    return true;
  }

  /**
   * 提交表单
   * @returns {Promise<boolean>} 是否成功
   */
  async function submit() {
    if (isSubmitting.value) return false;

    clearErrors();

    if (!validateForm()) {
      return false;
    }

    isSubmitting.value = true;
    success.value = '';

    try {
      if (onSubmit) {
        const result = await onSubmit({ ...formData });
        if (result && result.success) {
          success.value = result.message || '操作成功';
          return true;
        } else if (result && result.error) {
          error.value = result.error;
          return false;
        }
      }
      return true;
    } catch (err) {
      error.value = err.message || '操作失败';
      return false;
    } finally {
      isSubmitting.value = false;
    }
  }

  return {
    formData,
    isSubmitting,
    error,
    success,
    fieldErrors,
    hasErrors,
    isDirty,
    reset,
    setValues,
    clearErrors,
    setFieldError,
    validateForm,
    submit,
  };
}

/**
 * 常用验证规则
 */
export const validators = {
  required: (value, message = '此字段必填') => {
    if (!value || (typeof value === 'string' && !value.trim())) {
      return message;
    }
    return true;
  },

  email: (value, message = '请输入有效的邮箱地址') => {
    if (!value) return true; // 空值由 required 处理
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(value)) {
      return message;
    }
    return true;
  },

  minLength: (min, message) => value => {
    if (!value) return true;
    if (value.length < min) {
      return message || `至少需要 ${min} 个字符`;
    }
    return true;
  },

  maxLength: (max, message) => value => {
    if (!value) return true;
    if (value.length > max) {
      return message || `最多允许 ${max} 个字符`;
    }
    return true;
  },

  pattern:
    (regex, message = '格式不正确') =>
    value => {
      if (!value) return true;
      if (!regex.test(value)) {
        return message;
      }
      return true;
    },

  url: (value, message = '请输入有效的 URL') => {
    if (!value) return true;
    try {
      new URL(value);
      return true;
    } catch {
      return message;
    }
  },

  match: (fieldName, message) => (value, formData) => {
    if (value !== formData[fieldName]) {
      return message || '两次输入不一致';
    }
    return true;
  },
};

export default useForm;
