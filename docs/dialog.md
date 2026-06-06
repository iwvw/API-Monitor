# Dialog

A window overlaid on either the primary window or another dialog window, rendering the content underneath inert.

**Source:** [GitHub](https://github.com/cloudflare/kumo/blob/main/packages/kumo/src/components/dialog/dialog.tsx)

**Base UI:** [Documentation](https://base-ui.com/react/components/dialog)

---

```
import { Dialog, Button } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogWithActionsDemo() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Delete</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Modal Title
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="text-kumo-subtle">
          Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do
          eiusmod tempor incididunt ut labore et dolore magna aliqua.
        </Dialog.Description>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Dialog.Close
            render={(props) => (
              <Button variant="destructive" {...props}>
                Delete
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

## [Installation](#installation)

### [Barrel](#barrel)

```
import { Dialog } from "@cloudflare/kumo";
```

### [Granular](#granular)

```
import { Dialog } from "@cloudflare/kumo/components/dialog";
```

## [Usage](#usage)

```
import { Dialog, Button } from "@cloudflare/kumo";

export default function Example() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Open</Button>} />
      <Dialog>
        <Dialog.Title>Dialog Title</Dialog.Title>
        <Dialog.Description>Dialog content goes here.</Dialog.Description>
        <div className="flex justify-end gap-2 mt-4">
          <Dialog.Close
            render={(p) => (
              <Button variant="secondary" {...p}>
                Cancel
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

## [Dialog vs Alert Dialog](#dialog-vs-alert-dialog)

The Dialog component supports two ARIA roles to properly convey semantic meaning to assistive technologies:

| Role | Use Case | Behavior |
| --- | --- | --- |
| 
`role="dialog"` (default)

 | 

General-purpose modals, forms, content display

 | Dismissible by default |
| `role="alertdialog"` | 

Destructive actions, confirmations, critical warnings

 | Requires explicit user acknowledgment |

## [Examples](#examples)

### [Basic Dialog](#basic-dialog)

```
import { Dialog, Button } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogBasicDemo() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Click me</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Modal Title
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="text-kumo-subtle">
          Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do
          eiusmod tempor incididunt ut labore et dolore magna aliqua.
        </Dialog.Description>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [Alert Dialog (`role="alertdialog"`)](#alert-dialog-rolealertdialog)

For destructive or confirmation dialogs, use `role="alertdialog"` on `Dialog.Root`. This provides proper accessibility semantics by rendering the dialog with `role="alertdialog"` instead of `role="dialog"`.

When to use `role="alertdialog"`:

-   Destructive actions (delete, discard, remove)
-   Confirmation flows requiring explicit user acknowledgment
-   Actions that cannot be undone
-   Critical warnings or errors

```
import { Dialog, Button } from "@cloudflare/kumo";
import { Warning } from "@phosphor-icons/react";

/**
 * Alert dialog for destructive actions that uses role="alertdialog".
 * This provides proper accessibility semantics for confirmation flows.
 */
export function DialogAlertDemo() {
  return (
    <Dialog.Root role="alertdialog">
      <Dialog.Trigger
        render={(p) => (
          <Button {...p} variant="destructive">
            Delete Account
          </Button>
        )}
      />
      <Dialog className="p-8">
        <div className="mb-4 flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-kumo-danger/20">
            <Warning size={20} className="text-kumo-danger" weight="fill" />
          </div>
          <Dialog.Title className="text-xl font-semibold">
            Delete Account?
          </Dialog.Title>
        </div>
        <Dialog.Description className="text-kumo-subtle">
          This action cannot be undone. All your data will be permanently
          removed from our servers. Are you sure you want to proceed?
        </Dialog.Description>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Dialog.Close
            render={(props) => (
              <Button variant="destructive" {...props}>
                Delete Account
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [Confirmation Dialog (with `disablePointerDismissal`)](#confirmation-dialog-with-disablepointerdismissal)

For confirmation dialogs that should not be dismissed by clicking outside, use `disablePointerDismissal` on `Dialog.Root`. This can be combined with `role="alertdialog"` for proper accessibility.

```
import { Dialog, Button } from "@cloudflare/kumo";
import { Warning } from "@phosphor-icons/react";

export function DialogConfirmationDemo() {
  return (
    <Dialog.Root disablePointerDismissal>
      <Dialog.Trigger
        render={(p) => (
          <Button {...p} variant="destructive">
            Delete Project
          </Button>
        )}
      />
      <Dialog className="p-8">
        <div className="mb-4 flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-kumo-danger/20">
            <Warning size={20} className="text-kumo-danger" />
          </div>
          <Dialog.Title className="text-xl font-semibold">
            Delete Project?
          </Dialog.Title>
        </div>
        <Dialog.Description className="text-kumo-subtle">
          This action cannot be undone. This will permanently delete the project
          and all associated data.
        </Dialog.Description>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Dialog.Close
            render={(props) => (
              <Button variant="destructive" {...props}>
                Delete
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [With Actions](#with-actions)

```
import { Dialog, Button } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogWithActionsDemo() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Delete</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Modal Title
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="text-kumo-subtle">
          Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do
          eiusmod tempor incididunt ut labore et dolore magna aliqua.
        </Dialog.Description>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Dialog.Close
            render={(props) => (
              <Button variant="destructive" {...props}>
                Delete
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [With Select](#with-select)

Dialog containing a Select dropdown.

```
import { Dialog, Button, Select } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogWithSelectDemo() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Open Form</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Create Resource
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Select a region for your new resource.
        </Dialog.Description>
        <Select
          className="w-full"
          renderValue={(v) =>
            regions.find((r) => r.value === v)?.label ?? "Select region..."
          }
        >
          {regions.map((region) => (
            <Select.Option key={region.value} value={region.value}>
              {region.label}
            </Select.Option>
          ))}
        </Select>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Button variant="primary">Create</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [With Combobox](#with-combobox)

Dialog containing a Combobox for searchable selection.

```
import { useState } from "react";
import { Dialog, Button, Combobox } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogWithComboboxDemo() {
  const [value, setValue] = useState<{ value: string; label: string } | null>(
    null,
  );

  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Open Form</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Create Resource
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Search and select a region for your new resource.
        </Dialog.Description>
        <Combobox value={value} onValueChange={setValue} items={regions}>
          <Combobox.TriggerInput
            className="w-full"
            placeholder="Search regions..."
          />
          <Combobox.Content>
            <Combobox.Empty>No regions found</Combobox.Empty>
            <Combobox.List>
              {(item: { value: string; label: string }) => (
                <Combobox.Item key={item.value} value={item}>
                  {item.label}
                </Combobox.Item>
              )}
            </Combobox.List>
          </Combobox.Content>
        </Combobox>
        <div className="mt-8 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Cancel
              </Button>
            )}
          />
          <Button variant="primary">Create</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

### [With Dropdown](#with-dropdown)

Dialog containing a Dropdown menu.

```
import { Dialog, Button, DropdownMenu } from "@cloudflare/kumo";
import { X } from "@phosphor-icons/react";

export function DialogWithDropdownDemo() {
  return (
    <Dialog.Root>
      <Dialog.Trigger render={(p) => <Button {...p}>Open Form</Button>} />
      <Dialog className="p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Resource Actions
          </Dialog.Title>
          <Dialog.Close
            aria-label="Close"
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                icon={<X />}
                aria-label="Close"
              />
            )}
          />
        </div>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Choose an action for the selected resource.
        </Dialog.Description>
        <DropdownMenu>
          <DropdownMenu.Trigger render={<Button>Actions</Button>} />
          <DropdownMenu.Content>
            <DropdownMenu.Item>Edit</DropdownMenu.Item>
            <DropdownMenu.Item>Duplicate</DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item variant="danger">Delete</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu>
        <div className="mt-8 flex justify-end">
          <Dialog.Close
            render={(props) => (
              <Button variant="secondary" {...props}>
                Close
              </Button>
            )}
          />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
```

## [API Reference](#api-reference)

### [Dialog](#dialog)

The main dialog container that renders the modal overlay and popup.

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| className | `string` | \- | Additional CSS classes merged via \`cn()\`. |
| children | `ReactNode` | \- | Dialog content (typically Title, Description, Close, and action buttons). |
| container | `PortalContainer` | \- | Container element for the portal. Use this to render the dialog inside a Shadow DOM or custom container. Overrides \`KumoPortalProvider\` context. |
| size | `"base" | "sm" | "lg" | "xl"` | `"base"` | Dialog width. - \`"sm"\` — Small (min 288px) for simple confirmations - \`"base"\` — Default (min 384px) - \`"lg"\` — Large (min 512px) for complex content - \`"xl"\` — Extra large (min 768px) for detailed views |

### [Dialog.Root](#dialogroot)

Controls the open state of the dialog. Doesn’t render its own HTML element.

| Prop | Type | Default | Description |
| --- | --- | --- | --- |
| role | ”dialog” | “alertdialog" | "dialog” | 
The ARIA role for the dialog. Use `"alertdialog"` for destructive or confirmation flows.

 |
| disablePointerDismissal | boolean | false | 

When true, prevents the dialog from being dismissed by clicking outside.

 |

| Prop | Type | Default |
| --- | --- | --- |

No component-specific props. Accepts standard HTML attributes.

### [Dialog.Trigger](#dialogtrigger)

A button that opens the dialog when clicked.

| Prop | Type | Default |
| --- | --- | --- |

No component-specific props. Accepts standard HTML attributes.

### [Dialog.Title](#dialogtitle)

A heading that labels the dialog for accessibility.

| Prop | Type | Default |
| --- | --- | --- |

No component-specific props. Accepts standard HTML attributes.

### [Dialog.Description](#dialogdescription)

A paragraph providing additional context about the dialog.

| Prop | Type | Default |
| --- | --- | --- |

No component-specific props. Accepts standard HTML attributes.

### [Dialog.Close](#dialogclose)

A button that closes the dialog when clicked.

| Prop | Type | Default |
| --- | --- | --- |

No component-specific props. Accepts standard HTML attributes.