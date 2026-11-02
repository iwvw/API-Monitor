import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import { Shield } from '../../components/Icons.jsx';

// ==================== SSL Certificate Panel ====================
function SslCertificatePanel({ monitorId }) {
  const [sslData, setSslData] = useState(null);
  const [sslLoading, setSslLoading] = useState(false);
  const [sslExpanded, setSslExpanded] = useState(false);

  const loadSslInfo = async () => {
    if (sslData) { setSslExpanded(prev => !prev); return; }
    setSslLoading(true);
    setSslExpanded(true);
    try {
      const res = await fetch(`/api/uptime/monitors/${monitorId}/ssl`, {
        headers: { 'Content-Type': 'application/json' }
      });
      const data = await res.json();
      setSslData(data);
    } catch (e) {
      setSslData({ ssl: false, error: e.message });
    } finally {
      setSslLoading(false);
    }
  };

  const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '--';

  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-base overflow-hidden">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={loadSslInfo}
        className="h-auto w-full justify-between rounded-none px-3 py-2 text-left"
      >
        <span className="text-[11px] font-semibold text-kumo-strong uppercaser flex items-center gap-1.5 select-none">
          <Shield className="w-3.5 h-3.5" />
          SSL 证书信息
        </span>
        <span className="text-[10px] text-kumo-subtle">
          {sslLoading ? '加载中...' : sslExpanded ? '收起' : '展开'}
        </span>
      </Button>
      <AnimatedCollapse open={sslExpanded}>
        <div className="px-3 pb-3 space-y-2">
          {sslData && !sslData.ssl && (
            <div className="text-[10px] text-kumo-subtle py-2">{sslData.reason || sslData.error || '无 SSL 证书'}</div>
          )}
          {sslData && sslData.ssl && (() => {
            const daysLeft = sslData.daysLeft;
            let shieldColor = 'text-kumo-success';
            let bgColor = 'bg-kumo-success/10';
            if (daysLeft <= 7) { shieldColor = 'text-kumo-danger'; bgColor = 'bg-kumo-danger/10'; }
            else if (daysLeft <= 30) { shieldColor = 'text-kumo-warning'; bgColor = 'bg-kumo-warning/10'; }
            return (
              <>
                {/* 证书状态概览 */}
                <div className="grid grid-cols-2 cq-md:grid-cols-4 gap-2">
                  <div className={`rounded-md p-2 ${bgColor} flex flex-col`}>
                    <span className="text-[9px] text-kumo-subtle select-none">剩余天数</span>
                    <span className={`text-sm font-semibold font-mono ${shieldColor} flex items-center gap-1`}>
                      <Shield className="w-3 h-3" />
                      {daysLeft} 天
                    </span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">主体 (Subject)</span>
                    <span className="text-[10px] font-semibold text-kumo-strong truncate" title={sslData.subject}>{sslData.subject}</span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">签发机构 (Issuer)</span>
                    <span className="text-[10px] font-semibold text-kumo-strong truncate" title={sslData.issuer}>{sslData.issuer}</span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">有效期</span>
                    <span className="text-[10px] font-mono text-kumo-strong">{formatDate(sslData.notBefore)} ~ {formatDate(sslData.notAfter)}</span>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-2 cq-sm:grid-cols-2">
                  {/* DNS SANs */}
                  {sslData.dnsNames && sslData.dnsNames.length > 0 && (
                    <div className="rounded-md p-2 bg-kumo-recessed">
                      <span className="text-[9px] text-kumo-subtle select-none block mb-1">DNS 备用名称 (SANs)</span>
                      <div className="flex flex-wrap gap-1">
                        {sslData.dnsNames.map((name, i) => (
                          <span key={i} className="text-[9px] px-1.5 py-0.5 rounded bg-kumo-base text-kumo-strong font-mono border border-kumo-line">{name}</span>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* 证书链 */}
                  {sslData.chain && sslData.chain.length > 1 && (
                    <div className="rounded-md p-2 bg-kumo-recessed">
                      <span className="text-[9px] text-kumo-subtle select-none block mb-1.5">证书链 ({sslData.chain.length} 级)</span>
                      <div className="space-y-1">
                        {sslData.chain.map((cert, i) => (
                          <div key={i} className="flex items-center gap-2 text-[9px] font-mono text-kumo-strong">
                            <span className="w-3.5 h-3.5 rounded-full bg-kumo-base border border-kumo-line flex items-center justify-center text-[7px] flex-shrink-0">{i + 1}</span>
                            <span className="truncate flex-1" title={cert.subject}>{cert.subject || '(unnamed)'}</span>
                            <span className="text-kumo-subtle flex-shrink-0">{cert.isCA ? 'CA' : 'Leaf'}</span>
                            <span className="text-kumo-subtle flex-shrink-0">{formatDate(cert.notAfter)}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              </>
            );
          })()}
        </div>
      </AnimatedCollapse>
    </div>
  );
}

export default SslCertificatePanel;
