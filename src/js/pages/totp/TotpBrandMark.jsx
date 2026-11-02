import React from 'react';
import BrandIcon, { getIssuerColor } from '../../components/ui/BrandIcon.jsx';
import { isCustomUploadedIcon, isSVGRepoIcon } from './utils.js';

const TotpBrandMark = ({ issuer, icon, color, size = 'card' }) => {
  const isHeader = size === 'header';
  const isPicker = size === 'picker';
  const markColor = color || getIssuerColor(issuer);
  const remoteIcon = isSVGRepoIcon(icon) || isCustomUploadedIcon(icon);
  return (
    <span
      className={`app-totp-brand-mark ${remoteIcon ? 'app-totp-brand-mark--remote' : 'border border-kumo-line'} ${isPicker ? 'size-9 rounded-lg text-[22px]' : isHeader ? 'size-7 rounded-md text-[17px]' : 'size-7 rounded-md text-[18px]'} flex shrink-0 items-center justify-center`}
      style={{ background: remoteIcon ? 'transparent' : markColor, color: '#fff' }}
    >
      <BrandIcon issuer={issuer} icon={icon} color="inherit" />
    </span>
  );
};

export default TotpBrandMark;
