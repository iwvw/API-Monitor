import { describe, expect, it, vi } from 'vitest';
import { shouldIgnoreRowDoubleClick, handleEditableRowDoubleClick } from './tableInteractions.js';

const IGNORE_SELECTOR =
  'button,a,input,select,textarea,[role="button"],[role="checkbox"],[role="menuitem"],[data-row-dblclick-ignore]';

const IGNORE_SELECTORS = [
  'button',
  'a',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="checkbox"]',
  '[role="menuitem"]',
  '[data-row-dblclick-ignore]',
];

const makeEvent = (closestResult) => {
  const target = { closest: vi.fn(() => closestResult) };
  return { target };
};

describe('shouldIgnoreRowDoubleClick', () => {
  it('returns false without an event or target', () => {
    expect(shouldIgnoreRowDoubleClick()).toBe(false);
    expect(shouldIgnoreRowDoubleClick(null)).toBe(false);
    expect(shouldIgnoreRowDoubleClick({})).toBe(false);
  });

  it('returns false when the target lacks closest', () => {
    expect(shouldIgnoreRowDoubleClick({ target: {} })).toBe(false);
  });

  it('queries the combined ignore selector list', () => {
    const event = makeEvent({ interactive: true });
    expect(shouldIgnoreRowDoubleClick(event)).toBe(true);
    expect(event.target.closest).toHaveBeenCalledWith(IGNORE_SELECTOR);
  });

  it('covers every interactive selector in the ignore list', () => {
    for (const selector of IGNORE_SELECTORS) {
      expect(IGNORE_SELECTOR.split(',').map((part) => part.trim())).toContain(selector);
    }
  });

  it('returns false for ordinary elements', () => {
    expect(shouldIgnoreRowDoubleClick(makeEvent(null))).toBe(false);
  });
});

describe('handleEditableRowDoubleClick', () => {
  it('calls onEdit for ordinary elements', () => {
    const onEdit = vi.fn();
    handleEditableRowDoubleClick(makeEvent(null), onEdit);
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('skips onEdit for interactive elements', () => {
    const onEdit = vi.fn();
    handleEditableRowDoubleClick(makeEvent({ interactive: true }), onEdit);
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('tolerates a missing callback', () => {
    expect(() => handleEditableRowDoubleClick(makeEvent(null))).not.toThrow();
    expect(() => handleEditableRowDoubleClick()).not.toThrow();
  });
});