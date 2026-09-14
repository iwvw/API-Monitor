import React, {
  Children,
  createContext,
  isValidElement,
  useContext,
  useRef,
  useState,
} from 'react';
import { Drawer as DrawerBase } from '@base-ui/react/drawer';
import { ScrollArea as ScrollAreaBase } from '@base-ui/react/scroll-area';
import { useMediaQuery } from '@base-ui/react/unstable-use-media-query';
import { X } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerCard } from '@cloudflare/kumo/components/layer-card';
import { Text } from '@cloudflare/kumo/components/text';
import { cn } from '@cloudflare/kumo/utils';

const KUMO_LAYER_DIALOG_VARIANTS = {
  size: {
    sm: { classes: 'sm:max-w-md' },
    base: { classes: 'sm:max-w-xl' },
    lg: { classes: 'sm:max-w-2xl' },
    xl: { classes: 'sm:max-w-3xl' },
  },
  verticalAlign: {
    top: { classes: 'sm:items-start sm:pt-16 sm:pb-6' },
    center: { classes: 'sm:items-center sm:py-6' },
  },
};

const DEFAULTS = { size: 'base', verticalAlign: 'center' };

const resolveVariant = (variants, value, fallback) => {
  const resolved = value && variants[value] ? value : fallback;
  return variants[resolved];
};

const DesktopContext = createContext(false);
const DismissDisabledContext = createContext(false);
const AlertContext = createContext(false);

const BodySlotsContext = createContext({
  title: null,
  description: null,
  showCloseButton: false,
  closeLabel: '',
});

const USER_DISMISSAL_REASONS = new Set([
  'close-press',
  'close-watcher',
  'escape-key',
  'outside-press',
  'swipe',
  'trigger-press',
]);

function LayerDialogRootImpl({
  alert,
  children,
  dismissDisabled = false,
  onOpenChange,
  disablePointerDismissal,
  modal,
  ...props
}) {
  const isDesktop = useMediaQuery('(min-width: 640px)', {
    defaultMatches: false,
  });

  const handleOpenChange = (open, eventDetails) => {
    if (
      !open &&
      dismissDisabled &&
      USER_DISMISSAL_REASONS.has(eventDetails.reason)
    ) {
      eventDetails.cancel();
      return;
    }
    onOpenChange?.(open, eventDetails);
  };

  const rootProps = {
    ...props,
    disablePointerDismissal: alert || dismissDisabled || disablePointerDismissal,
    modal: alert ? true : modal,
    onOpenChange: handleOpenChange,
  };

  return (
    <AlertContext.Provider value={alert}>
      <DesktopContext.Provider value={isDesktop}>
        <DismissDisabledContext.Provider value={dismissDisabled}>
          <DrawerBase.Root {...rootProps}>{children}</DrawerBase.Root>
        </DismissDisabledContext.Provider>
      </DesktopContext.Provider>
    </AlertContext.Provider>
  );
}

function LayerDialogRoot(props) {
  return <LayerDialogRootImpl {...props} alert={false} />;
}

LayerDialogRoot.displayName = 'LayerDialog.Root';

function LayerDialogAlert(props) {
  return <LayerDialogRootImpl {...props} alert />;
}

LayerDialogAlert.displayName = 'LayerDialog.Alert';

function LayerDialogTrigger(props) {
  return <DrawerBase.Trigger {...props} />;
}

LayerDialogTrigger.displayName = 'LayerDialog.Trigger';

function collectSlot(children, type) {
  const matches = children.filter(
    (child) => isValidElement(child) && child.type === type
  );
  return { element: matches[0], count: matches.length };
}

function LayerDialogContent({
  children,
  closeLabel,
  container,
  size = DEFAULTS.size,
  verticalAlign = DEFAULTS.verticalAlign,
}) {
  const isDesktop = useContext(DesktopContext);
  const dismissDisabled = useContext(DismissDisabledContext);
  const isAlert = useContext(AlertContext);
  const childArray = Children.toArray(children);
  const title = collectSlot(childArray, LayerDialogTitle);
  const description = collectSlot(childArray, LayerDialogDescription);
  const body = collectSlot(childArray, LayerDialogBody);
  const actions = collectSlot(childArray, LayerDialogActions);
  const hasInvalidChildren = childArray.some(
    (child) =>
      !isValidElement(child) ||
      (child.type !== LayerDialogTitle &&
        child.type !== LayerDialogDescription &&
        child.type !== LayerDialogBody &&
        child.type !== LayerDialogActions)
  );

  if (
    hasInvalidChildren ||
    title.count !== 1 ||
    body.count !== 1 ||
    description.count > 1 ||
    actions.count > 1 ||
    (isAlert && actions.count !== 1)
  ) {
    throw new Error(
      isAlert
        ? 'LayerDialog.Alert requires exactly one direct LayerDialog.Title, LayerDialog.Body, and LayerDialog.Actions, with an optional direct LayerDialog.Description.'
        : 'LayerDialog.Content requires exactly one direct LayerDialog.Title and LayerDialog.Body, with an optional direct LayerDialog.Description and LayerDialog.Actions.'
    );
  }

  const sizeConfig = resolveVariant(
    KUMO_LAYER_DIALOG_VARIANTS.size,
    size,
    DEFAULTS.size
  );
  const verticalAlignConfig = resolveVariant(
    KUMO_LAYER_DIALOG_VARIANTS.verticalAlign,
    verticalAlign,
    DEFAULTS.verticalAlign
  );

  const bodySlots = {
    title: title.element,
    description: description.element ?? null,
    showCloseButton: actions.count === 0,
    closeLabel: closeLabel ?? '关闭',
  };

  return (
    <DrawerBase.Portal container={container}>
      <DrawerBase.Backdrop className="fixed inset-0 bg-kumo-recessed opacity-80 transition-opacity duration-[450ms] ease-[cubic-bezier(0.32,0.72,0,1)] data-[ending-style]:opacity-0 data-[ending-style]:duration-[calc(var(--drawer-swipe-strength)*400ms)] data-[starting-style]:opacity-0 data-[swiping]:duration-0 motion-reduce:transition-none sm:duration-200 sm:data-[ending-style]:duration-200" />
      <DrawerBase.Viewport
        className={cn(
          'fixed inset-0 flex items-end justify-center sm:px-4',
          verticalAlignConfig.classes
        )}
        data-base-ui-swipe-ignore={
          isDesktop || dismissDisabled || isAlert ? '' : undefined
        }
      >
        <DrawerBase.Popup
          render={isAlert ? <div role="alertdialog" /> : <div />}
          className={cn(
            'fixed inset-x-0 bottom-0 flex max-h-[85dvh] min-h-0 w-full max-w-none [transform:translate3d(0,var(--drawer-swipe-movement-y,0px),0)] transform-gpu overflow-visible transition-[transform,opacity] duration-[450ms] ease-[cubic-bezier(0.32,0.72,0,1)] will-change-transform outline-none data-[ending-style]:[transform:translate3d(0,100%,0)] data-[ending-style]:duration-[calc(var(--drawer-swipe-strength)*400ms)] data-[starting-style]:[transform:translate3d(0,100%,0)] data-[swiping]:duration-0 data-[swiping]:select-none motion-reduce:transition-none sm:static sm:max-h-full sm:[transform:translate3d(0,0,0)] sm:duration-200 sm:data-[ending-style]:[transform:translate3d(0,8px,0)] sm:data-[ending-style]:opacity-0 sm:data-[ending-style]:duration-200 sm:data-[starting-style]:[transform:translate3d(0,8px,0)] sm:data-[starting-style]:opacity-0',
            sizeConfig.classes
          )}
        >
          <LayerCard className="flex max-h-[85dvh] min-h-0 w-full flex-col overflow-hidden rounded-none bg-kumo-elevated p-1.5 shadow-[0_20px_25px_-5px_rgb(0_0_0/0.03),0_8px_10px_-6px_rgb(0_0_0/0.03)] max-sm:border-t max-sm:border-kumo-hairline max-sm:shadow-xs max-sm:ring-0 sm:max-h-full sm:rounded-xl">
            {!isDesktop && !isAlert && (
              <div aria-hidden className="flex justify-center pt-1.5 pb-3">
                <div className="h-1 w-10 rounded-full bg-kumo-fill" />
              </div>
            )}
            <DrawerBase.Content className="flex min-h-0 flex-col overflow-visible">
              <BodySlotsContext.Provider value={bodySlots}>
                {body.element}
              </BodySlotsContext.Provider>
              {actions.element}
            </DrawerBase.Content>
          </LayerCard>
        </DrawerBase.Popup>
      </DrawerBase.Viewport>
    </DrawerBase.Portal>
  );
}

LayerDialogContent.displayName = 'LayerDialog.Content';

function LayerDialogTitle({ children }) {
  const title = (props) => (
    <Text {...props} as="h2" variant="heading" DANGEROUS_className="font-medium">
      {children}
    </Text>
  );
  return <DrawerBase.Title render={title} />;
}

LayerDialogTitle.displayName = 'LayerDialog.Title';

function LayerDialogDescription({ children }) {
  const description = (props) => (
    <Text {...props} as="p" variant="secondary">
      {children}
    </Text>
  );
  return <DrawerBase.Description render={description} />;
}

LayerDialogDescription.displayName = 'LayerDialog.Description';

const SCROLL_THRESHOLD = 8;
const CONDENSE_MIN_OVERFLOW = 16;

function LayerDialogBody({ children }) {
  const [condensed, setCondensed] = useState(false);
  const descriptionClipRef = useRef(null);
  const dismissDisabled = useContext(DismissDisabledContext);
  const { title, description, showCloseButton, closeLabel } =
    useContext(BodySlotsContext);

  const handleScroll = (event) => {
    const { scrollTop, scrollHeight, clientHeight } = event.currentTarget;
    const scrolled = scrollTop > SCROLL_THRESHOLD;

    if (!scrolled) {
      setCondensed(false);
      return;
    }
    if (condensed) return;

    const descriptionHeight = descriptionClipRef.current?.offsetHeight ?? 0;
    const overflowAfterCollapse =
      scrollHeight - clientHeight - descriptionHeight;
    if (overflowAfterCollapse > CONDENSE_MIN_OVERFLOW) setCondensed(true);
  };

  const content = description ? (
    children
  ) : (
    <DrawerBase.Description render={<div />}>{children}</DrawerBase.Description>
  );

  return (
    <LayerCard.Primary className="min-h-0 flex-1 gap-0 p-0">
      <div className="z-10 flex shrink-0 items-start justify-between gap-4 rounded-t-lg bg-kumo-base px-4.5 py-4">
        <div className="flex min-w-0 flex-col">
          {title}
          {description && (
            <div
              className={cn(
                'grid transition-[grid-template-rows,opacity] duration-200 ease-out motion-reduce:transition-none',
                condensed
                  ? 'grid-rows-[0fr] opacity-0'
                  : 'grid-rows-[1fr] opacity-100'
              )}
              data-condensed={condensed || undefined}
            >
              <div ref={descriptionClipRef} className="min-h-0 overflow-hidden">
                <div className="pt-1">{description}</div>
              </div>
            </div>
          )}
        </div>
        {showCloseButton && (
          <LayerDialogIconClose disabled={dismissDisabled} label={closeLabel} />
        )}
      </div>
      <ScrollAreaBase.Root className="relative flex min-h-0 flex-1 flex-col">
        <ScrollAreaBase.Viewport
          className="min-h-0 flex-1 overscroll-none [mask-image:linear-gradient(to_bottom,transparent_0,black_min(24px,var(--scroll-area-overflow-y-start,24px)),black_calc(100%-min(24px,var(--scroll-area-overflow-y-end,24px))),transparent_100%)]"
          onScroll={handleScroll}
        >
          <ScrollAreaBase.Content className="px-4.5 pb-4.5">
            {content}
          </ScrollAreaBase.Content>
        </ScrollAreaBase.Viewport>
        <ScrollAreaBase.Scrollbar
          keepMounted
          orientation="vertical"
          className="my-1.5 mr-0.5 hidden w-2 p-px opacity-0 transition-opacity data-[has-overflow-y]:block data-[hovering]:opacity-100 data-[scrolling]:opacity-100"
        >
          <ScrollAreaBase.Thumb className="w-full rounded-full bg-kumo-contrast opacity-10 transition-opacity hover:opacity-20 active:opacity-30" />
        </ScrollAreaBase.Scrollbar>
      </ScrollAreaBase.Root>
    </LayerCard.Primary>
  );
}

LayerDialogBody.displayName = 'LayerDialog.Body';

function LayerDialogIconClose({ disabled, label }) {
  const close = (closeProps) => (
    <Button
      {...closeProps}
      aria-label={label}
      className="-mt-1.5 -mr-1.5 rounded-lg"
      disabled={disabled}
      icon={<X size={15} />}
      shape="square"
      size="sm"
      variant="ghost"
    />
  );
  return <DrawerBase.Close render={close} />;
}

function LayerDialogPrimary({
  children,
  loading,
  variant = 'primary',
  ...props
}) {
  return (
    <Button {...props} loading={loading} variant={variant}>
      {children}
    </Button>
  );
}

LayerDialogPrimary.displayName = 'LayerDialog.Actions.Primary';

function LayerDialogDismiss({ disabled, label }) {
  const close = (closeProps) => (
    <Button
      {...closeProps}
      className="hover:bg-kumo-fill/50"
      disabled={disabled}
      variant="ghost"
    >
      {label}
    </Button>
  );
  return <DrawerBase.Close render={close} />;
}

const LayerDialogActions = Object.assign(
  function LayerDialogActions({ children, dismissLabel }) {
    const dismissDisabled = useContext(DismissDisabledContext);
    const isAlert = useContext(AlertContext);
    const label = dismissLabel ?? (isAlert ? '取消' : '关闭');

    if (!isValidElement(children) || children.type !== LayerDialogPrimary) {
      throw new Error(
        'LayerDialog.Actions requires exactly one direct LayerDialog.Actions.Primary.'
      );
    }

    return (
      <div className="flex w-full shrink-0 items-center justify-between gap-2 pt-1.75">
        <LayerDialogDismiss disabled={dismissDisabled} label={label} />
        {children}
      </div>
    );
  },
  { Primary: LayerDialogPrimary }
);

export const LayerDialog = Object.assign(LayerDialogRoot, {
  Root: LayerDialogRoot,
  Alert: LayerDialogAlert,
  Trigger: LayerDialogTrigger,
  Content: LayerDialogContent,
  Title: LayerDialogTitle,
  Description: LayerDialogDescription,
  Body: LayerDialogBody,
  Actions: LayerDialogActions,
});

export default LayerDialog;
