import { useEffect } from 'react';

/**
 * Tracks the visual viewport offset caused by virtual keyboards (Chrome on iPad/Android).
 * Sets --vvh-offset on <html> so CSS can compensate for the keyboard overlay.
 * No-op on desktop or browsers without visualViewport support.
 */
export function useVisualViewport() {
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    function update() {
      // The offset is the difference between the layout viewport and visual viewport height,
      // accounting for any scroll of the visual viewport (offsetTop).
      const offset = vv!.offsetTop + (window.innerHeight - vv!.height);
      document.documentElement.style.setProperty(
        '--vvh-offset',
        `${offset}px`,
      );
    }

    vv.addEventListener('resize', update);
    vv.addEventListener('scroll', update);
    update();

    return () => {
      vv.removeEventListener('resize', update);
      vv.removeEventListener('scroll', update);
      document.documentElement.style.removeProperty('--vvh-offset');
    };
  }, []);
}
