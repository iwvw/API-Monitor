import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BubbleMap } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { feature } from 'topojson-client';
import worldCountries from 'world-atlas/countries-110m.json';
import { createSiteFontEcharts } from '../../chartFont.js';
import { Minus, Plus, RotateCw } from '../Icons.jsx';

const rawWorldGeoJson = feature(worldCountries, worldCountries.objects.countries);
const WORLD_GEO_JSON = {
  ...rawWorldGeoJson,
  features: rawWorldGeoJson.features
    .filter((f) => f.properties?.name !== 'Antarctica')
    .map((f) => {
      if (f.properties?.name === 'Russia' || f.properties?.name === 'Fiji') {
        const cleanCoords = (coords) => {
          if (typeof coords[0] === 'number') {
            // 负经度（俄罗斯/斐济等跨反经线国家）平移 +360 保持几何连续，
            // 直接锚定 180 会把整条边界压成反经线上的竖条。
            const lon = coords[0] < 0 ? coords[0] + 360 : coords[0];
            const lat = coords[1];
            return [lon, lat];
          }
          return coords.map(cleanCoords);
        };
        return {
          ...f,
          geometry: {
            ...f.geometry,
            coordinates: cleanCoords(f.geometry.coordinates),
          },
        };
      }
      return f;
    }),
};

const STATUS_COLORS = {
  online: '#00A63E',
  offline: '#878787',
  interrupted: '#FC574A',
  degraded: '#F8A054',
  warning: '#F8A054',
};

const STATUS_LABELS = {
  online: '在线',
  offline: '离线',
  interrupted: '中断',
  degraded: '异常',
  warning: '预警',
};

const getDocumentDarkMode = () => {
  if (typeof document === 'undefined') return false;
  const root = document.documentElement;
  return root.dataset.mode === 'dark' || root.classList.contains('dark');
};

const useDocumentDarkMode = () => {
  const [isDarkMode, setIsDarkMode] = useState(getDocumentDarkMode);

  useEffect(() => {
    if (typeof document === 'undefined') return undefined;
    const root = document.documentElement;
    const update = () => setIsDarkMode(getDocumentDarkMode());
    update();
    const observer = new MutationObserver(update);
    observer.observe(root, { attributes: true, attributeFilter: ['data-mode', 'class'] });
    return () => observer.disconnect();
  }, []);

  return isDarkMode;
};

const toFiniteNumber = (value) => {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
};

const firstNumber = (...values) => {
  for (const value of values) {
    const number = toFiniteNumber(value);
    if (number !== null) return number;
  }
  return null;
};

const escapeHtml = (value) => String(value ?? '')
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;');

const getServerCoordinates = (server) => {
  const info = server?.info || {};
  const system = info.system || {};
  const lat = firstNumber(server?.latitude, server?.lat, info.latitude, info.lat, system.latitude, system.lat);
  const lon = firstNumber(server?.longitude, server?.lon, info.longitude, info.lon, system.longitude, system.lon);
  if (lat !== null && lon !== null) {
    if (lat === 0 && lon === 0) return null;
    if (Math.abs(lat) > 90 && Math.abs(lon) <= 90) return { lat: lon, lon: lat };
    return { lat, lon };
  }
  return null;
};

const STATUS_PRIORITY = ['interrupted', 'degraded', 'warning', 'offline', 'online'];

const getGroupStatus = (items) => STATUS_PRIORITY.find((status) => items.some((item) => item.status === status)) || 'offline';

const cleanAutoText = (value) => {
  const text = String(value || '').trim();
  if (!text || text.toLowerCase() === 'auto') return '';
  return text;
};

const getLocationLabel = (items) => {
  const first = items[0] || {};
  const info = first.info || {};
  return cleanAutoText(first.location)
    || cleanAutoText(first.region)
    || cleanAutoText(first.countryCode)
    || cleanAutoText(first.country)
    || cleanAutoText(info.location)
    || cleanAutoText(info.region)
    || cleanAutoText(info.countryCode)
    || cleanAutoText(info.country_code)
    || cleanAutoText(info.country)
    || `${first.lat.toFixed(2)}, ${first.lon.toFixed(2)}`;
};

const formatServerListTooltip = (row) => {
  const servers = Array.isArray(row.servers) ? row.servers : [row];
  const preview = servers.slice(0, 8);
  const remaining = servers.length - preview.length;
  const statusCounts = servers.reduce((acc, server) => {
    acc[server.status] = (acc[server.status] || 0) + 1;
    return acc;
  }, {});
  const statusSummary = STATUS_PRIORITY
    .filter((status) => statusCounts[status])
    .map((status) => `${statusCounts[status]} ${STATUS_LABELS[status] || status}`)
    .join(' · ');

  const listHtml = preview.map((server) => {
    const host = server.host ? `<span style="color:var(--text-color-kumo-subtle);font-size:11px;">${escapeHtml(server.host)}</span>` : '';
    return `<div style="display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px;min-height:22px;padding:2px 0;">
      <div style="display:flex;min-width:0;flex-direction:column;gap:1px;">
        <span style="font-size:12px;color:var(--text-color-kumo-strong);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">${escapeHtml(server.name)}</span>
        ${host}
      </div>
      <span style="width:7px;height:7px;border-radius:999px;background:${STATUS_COLORS[server.status] || '#4290F0'};box-shadow:0 0 0 1px rgba(255,255,255,.45);"></span>
    </div>`;
  }).join('');

  const moreHtml = remaining > 0
    ? `<div style="padding-top:4px;color:var(--text-color-kumo-subtle);font-size:11px;">还有 ${remaining} 台主机</div>`
    : '';

  return `<div style="display:flex;min-width:190px;max-width:260px;flex-direction:column;gap:8px;">
    <div style="display:flex;flex-direction:column;gap:2px;">
      <strong style="font-size:13px;color:var(--text-color-kumo-strong);">${escapeHtml(row.name)}</strong>
      <span style="color:var(--text-color-kumo-subtle);font-size:12px;">${servers.length} 台主机${statusSummary ? ` · ${escapeHtml(statusSummary)}` : ''}</span>
    </div>
    <div style="display:flex;flex-direction:column;border-top:1px solid var(--border-color-kumo-line);padding-top:5px;">${listHtml}${moreHtml}</div>
  </div>`;
};

function ServerLocationMap({
  echarts,
  servers,
  resolveStatus,
  height,
  aspectRatio,
}) {
  const isDarkMode = useDocumentDarkMode();
  const chartRef = useRef(null);
  const siteFontEcharts = useMemo(() => createSiteFontEcharts(echarts), [echarts]);
  const chartHeight = height ?? (aspectRatio ? undefined : 190);
  const points = useMemo(() => {
    const rawPoints = (Array.isArray(servers) ? servers : [])
      .map((server) => {
        const coordinates = getServerCoordinates(server);
        if (!coordinates) return null;
        const info = server?.info || {};
        const status = resolveStatus ? resolveStatus(server) : (server?.online ? 'online' : 'offline');
        return {
          id: server?.id,
          name: server?.name || server?.host || server?.id || '主机',
          host: server?.host || '',
          location: cleanAutoText(server?.location) || cleanAutoText(server?.resolved_country) || cleanAutoText(info.location) || cleanAutoText(info.region),
          region: cleanAutoText(server?.region) || cleanAutoText(info.region),
          countryCode: cleanAutoText(server?.countryCode) || cleanAutoText(server?.country_code) || cleanAutoText(info.countryCode) || cleanAutoText(info.country_code),
          country: cleanAutoText(server?.country) || cleanAutoText(info.country),
          status,
          value: 1,
          ...coordinates,
        };
      })
      .filter(Boolean);

    // Group by coordinates to identify overlapping points
    const groups = {};
    rawPoints.forEach((point) => {
      const key = `${point.lat.toFixed(4)}_${point.lon.toFixed(4)}`;
      if (!groups[key]) {
        groups[key] = [];
      }
      groups[key].push(point);
    });

    return Object.values(groups).map((group) => {
      const first = group[0];
      const status = getGroupStatus(group);
      return {
        ...first,
        id: group.map((item) => item.id).filter(Boolean).join(',') || first.id,
        name: getLocationLabel(group),
        status,
        value: group.length,
        servers: group,
      };
    });
  }, [servers, resolveStatus]);

  const changeZoom = useCallback((factor) => {
    const chart = chartRef.current;
    if (!chart) return;
    chart.dispatchAction({
      type: 'geoRoam',
      componentType: 'geo',
      geoIndex: 0,
      zoom: factor,
      originX: chart.getWidth() / 2,
      originY: chart.getHeight() / 2,
    });
  }, []);

  const resetView = useCallback(() => {
    chartRef.current?.setOption({ geo: { center: null, zoom: 1.15 } });
  }, []);

  return (
    <section className="relative overflow-hidden rounded-md border border-kumo-line bg-kumo-base">
      <div className="absolute right-3 top-3 z-10 flex items-center gap-1 rounded-md border border-kumo-line bg-kumo-base/90 p-1 shadow-sm backdrop-blur-sm">
        <Button
          type="button"
          size="sm"
          variant="ghost"
          shape="square"
          icon={<Plus className="size-3.5" />}
          aria-label="放大地图"
          title="放大地图"
          onClick={() => changeZoom(1.35)}
        />
        <Button
          type="button"
          size="sm"
          variant="ghost"
          shape="square"
          icon={<Minus className="size-3.5" />}
          aria-label="缩小地图"
          title="缩小地图"
          onClick={() => changeZoom(1 / 1.35)}
        />
        <Button
          type="button"
          size="sm"
          variant="ghost"
          shape="square"
          icon={<RotateCw className="size-3.5" />}
          aria-label="重置地图"
          title="重置地图"
          onClick={resetView}
        />
      </div>
      <div className="bg-kumo-recessed/20 px-2 py-1.5">
        <BubbleMap
          ref={chartRef}
          echarts={siteFontEcharts}
          geoJson={WORLD_GEO_JSON}
          mapName="api-monitor-world-hosts"
          data={points}
          lng="lon"
          lat="lat"
          name="name"
          value="value"
          minRadius={5}
          maxRadius={15}
          bubbleColor={(row) => STATUS_COLORS[row.status] || '#4290F0'}
          bubbleBorderColor={isDarkMode ? 'rgba(255,255,255,0.76)' : '#ffffff'}
          bubbleBorderWidth={1}
          height={chartHeight}
          aspectRatio={chartHeight === undefined ? aspectRatio : undefined}
          zoom={1.15}
          roam
          projection={null}
          isDarkMode={isDarkMode}
          tooltipFormatter={formatServerListTooltip}
        />
      </div>
    </section>
  );
}

export default React.memo(ServerLocationMap);
