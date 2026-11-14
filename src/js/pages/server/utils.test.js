import { describe, expect, it } from 'vitest';
import { SERVER_CARD_ROW_HEIGHT_REM, SERVER_CARD_WIDE_MIN_REM } from './constants.js';
import { getRootFontSizePx, getServerCardRowHeight } from './utils.js';

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
