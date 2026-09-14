import React from 'react';

/* ---------- 点阵背景（复用登录页 .cf-ai-background + surface 光斑） ---------- */
function DotGrid({ surfaceRef }) {
  return (
    <div className="pointer-events-none absolute inset-0" aria-hidden>
      <div ref={surfaceRef} className="cf-ai-background-surface cf-ai-background absolute inset-0" />
    </div>
  );
}

export default DotGrid;

