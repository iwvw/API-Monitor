let installed = false;

const isSameOriginApiUrl = (input) => {
  try {
    const raw = typeof input === 'string' ? input : input?.url;
    if (!raw || typeof window === 'undefined') return false;
    const url = new URL(raw, window.location.origin);
    return url.origin === window.location.origin && url.pathname.startsWith('/api/');
  } catch {
    return false;
  }
};

export function installAuthGuard(onUnauthorized) {
  if (installed || typeof onUnauthorized !== 'function') return;
  if (typeof window === 'undefined' || typeof window.fetch !== 'function') return;
  installed = true;

  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input, init) => {
    const response = await nativeFetch(input, init);
    if (response.status === 401 && isSameOriginApiUrl(input)) {
      onUnauthorized();
    }
    return response;
  };

  import('axios')
    .then((mod) => {
      const axios = mod?.default;
      if (!axios?.interceptors?.response?.use) return;
      axios.interceptors.response.use(
        (response) => response,
        (error) => {
          if (error?.response?.status === 401 && isSameOriginApiUrl(error?.config?.url)) {
            onUnauthorized();
          }
          return Promise.reject(error);
        },
      );
    })
    .catch(() => {});
}
