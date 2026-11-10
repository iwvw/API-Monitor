import { useEffect, useRef } from 'react';

export function useCloudflareSpotlight() {
  const surfaceRef = useRef(null);

  useEffect(() => {
    // 系统开启「减弱动态效果」时禁用背景光斑跟随，避免登录页整屏鼠标重绘。
    if (typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      return undefined;
    }
    const surface = surfaceRef.current;
    if (!surface) return undefined;
    let animationFrame = 0;
    let latestEvent = null;

    const hideSpotlight = () => {
      latestEvent = null;
      if (animationFrame) {
        cancelAnimationFrame(animationFrame);
        animationFrame = 0;
      }
      surface.style.setProperty('--cf-ai-spotlight-x', '-1000px');
      surface.style.setProperty('--cf-ai-spotlight-y', '-1000px');
      surface.style.setProperty('--cf-ai-spotlight-opacity', '0');
    };

    const paintSpotlight = () => {
      animationFrame = 0;
      if (!latestEvent) return;

      const rect = surface.getBoundingClientRect();
      const insideSurface =
        latestEvent.clientX >= rect.left &&
        latestEvent.clientX <= rect.right &&
        latestEvent.clientY >= rect.top &&
        latestEvent.clientY <= rect.bottom;

      if (!insideSurface) {
        hideSpotlight();
        return;
      }

      surface.style.setProperty('--cf-ai-spotlight-x', `${latestEvent.clientX - rect.left}px`);
      surface.style.setProperty('--cf-ai-spotlight-y', `${latestEvent.clientY - rect.top}px`);
      surface.style.setProperty('--cf-ai-spotlight-opacity', '1');
    };

    const showSpotlight = (event) => {
      latestEvent = {
        clientX: event.clientX,
        clientY: event.clientY,
      };

      if (!animationFrame) {
        animationFrame = requestAnimationFrame(paintSpotlight);
      }
    };

    surface.addEventListener('pointermove', showSpotlight);
    surface.addEventListener('pointerleave', hideSpotlight);
    window.addEventListener('mousemove', showSpotlight);

    return () => {
      surface.removeEventListener('pointermove', showSpotlight);
      surface.removeEventListener('pointerleave', hideSpotlight);
      window.removeEventListener('mousemove', showSpotlight);
      if (animationFrame) cancelAnimationFrame(animationFrame);
    };
  }, []);

  return surfaceRef;
}
