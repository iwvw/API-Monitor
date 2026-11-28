import { describe, expect, it } from 'vitest';
import { SERVER_CARD_ROW_HEIGHT_REM, SERVER_CARD_WIDE_MIN_REM } from './constants.js';
import { getRootFontSizePx, getServerCardRowHeight } from './utils.js';
import { getContainerAutoUpdateBadge, getLoadTone, matchContainerAutoUpdateLabel } from './serverHelpers.js';

const ROOT_FONT_SIZE = 16;
const NARROW_ROW_HEIGHT = Math.round(SERVER_CARD_ROW_HEIGHT_REM.narrow * ROOT_FONT_SIZE);
const WIDE_ROW_HEIGHT = Math.round(SERVER_CARD_ROW_HEIGHT_REM.wide * ROOT_FONT_SIZE);
const CARD_BORDER = 2;

describe('getRootFontSizePx', () => {
  it('node 环境无 document 时回退到 16px', () => {
    expect(getRootFontSizePx()).toBe(ROOT_FONT_SIZE);
  });
});

describe('getServerCardRowHeight', () => {
  it('两列布局下的卡片宽度改用两行头部行高', () => {
    expect(getServerCardRowHeight({ colWidth: 530 })).toBe(NARROW_ROW_HEIGHT);
  });

  it('单列全宽卡片保持单行头部行高', () => {
    expect(getServerCardRowHeight({ colWidth: 1200 })).toBe(WIDE_ROW_HEIGHT);
  });

  it('断点扣除卡片左右 1px 边框，按内容盒宽度判定', () => {
    const boundary = SERVER_CARD_WIDE_MIN_REM * ROOT_FONT_SIZE + CARD_BORDER;
    expect(getServerCardRowHeight({ colWidth: boundary - 1 })).toBe(NARROW_ROW_HEIGHT);
    expect(getServerCardRowHeight({ colWidth: boundary })).toBe(WIDE_ROW_HEIGHT);
  });
});

describe('matchContainerAutoUpdateLabel', () => {
  it('命中 key=value 标签', () => {
    expect(matchContainerAutoUpdateLabel({ 'auto-update': 'true' }, 'auto-update=true')).toBe(true);
    expect(matchContainerAutoUpdateLabel({ 'auto-update': 'false' }, 'auto-update=true')).toBe(false);
  });

  it('仅指定 key 时只要标签存在即命中', () => {
    expect(matchContainerAutoUpdateLabel({ 'auto-update': 'anything' }, 'auto-update')).toBe(true);
    expect(matchContainerAutoUpdateLabel({}, 'auto-update')).toBe(false);
  });

  it('空表达式或非法 labels 不命中', () => {
    expect(matchContainerAutoUpdateLabel({ 'auto-update': 'true' }, '')).toBe(false);
    expect(matchContainerAutoUpdateLabel(null, 'auto-update=true')).toBe(false);
  });
});

describe('getContainerAutoUpdateBadge', () => {
  it('无结果时展示自动更新标记', () => {
    expect(getContainerAutoUpdateBadge(null)).toMatchObject({ label: '自动更新' });
  });

  it('按结果状态映射徽标', () => {
    expect(getContainerAutoUpdateBadge({ status: 'updated' }).variant).toBe('success');
    expect(getContainerAutoUpdateBadge({ status: 'failed' }).variant).toBe('error');
    expect(getContainerAutoUpdateBadge({ status: 'up-to-date' }).label).toBe('自动·已最新');
    expect(getContainerAutoUpdateBadge({ status: 'skipped' }).label).toBe('自动·跳过');
  });
});

describe('getLoadTone', () => {
  it('有核数时按每核负载分档', () => {
    expect(getLoadTone('0.2 0.3 0.4', { Cores: 4 })).toBe('success');
    expect(getLoadTone('3.2 3.0 2.8', { Cores: 4 })).toBe('warning');
    expect(getLoadTone('4.5 4.2 4.0', { Cores: 4 })).toBe('danger');
  });

  it('无核数时退回绝对阈值', () => {
    expect(getLoadTone('0.5', {})).toBe('success');
    expect(getLoadTone('1.5', {})).toBe('warning');
    expect(getLoadTone('2.5', {})).toBe('danger');
  });

  it('负载缺失返回 neutral', () => {
    expect(getLoadTone('-', { Cores: 4 })).toBe('neutral');
    expect(getLoadTone('', {})).toBe('neutral');
    expect(getLoadTone(null, { Cores: 4 })).toBe('neutral');
  });
});
