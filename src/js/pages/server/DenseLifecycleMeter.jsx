import React from 'react';
import { DenseUsageMeter } from './DenseUsageMeter.jsx';

function DenseLifecycleMeterComponent({ lifecycle, muted = false }) {
  return (
    <DenseUsageMeter
      label="剩余"
      value={lifecycle.remainingPercent}
      detail={lifecycle.label}
      indicatorClassName={lifecycle.indicatorClassName}
      muted={muted}
    />
  );
}

export const DenseLifecycleMeter = React.memo(DenseLifecycleMeterComponent, (prev, next) => (
  prev.lifecycle?.remainingPercent === next.lifecycle?.remainingPercent
  && prev.lifecycle?.label === next.lifecycle?.label
  && prev.lifecycle?.indicatorClassName === next.lifecycle?.indicatorClassName
  && prev.muted === next.muted
));
