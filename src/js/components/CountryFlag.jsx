import React, { useEffect, useState } from 'react';
import getUnicodeFlagIcon from 'country-flag-icons/unicode';

let cachedEmojiFlagSupport = null;

const normalizeCountryCode = (countryCode) => {
  const code = String(countryCode || '').trim();
  return /^[a-z]{2}$/i.test(code) ? code : '';
};

const detectEmojiFlagSupport = () => {
  if (cachedEmojiFlagSupport !== null) return cachedEmojiFlagSupport;
  if (typeof document === 'undefined') {
    cachedEmojiFlagSupport = false;
    return cachedEmojiFlagSupport;
  }

  const canvas = document.createElement('canvas');
  canvas.width = 32;
  canvas.height = 32;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    cachedEmojiFlagSupport = false;
    return cachedEmojiFlagSupport;
  }

  ctx.textBaseline = 'top';
  ctx.font = '28px Arial';
  ctx.fillText('🇺🇸', 0, 0);
  cachedEmojiFlagSupport = ctx.getImageData(16, 16, 1, 1).data[3] !== 0;
  return cachedEmojiFlagSupport;
};

export default function CountryFlag({ countryCode, className = '' }) {
  const code = normalizeCountryCode(countryCode);
  const [supportsEmojiFlags, setSupportsEmojiFlags] = useState(() => cachedEmojiFlagSupport === true);

  useEffect(() => {
    setSupportsEmojiFlags(detectEmojiFlagSupport());
  }, []);

  if (!code) return null;

  const forceSvgFlag = typeof window !== 'undefined' && window.ForceUseSvgFlag === true;
  if (forceSvgFlag || !supportsEmojiFlags) {
    return (
      <span
        aria-label={code.toUpperCase()}
        className={`fi fi-${code.toLowerCase()} inline-block rounded-xs ${className}`}
      />
    );
  }

  return (
    <span aria-label={code.toUpperCase()} className={`inline-block leading-none ${className}`}>
      {getUnicodeFlagIcon(code.toUpperCase())}
    </span>
  );
}
