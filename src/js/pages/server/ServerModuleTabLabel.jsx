import { SERVER_MODULE_TAB_ICON_CLASS } from './constants.js';

export function ServerModuleTabLabel({ icon: Icon, children, short, badge = null }) {
  return (
    <span className="inline-flex h-5 items-center gap-1.5 whitespace-nowrap">
      <Icon className={SERVER_MODULE_TAB_ICON_CLASS} />
      <span className="hidden cq-sm:inline">{children}</span>
      <span className="cq-sm:hidden">{short || children}</span>
      {badge !== null && (
        <span className="inline-flex h-4 min-w-4 items-center justify-center rounded bg-brand/10 px-1 text-[10px] font-semibold leading-none text-brand">
          {badge}
        </span>
      )}
    </span>
  );
}
