# Kumo Component Registry

Generated documentation for the Kumo registry used by API-Monitor.

This file is generated from `node_modules/@cloudflare/kumo/ai/component-registry.json`. The raw JSON snapshot is stored next to it as `docs/reference/kumo-component-registry.json`.

- Registry version: `1.0.0`
- Component count: `42`
- Block count: `3`
- Package declaration: `@cloudflare/kumo ^2.5.0`

## Important Notes

- Components are runtime components exported by Kumo unless noted by the package itself.
- Blocks are composite layouts. Current Kumo package docs say blocks are installed via `npx @cloudflare/kumo add <block-name>`; do not assume every block is exported from the barrel.
- `DeleteResource` is exported by the installed package, while `PageHeader` and `ResourceListPage` are present as block source in this package version.
- Use Kumo semantic tokens and Kumo props before local styling overrides.

## Category Index

### Other

- [Autocomplete](#autocomplete)
- [CloudflareLogo](#cloudflarelogo)
- [DatePicker](#datepicker)
- [Label](#label)
- [Link](#link)
- [SensitiveInput](#sensitiveinput)
- [Sidebar](#sidebar)
- [Table](#table)
- [TableOfContents](#tableofcontents)
- [DeleteResource](#deleteresource)

### Display

- [Badge](#badge)
- [Breadcrumbs](#breadcrumbs)
- [Code](#code)
- [Collapsible](#collapsible)
- [Empty](#empty)
- [LayerCard](#layercard)
- [Meter](#meter)
- [Text](#text)

### Feedback

- [Banner](#banner)
- [Loader](#loader)
- [Toasty](#toasty)

### Action

- [Button](#button)
- [ClipboardText](#clipboardtext)

### Input

- [Checkbox](#checkbox)
- [Combobox](#combobox)
- [DateRangePicker](#daterangepicker)
- [Field](#field)
- [Input](#input)
- [InputGroup](#inputgroup)
- [Radio](#radio)
- [Select](#select)
- [Switch](#switch)

### Navigation

- [CommandPalette](#commandpalette)
- [MenuBar](#menubar)
- [Pagination](#pagination)
- [Tabs](#tabs)

### Overlay

- [Dialog](#dialog)
- [DropdownMenu](#dropdownmenu)
- [Popover](#popover)
- [Tooltip](#tooltip)

### Layout

- [Grid](#grid)
- [Surface](#surface)
- [PageHeader](#pageheader)
- [ResourceListPage](#resourcelistpage)

## Name Index

- [Autocomplete](#autocomplete)
- [Badge](#badge)
- [Banner](#banner)
- [Breadcrumbs](#breadcrumbs)
- [Button](#button)
- [Checkbox](#checkbox)
- [ClipboardText](#clipboardtext)
- [CloudflareLogo](#cloudflarelogo)
- [Code](#code)
- [Collapsible](#collapsible)
- [Combobox](#combobox)
- [CommandPalette](#commandpalette)
- [DatePicker](#datepicker)
- [DateRangePicker](#daterangepicker)
- [DeleteResource](#deleteresource)
- [Dialog](#dialog)
- [DropdownMenu](#dropdownmenu)
- [Empty](#empty)
- [Field](#field)
- [Grid](#grid)
- [Input](#input)
- [InputGroup](#inputgroup)
- [Label](#label)
- [LayerCard](#layercard)
- [Link](#link)
- [Loader](#loader)
- [MenuBar](#menubar)
- [Meter](#meter)
- [PageHeader](#pageheader)
- [Pagination](#pagination)
- [Popover](#popover)
- [Radio](#radio)
- [ResourceListPage](#resourcelistpage)
- [Select](#select)
- [SensitiveInput](#sensitiveinput)
- [Sidebar](#sidebar)
- [Surface](#surface)
- [Switch](#switch)
- [Table](#table)
- [TableOfContents](#tableofcontents)
- [Tabs](#tabs)
- [Text](#text)
- [Toasty](#toasty)
- [Tooltip](#tooltip)

## Components

## Autocomplete

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Autocomplete - free-form text input with an optional filtered suggestion list.  Unlike Combobox, the input value is not restricted to items in the list. Use Combobox when the selected value must come from the list.  Compound component: `Autocomplete` (Root), `.InputGroup`, `.Content`, `.Item`, `.GroupLabel`, `.Group`, `.Separator`, `.List`, `.Collection`.  `InputGroup` renders the text input with Input component styling. Pass a `size` prop to `InputGroup` to match the Input component sizes.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `items` | unknown[] | Yes | - | - | Array of items to display in the dropdown |
| `value` | string \| number \| string[] | No | - | - | The controlled input value |
| `open` | boolean | No | - | - | Whether the popup is open (controlled) |
| `children` | ReactNode | No | - | - | Autocomplete content (input group, popup content) |
| `className` | string | No | - | - | Additional CSS classes |
| `label` | ReactNode | No | - | - | Label content (enables Field wrapper) |
| `required` | boolean | No | - | - | Whether the field is required |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label |
| `description` | ReactNode | No | - | - | Helper text displayed below the field |
| `error` | string \| object | No | - | - | Error message or validation error object |

**Subcomponents**

### Autocomplete.InputGroup

- Description: InputGroup sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |
| `size` | KumoAutocompleteSize | No | - | - | - |
| `placeholder` | string | No | - | - | - |

### Autocomplete.Content

- Description: Content sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `align` | AutocompleteBase.Positioner.Props["align"] | No | - | - | - |
| `alignOffset` | AutocompleteBase.Positioner.Props["alignOffset"] | No | - | - | - |
| `side` | AutocompleteBase.Positioner.Props["side"] | No | - | - | - |
| `sideOffset` | AutocompleteBase.Positioner.Props["sideOffset"] | No | - | - | - |

### Autocomplete.Item

- Description: Item sub-component

Props:

- None

### Autocomplete.GroupLabel

- Description: GroupLabel sub-component

Props:

- None

### Autocomplete.Group

- Description: Group sub-component

Props:

- None

### Autocomplete.Separator

- Description: Separator sub-component

Props:

- None

### Autocomplete.List

- Description: List sub-component

Props:

- None

### Autocomplete.Empty

- Description: Empty sub-component (wraps AutocompleteBase)
- Pass through: Yes, base: `AutocompleteBase.Empty`

Props:

- None

### Autocomplete.Collection

- Description: Collection sub-component (wraps AutocompleteBase)
- Pass through: Yes, base: `AutocompleteBase.Collection`

Props:

- None

**Kumo Color Tokens**

`bg-kumo-control`, `bg-kumo-line`, `bg-kumo-overlay`, `border-kumo-line`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-strong`

**Examples**

Example 1:

```tsx
<Autocomplete items={fruits}>
      <Autocomplete.InputGroup placeholder="Search fruits..." />
      <Autocomplete.Content>
        <Autocomplete.List>
          {(item: string) => (
            <Autocomplete.Item key={item} value={item}>
              {item}
            </Autocomplete.Item>
          )}
        </Autocomplete.List>
      </Autocomplete.Content>
    </Autocomplete>
```

Example 2:

```tsx
<div className="w-80">
      <Autocomplete
        items={languages}
        label="Language"
        description="Start typing to filter languages"
        filter={filter}
      >
        <Autocomplete.InputGroup placeholder="Search a language..." />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: Language) => (
              <Autocomplete.Item key={item.value} value={item}>
                {item.emoji} {item.label}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
    </div>
```

Example 3:

```tsx
<div className="w-80">
      <Autocomplete
        items={countries}
        label="Country"
        error={{ message: "Please enter a valid country", match: true }}
        filter={filter}
      >
        <Autocomplete.InputGroup placeholder="Search countries..." />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: Country) => (
              <Autocomplete.Item key={item.code} value={item}>
                {item.label}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
    </div>
```

Example 4:

```tsx
<Autocomplete items={servers}>
      <Autocomplete.InputGroup placeholder="Select region..." />
      <Autocomplete.Content>
        <Autocomplete.List>
          {(group: ServerGroup) => (
            <Autocomplete.Group key={group.value} items={group.items}>
              <Autocomplete.GroupLabel>{group.value}</Autocomplete.GroupLabel>
              <Autocomplete.Collection>
                {(item: ServerLocation) => (
                  <Autocomplete.Item key={item.value} value={item}>
                    {item.label}
                  </Autocomplete.Item>
                )}
              </Autocomplete.Collection>
            </Autocomplete.Group>
          )}
        </Autocomplete.List>
      </Autocomplete.Content>
    </Autocomplete>
```

Example 5:

```tsx
<div className="flex flex-wrap items-center gap-4">
      <Autocomplete items={fruits.slice(0, 10)}>
        <Autocomplete.InputGroup size="xs" placeholder="xs" />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: string) => (
              <Autocomplete.Item key={item} value={item}>
                {item}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
      <Autocomplete items={fruits.slice(0, 10)}>
        <Autocomplete.InputGroup size="sm" placeholder="sm" />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: string) => (
              <Autocomplete.Item key={item} value={item}>
                {item}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
      <Autocomplete items={fruits.slice(0, 10)}>
        <Autocomplete.InputGroup size="base" placeholder="base (default)" />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: string) => (
              <Autocomplete.Item key={item} value={item}>
                {item}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
      <Autocomplete items={fruits.slice(0, 10)}>
        <Autocomplete.InputGroup size="lg" placeholder="lg" />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: string) => (
              <Autocomplete.Item key={item} value={item}>
                {item}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
    </div>
```

Example 6:

```tsx
<div className="flex flex-col gap-3 w-80">
      <Autocomplete
        items={fruits}
        value={value}
        onValueChange={(v) => setValue(v)}
      >
        <Autocomplete.InputGroup placeholder="Type a fruit..." />
        <Autocomplete.Content>
          <Autocomplete.List>
            {(item: string) => (
              <Autocomplete.Item key={item} value={item}>
                {item}
              </Autocomplete.Item>
            )}
          </Autocomplete.List>
        </Autocomplete.Content>
      </Autocomplete>
      {value && (
        <p className="text-sm text-kumo-subtle">
          Value: <span className="text-kumo-default font-medium">{value}</span>
        </p>
      )}
    </div>
```


## Badge

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Small status label for categorizing or highlighting content.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | primary | `primary`, `secondary`, `error`, `warning`, `success`, `destructive`, `info`, `beta`, `outline`, `red`, `green`, `neutral`, `orange`, `purple`, `teal`, `teal-subtle`, `blue` | Color variant of the badge. Recommended semantic variants: - `"primary"` - Primary badge - `"secondary"` - Secondary badge - `"error"` - Error badge - `"warning"` - Warning badge - `"success"` - Success badge - `"info"` - Info badge  Additional token variants: - `"red"`, `"orange"`, `"green"`, `"teal"`, `"blue"`, `"purple"`, `"neutral"` - `"teal-subtle"`, `"neutral-subtle"` - `"inverted"` - `"outline"` - Bordered badge with transparent background - `"beta"` - Dashed-border badge for beta/experimental features |
| `appearance` | enum | No | filled | `filled`, `dot` | Visual appearance of the badge. - `"filled"` - Filled background using the variant color (default) - `"dot"` - Outlined badge with a colored circle dot. Only `success`,   `warning`, `error`, and `neutral` variants show a dot; other variants   render the badge without a dot. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `children` | ReactNode | No | - | - | Content rendered inside the badge. |

**Kumo Color Tokens**

`bg-kumo-badge-blue`, `bg-kumo-badge-inverted`, `bg-kumo-badge-neutral`, `bg-kumo-badge-orange`, `bg-kumo-badge-purple`, `bg-kumo-badge-red`, `bg-kumo-badge-teal`, `bg-kumo-badge-teal-subtle`, `bg-kumo-danger-tint`, `bg-kumo-fill`, `bg-kumo-info-tint`, `bg-kumo-success`, `bg-kumo-success-tint`, `bg-kumo-warning-tint`, `border-kumo-brand`, `border-kumo-fill`, `ring-kumo-hairline`, `text-kumo-badge-inverted`, `text-kumo-badge-neutral-subtle`, `text-kumo-badge-teal-subtle`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-info`, `text-kumo-link`, `text-kumo-success`, `text-kumo-warning`

**Base Styles**

`inline-flex w-fit flex-none shrink-0 items-center justify-self-start rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap`

**Examples**

Example 1:

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Badge variant="primary">Primary</Badge>
      <Badge variant="secondary">Secondary</Badge>
      <Badge variant="error">Error</Badge>
      <Badge variant="success">Success</Badge>
      <Badge variant="warning">Warning</Badge>
      <Badge variant="info">Info</Badge>
      <Badge variant="outline">Outline</Badge>
      <Badge variant="beta">Beta</Badge>
    </div>
```

Example 2:

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Badge variant="neutral">Neutral</Badge>
      <Badge variant="red">Red</Badge>
      <Badge variant="orange">Orange</Badge>
      <Badge variant="teal">Teal</Badge>
      <Badge variant="blue">Blue</Badge>
      <Badge variant="purple">Purple</Badge>
    </div>
```

Example 3:

```tsx
<p className="flex items-center gap-2">
      Workers
      <Badge variant="secondary">New</Badge>
    </p>
```

Example 4:

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Badge variant="success" appearance="dot">Healthy</Badge>
      <Badge variant="warning" appearance="dot">Warning</Badge>
      <Badge variant="error" appearance="dot">Error</Badge>
      <Badge variant="neutral" appearance="dot">Neutral</Badge>
    </div>
```


## Banner

- Type: component
- Category: Feedback
- Import path: `@cloudflare/kumo`
- Description: Full-width message bar for informational, warning, or error notices. Supports structured title/description for i18n, or simple children for basic usage.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `icon` | ReactNode | No | - | - | Icon element rendered before the banner content (e.g. from `@phosphor-icons/react`). |
| `title` | string | No | - | - | Primary heading text for the banner. Use for i18n string injection. |
| `description` | ReactNode | No | - | - | Secondary description text displayed below the title. Use for i18n string injection. |
| `action` | ReactNode | No | - | - | Action slot rendered at the trailing end of the banner (e.g. a CTA button or link). Only used in structured mode (with `title` or `description`). |
| `text` | string | No | - | - | - |
| `children` | ReactNode | No | - | - | - |
| `variant` | enum | No | default | `default`, `alert`, `error`, `secondary` | Visual style of the banner. - `"default"` - Informational blue banner for general messages - `"alert"` - Warning yellow banner for cautionary messages - `"error"` - Error red banner for critical issues - `"secondary"` - Neutral banner for secondary messages |

**Kumo Color Tokens**

`bg-kumo-banner-info`, `bg-kumo-banner-warning`, `bg-kumo-contrast`, `bg-kumo-danger-tint`, `text-kumo-danger`, `text-kumo-info`, `text-kumo-subtle`, `text-kumo-warning`

**Base Styles**

`flex w-full items-start gap-3 rounded-lg px-4 py-3 text-base`

**Examples**

Example 1:

```tsx
<div className="w-full space-y-3">
      <Banner
        icon={<Info weight="fill" />}
        title="Update available"
        description="A new version is ready to install."
      />
      <Banner
        icon={<Warning weight="fill" />}
        variant="alert"
        title="Session expiring"
        description="Your session will expire in 5 minutes."
      />
      <Banner
        icon={<WarningCircle weight="fill" />}
        variant="error"
        title="Save failed"
        description="We couldn't save your changes. Please try again."
      />
      <Banner
        icon={<Info weight="fill" />}
        variant="secondary"
        title="Maintenance scheduled"
        description="This service will be unavailable for 10 minutes."
      />
    </div>
```

Example 2:

```tsx
<Banner
      icon={<Info weight="fill" />}
      title="Update available"
      description="A new version is ready to install."
    />
```

Example 3:

```tsx
<Banner
      icon={<Warning weight="fill" />}
      variant="alert"
      title="Session expiring"
      description="Your session will expire in 5 minutes."
    />
```

Example 4:

```tsx
<Banner
      icon={<WarningCircle weight="fill" />}
      variant="error"
      title="Save failed"
      description="We couldn't save your changes. Please try again."
    />
```

Example 5:

```tsx
<Banner
      icon={<Info weight="fill" />}
      variant="secondary"
      title="Maintenance scheduled"
      description="This service will be unavailable for 10 minutes."
    />
```

Example 6:

```tsx
<Banner
      icon={<Info weight="fill" />}
      title="Your changes have been saved."
    />
```

Example 7:

```tsx
<Banner
      icon={<Warning weight="fill" />}
      variant="alert"
      title="Review required"
      description="Please review your billing information before proceeding."
    />
```

Example 8:

```tsx
<Banner
      icon={<Info weight="fill" />}
      title="Custom content supported"
      description={
        <Text DANGEROUS_className="text-inherit">
          This banner supports <strong>custom content</strong> with Text.
        </Text>
      }
    />
```

Example 9:

```tsx
<div className="w-full space-y-3">
      <Banner
        icon={<Info weight="fill" />}
        title="Update available"
        description="A new version is ready to install."
        action={<Button size="sm">Update now</Button>}
      />
      <Banner
        icon={<Info weight="fill" />}
        title="Update available"
        description="A new version is ready to install."
        action={
          <Button
            size="sm"
            variant="ghost"
            shape="square"
            icon={X}
            aria-label="Dismiss"
          />
        }
      />
    </div>
```

Example 10:

```tsx
<Banner
      icon={<Warning weight="fill" />}
      variant="alert"
      title="Session expiring"
      description="Your session will expire in 5 minutes."
      action={
        <>
          <Button size="sm" variant="secondary">
            Dismiss
          </Button>
          <Button size="sm">Extend session</Button>
        </>
      }
    />
```

Example 11:

```tsx
<Banner icon={<Info />}>This is a simple banner using children.</Banner>
```


## Breadcrumbs

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Breadcrumbs component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | base | `sm`, `base` | Size of the breadcrumbs. - `"sm"` - Compact breadcrumbs for dense UIs - `"base"` - Default breadcrumbs size |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |

**Subcomponents**

### Breadcrumbs.Link

- Description: Link sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `href` | string | Yes | - | - | - |
| `icon` | React.ReactNode | No | - | - | - |

### Breadcrumbs.Current

- Description: Current sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `loading` | boolean | No | - | - | - |
| `icon` | React.ReactNode | No | - | - | - |

### Breadcrumbs.Separator

- Description: Separator sub-component

Props:

- None

### Breadcrumbs.Clipboard

- Description: Clipboard sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `text` | string | Yes | - | - | - |

**Kumo Color Tokens**

`text-kumo-inactive`, `text-kumo-subtle`, `text-kumo-success`

**Examples**

Example 1:

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Link href="#">Docs</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current>Breadcrumbs</Breadcrumbs.Current>
    </Breadcrumbs>
```

Example 2:

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#" icon={<House size={16} />}>
        Home
      </Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Link href="#">Projects</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current>Current Project</Breadcrumbs.Current>
    </Breadcrumbs>
```

Example 3:

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#" icon={<House size={16} />}>
        Home
      </Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Link href="#">Docs</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current loading></Breadcrumbs.Current>
    </Breadcrumbs>
```

Example 4:

```tsx
<Breadcrumbs>
      <Breadcrumbs.Current icon={<House size={16} />}>
        Worker Analytics
      </Breadcrumbs.Current>
    </Breadcrumbs>
```

Example 5:

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current>Breadcrumbs</Breadcrumbs.Current>
      <Breadcrumbs.Clipboard text="#" />
    </Breadcrumbs>
```


## Button

- Type: component
- Category: Action
- Import path: `@cloudflare/kumo`
- Description: Primary action trigger. Supports multiple variants, sizes, shapes, icons, and loading state.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `shape` | enum | No | base | `base`, `square`, `circle` | `{"base":"Default rectangular button shape","square":"Square button for icon-only actions","circle":"Circular button for icon-only actions"}` |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | `{"xs":"Extra small button for compact UIs","sm":"Small button for secondary actions","base":"Default button size","lg":"Large button for primary CTAs"}` |
| `variant` | enum | No | secondary | `primary`, `secondary`, `ghost`, `destructive`, `secondary-destructive`, `outline` | `{"primary":"High-emphasis button for primary actions","secondary":"Default button style for most actions","ghost":"Minimal button with no background","destructive":"Danger button for destructive actions like delete","secondary-destructive":"Secondary button with destructive text for less prominent dangerous actions","outline":"Bordered button with transparent background"}` |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `icon` | ReactNode | No | - | - | Icon from `@phosphor-icons/react` or a React element. Rendered before children. |
| `loading` | boolean | No | - | - | Shows a loading spinner and disables interaction. |
| `title` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `name` | string | No | - | - | - |
| `type` | enum | No | - | `submit`, `reset`, `button` | - |
| `value` | string \| string[] \| number | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-brand`, `bg-kumo-brand-hover`, `bg-kumo-danger`, `bg-kumo-tint`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-hairline`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Button variant="secondary">Button</Button>
      <Button
        variant="secondary"
        shape="square"
        icon={PlusIcon}
        aria-label="Add"
      />
    </div>
```

Example 2:

```tsx
<Button variant="primary">Primary</Button>
```

Example 3:

```tsx
<Button variant="secondary">Secondary</Button>
```

Example 4:

```tsx
<Button variant="ghost">Ghost</Button>
```

Example 5:

```tsx
<Button variant="destructive">Destructive</Button>
```

Example 6:

```tsx
<Button variant="outline">Outline</Button>
```

Example 7:

```tsx
<Button variant="secondary-destructive">Secondary Destructive</Button>
```

Example 8:

```tsx
<div className="flex flex-wrap items-center gap-3">
      <Button size="xs" variant="secondary">
        Extra Small
      </Button>
      <Button size="sm" variant="secondary">
        Small
      </Button>
      <Button size="base" variant="secondary">
        Base
      </Button>
      <Button size="lg" variant="secondary">
        Large
      </Button>
    </div>
```

Example 9:

```tsx
<Button variant="secondary" icon={PlusIcon}>
      Create Worker
    </Button>
```

Example 10:

```tsx
<div className="flex flex-wrap items-center gap-3">
      <Button
        variant="secondary"
        shape="square"
        icon={PlusIcon}
        aria-label="Add item"
      />
      <Button
        variant="secondary"
        shape="circle"
        icon={PlusIcon}
        aria-label="Add item"
      />
    </div>
```

Example 11:

```tsx
<Button variant="primary" loading>
      Loading...
    </Button>
```

Example 12:

```tsx
<Button variant="secondary" disabled>
      Disabled
    </Button>
```

Example 13:

```tsx
<Button variant="secondary">Click me</Button>
```

Example 14:

```tsx
<div className="flex flex-wrap items-center gap-3">
      <Button variant="secondary" title="Create a new Worker">
        Create Worker
      </Button>
      <Button
        variant="secondary"
        shape="square"
        icon={PlusIcon}
        aria-label="Add item"
        title="Add item"
      />
    </div>
```

Example 15:

```tsx
<div className="flex flex-wrap items-center gap-3">
      <LinkButton href="/components/link" variant="secondary">
        Read Link docs
      </LinkButton>
      <LinkButton
        href="https://developers.cloudflare.com"
        variant="ghost"
        icon={ArrowSquareOutIcon}
        external
      >
        Cloudflare Docs
      </LinkButton>
    </div>
```


## Checkbox

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Checkbox component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | default | `default`, `error` | Visual variant: "default" or "error" for validation failures (visual only, no error text) |
| `label` | ReactNode | No | - | - | Label content for the checkbox (enables built-in Field wrapper) - can be a string or any React node |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label via an info icon |
| `controlFirst` | boolean | No | - | - | When true (default), checkbox appears before label. When false, label appears before checkbox. |
| `checked` | boolean | No | - | - | Whether the checkbox is checked (controlled) |
| `indeterminate` | boolean | No | - | - | Whether the checkbox is in indeterminate state |
| `disabled` | boolean | No | - | - | Whether the checkbox is disabled |
| `name` | string | No | - | - | Name for form submission |
| `required` | boolean | No | - | - | Whether the field is required |
| `className` | string | No | - | - | Additional class name |

**Subcomponents**

### Checkbox.Item

- Description: Item sub-component

Props:

- None

### Checkbox.Group

- Description: Group sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `legend` | string | No | - | - | - |
| `children` | ReactNode | Yes | - | - | - |
| `error` | string | No | - | - | - |
| `description` | ReactNode | No | - | - | - |
| `value` | string[] | No | - | - | - |
| `allValues` | string[] | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `controlFirst` | boolean | No | - | - | - |
| `className` | string | No | - | - | - |

### Checkbox.Legend

- Description: Legend sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-contrast`, `ring-kumo-brand`, `ring-kumo-contrast`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-hairline`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Styling**

```json
{
  "dimensions": "h-4 w-4",
  "borderRadius": "rounded-sm",
  "baseTokens": [
    "bg-kumo-base",
    "ring-kumo-line"
  ],
  "states": {
    "checked": [
      "bg-kumo-contrast",
      "text-kumo-inverse"
    ],
    "indeterminate": [
      "bg-kumo-contrast",
      "text-kumo-inverse"
    ],
    "error": [
      "ring-kumo-danger"
    ],
    "hover": [
      "ring-kumo-hairline"
    ],
    "focus": [
      "ring-kumo-hairline"
    ],
    "disabled": [
      "opacity-50",
      "cursor-not-allowed"
    ]
  },
  "icons": [
    {
      "name": "ph-check",
      "state": "checked",
      "size": 12
    },
    {
      "name": "ph-minus",
      "state": "indeterminate",
      "size": 12
    }
  ]
}
```


**Examples**

Example 1:

```tsx
<Checkbox
      label="Accept terms and conditions"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

Example 2:

```tsx
<Checkbox
      label="Enable notifications"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

Example 3:

```tsx
<Checkbox label="I agree" checked={checked} onCheckedChange={setChecked} />
```

Example 4:

```tsx
<Checkbox
      label="Select all"
      indeterminate={indeterminate}
      onCheckedChange={setIndeterminate}
    />
```

Example 5:

```tsx
<Checkbox
      label="Remember me"
      controlFirst={false}
      checked={checked}
      onCheckedChange={setChecked}
    />
```

Example 6:

```tsx
<Checkbox label="Disabled option" disabled />
```

Example 7:

```tsx
<Checkbox label="Invalid option" variant="error" />
```

Example 8:

```tsx
<Checkbox.Group
      legend="Email preferences"
      description="Choose how you'd like to receive updates"
      value={preferences}
      onValueChange={setPreferences}
    >
      <Checkbox.Item value="email" label="Email notifications" />
      <Checkbox.Item value="sms" label="SMS notifications" />
      <Checkbox.Item value="push" label="Push notifications" />
    </Checkbox.Group>
```

Example 9:

```tsx
<Checkbox.Group value={preferences} onValueChange={setPreferences}>
      <Checkbox.Legend className="sr-only">
        Notification preferences
      </Checkbox.Legend>
      <Checkbox.Item value="email" label="Email notifications" />
      <Checkbox.Item value="sms" label="SMS notifications" />
      <Checkbox.Item value="push" label="Push notifications" />
    </Checkbox.Group>
```

Example 10:

```tsx
<Checkbox.Group value={preferences} onValueChange={setPreferences}>
      <Checkbox.Legend className="text-sm font-normal text-kumo-subtle">
        Notification preferences
      </Checkbox.Legend>
      <Checkbox.Item value="email" label="Email notifications" />
      <Checkbox.Item value="sms" label="SMS notifications" />
      <Checkbox.Item value="push" label="Push notifications" />
    </Checkbox.Group>
```

Example 11:

```tsx
<Checkbox.Group
      legend="Required preferences"
      error="Please select at least one notification method"
      value={[]}
      onValueChange={() => {}}
    >
      <Checkbox.Item value="email" label="Email" variant="error" />
      <Checkbox.Item value="sms" label="SMS" variant="error" />
    </Checkbox.Group>
```


## ClipboardText

- Type: component
- Category: Action
- Import path: `@cloudflare/kumo`
- Description: Read-only text field with a one-click copy-to-clipboard button.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | lg | `sm`, `base`, `lg` | Size of the clipboard text field. - `"sm"` - Small clipboard text for compact UIs - `"base"` - Default clipboard text size - `"lg"` - Large clipboard text for prominent display |
| `text` | string | Yes | - | - | The text to display and copy to clipboard. |
| `textToCopy` | string | No | - | - | If provided, this text will be copied to clipboard instead of the `text` prop. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `tooltip` | object | No | - | - | Tooltip config. Shows tooltip on hover, anchored toast on click. |
| `labels` | object | No | - | - | Accessible labels for i18n. |

**Kumo Color Tokens**

`bg-kumo-base`, `border-kumo-line`, `outline-kumo-fill`, `ring-kumo-brand`, `ring-kumo-focus`, `text-kumo-default`

**Styling**

```json
{
  "baseTokens": [
    "bg-kumo-base",
    "text-kumo-default",
    "ring-kumo-line",
    "border-kumo-fill"
  ],
  "states": {
    "input": [
      "bg-kumo-control",
      "text-kumo-default",
      "ring-kumo-line"
    ],
    "text": [
      "bg-kumo-base",
      "font-mono"
    ],
    "button": [
      "border-kumo-fill"
    ]
  },
  "inputStyles": {
    "base": "bg-kumo-control text-kumo-default ring ring-kumo-line",
    "sizes": {
      "xs": "h-5 gap-1 rounded-sm px-1.5 text-xs",
      "sm": "h-6.5 gap-1 rounded-md px-2 text-xs",
      "base": "h-9 gap-1.5 rounded-lg px-3 text-base",
      "lg": "h-10 gap-2 rounded-lg px-4 text-base"
    }
  },
  "sizeVariants": {
    "sm": {
      "height": 26,
      "classes": "text-xs",
      "buttonSize": "sm",
      "dimensions": {
        "paddingX": 8,
        "gap": 1,
        "borderRadius": 6,
        "fontSize": 12
      }
    },
    "base": {
      "height": 36,
      "classes": "text-sm",
      "buttonSize": "base",
      "dimensions": {
        "paddingX": 12,
        "gap": 6,
        "borderRadius": 8,
        "fontSize": 14
      }
    },
    "lg": {
      "height": 40,
      "classes": "text-sm",
      "buttonSize": "lg",
      "dimensions": {
        "paddingX": 16,
        "gap": 8,
        "borderRadius": 8,
        "fontSize": 14
      }
    }
  },
  "icons": [
    {
      "name": "ph-clipboard",
      "state": "default",
      "size": 16
    },
    {
      "name": "ph-check",
      "state": "copied",
      "size": 16
    }
  ]
}
```


**Examples**

Example 1:

```tsx
<ClipboardText text="0c239dd2" />
```

Example 2:

```tsx
<ClipboardText text="abc123" />
```

Example 3:

```tsx
<ClipboardText text="sk_live_51H8..." />
```

Example 4:

```tsx
<ClipboardText
      text="sk_live_***********"
      textToCopy="sk_live_51H8_abc123"
    />
```

Example 5:

```tsx
<ClipboardText text="https://example.com/very/long/url/path" />
```

Example 6:

```tsx
<ClipboardText
      text="npx kumo add button"
      tooltip={{ text: "Copy", copiedText: "Copied!", side: "top" }}
    />
```


## CloudflareLogo

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Cloudflare logo component.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `height` | number \| string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `media` | string | No | - | - | - |
| `method` | string | No | - | - | - |
| `name` | string | No | - | - | - |
| `target` | string | No | - | - | - |
| `type` | string | No | - | - | - |
| `width` | number \| string | No | - | - | - |
| `accentHeight` | number \| string | No | - | - | - |
| `accumulate` | enum | No | - | `none`, `sum` | - |
| `additive` | enum | No | - | `replace`, `sum` | - |
| `alignmentBaseline` | enum | No | - | `auto`, `baseline`, `before-edge`, `text-before-edge`, `middle`, `central`, `after-edge`, `text-after-edge`, `ideographic`, `alphabetic`, `hanging`, `mathematical`, `inherit` | - |
| `allowReorder` | enum | No | - | `no`, `yes` | - |
| `alphabetic` | number \| string | No | - | - | - |
| `amplitude` | number \| string | No | - | - | - |
| `arabicForm` | enum | No | - | `initial`, `medial`, `terminal`, `isolated` | - |
| `ascent` | number \| string | No | - | - | - |
| `attributeName` | string | No | - | - | - |
| `attributeType` | string | No | - | - | - |
| `autoReverse` | Booleanish | No | - | - | - |
| `azimuth` | number \| string | No | - | - | - |
| `baseFrequency` | number \| string | No | - | - | - |
| `baselineShift` | number \| string | No | - | - | - |
| `baseProfile` | number \| string | No | - | - | - |
| `bbox` | number \| string | No | - | - | - |
| `begin` | number \| string | No | - | - | - |
| `bias` | number \| string | No | - | - | - |
| `by` | number \| string | No | - | - | - |
| `calcMode` | number \| string | No | - | - | - |
| `capHeight` | number \| string | No | - | - | - |
| `clip` | number \| string | No | - | - | - |
| `clipPath` | string | No | - | - | - |
| `clipPathUnits` | number \| string | No | - | - | - |
| `clipRule` | number \| string | No | - | - | - |
| `colorInterpolation` | number \| string | No | - | - | - |
| `colorInterpolationFilters` | enum | No | - | `auto`, `sRGB`, `linearRGB`, `inherit` | - |
| `colorProfile` | number \| string | No | - | - | - |
| `colorRendering` | number \| string | No | - | - | - |
| `contentScriptType` | number \| string | No | - | - | - |
| `contentStyleType` | number \| string | No | - | - | - |
| `cursor` | number \| string | No | - | - | - |
| `cx` | number \| string | No | - | - | - |
| `cy` | number \| string | No | - | - | - |
| `d` | string | No | - | - | - |
| `decelerate` | number \| string | No | - | - | - |
| `descent` | number \| string | No | - | - | - |
| `diffuseConstant` | number \| string | No | - | - | - |
| `direction` | number \| string | No | - | - | - |
| `display` | number \| string | No | - | - | - |
| `divisor` | number \| string | No | - | - | - |
| `dominantBaseline` | enum | No | - | `auto`, `use-script`, `no-change`, `reset-size`, `ideographic`, `alphabetic`, `hanging`, `mathematical`, `central`, `middle`, `text-after-edge`, `text-before-edge`, `inherit` | - |
| `dur` | number \| string | No | - | - | - |
| `dx` | number \| string | No | - | - | - |
| `dy` | number \| string | No | - | - | - |
| `edgeMode` | number \| string | No | - | - | - |
| `elevation` | number \| string | No | - | - | - |
| `enableBackground` | number \| string | No | - | - | - |
| `end` | number \| string | No | - | - | - |
| `exponent` | number \| string | No | - | - | - |
| `externalResourcesRequired` | Booleanish | No | - | - | - |
| `fill` | string | No | - | - | - |
| `fillOpacity` | number \| string | No | - | - | - |
| `fillRule` | enum | No | - | `nonzero`, `evenodd`, `inherit` | - |
| `filter` | string | No | - | - | - |
| `filterRes` | number \| string | No | - | - | - |
| `filterUnits` | number \| string | No | - | - | - |
| `floodColor` | number \| string | No | - | - | - |
| `floodOpacity` | number \| string | No | - | - | - |
| `focusable` | Booleanish \| string | No | - | - | - |
| `fontFamily` | string | No | - | - | - |
| `fontSize` | number \| string | No | - | - | - |
| `fontSizeAdjust` | number \| string | No | - | - | - |
| `fontStretch` | number \| string | No | - | - | - |
| `fontStyle` | number \| string | No | - | - | - |
| `fontVariant` | number \| string | No | - | - | - |
| `fontWeight` | number \| string | No | - | - | - |
| `format` | number \| string | No | - | - | - |
| `fr` | number \| string | No | - | - | - |
| `from` | number \| string | No | - | - | - |
| `fx` | number \| string | No | - | - | - |
| `fy` | number \| string | No | - | - | - |
| `g1` | number \| string | No | - | - | - |
| `g2` | number \| string | No | - | - | - |
| `glyphName` | number \| string | No | - | - | - |
| `glyphOrientationHorizontal` | number \| string | No | - | - | - |
| `glyphOrientationVertical` | number \| string | No | - | - | - |
| `glyphRef` | number \| string | No | - | - | - |
| `gradientTransform` | string | No | - | - | - |
| `gradientUnits` | string | No | - | - | - |
| `hanging` | number \| string | No | - | - | - |
| `horizAdvX` | number \| string | No | - | - | - |
| `horizOriginX` | number \| string | No | - | - | - |
| `href` | string | No | - | - | - |
| `ideographic` | number \| string | No | - | - | - |
| `imageRendering` | number \| string | No | - | - | - |
| `in2` | number \| string | No | - | - | - |
| `in` | string | No | - | - | - |
| `intercept` | number \| string | No | - | - | - |
| `k1` | number \| string | No | - | - | - |
| `k2` | number \| string | No | - | - | - |
| `k3` | number \| string | No | - | - | - |
| `k4` | number \| string | No | - | - | - |
| `k` | number \| string | No | - | - | - |
| `kernelMatrix` | number \| string | No | - | - | - |
| `kernelUnitLength` | number \| string | No | - | - | - |
| `kerning` | number \| string | No | - | - | - |
| `keyPoints` | number \| string | No | - | - | - |
| `keySplines` | number \| string | No | - | - | - |
| `keyTimes` | number \| string | No | - | - | - |
| `lengthAdjust` | number \| string | No | - | - | - |
| `letterSpacing` | number \| string | No | - | - | - |
| `lightingColor` | number \| string | No | - | - | - |
| `limitingConeAngle` | number \| string | No | - | - | - |
| `local` | number \| string | No | - | - | - |
| `markerEnd` | string | No | - | - | - |
| `markerHeight` | number \| string | No | - | - | - |
| `markerMid` | string | No | - | - | - |
| `markerStart` | string | No | - | - | - |
| `markerUnits` | number \| string | No | - | - | - |
| `markerWidth` | number \| string | No | - | - | - |
| `mask` | string | No | - | - | - |
| `maskContentUnits` | number \| string | No | - | - | - |
| `maskUnits` | number \| string | No | - | - | - |
| `mathematical` | number \| string | No | - | - | - |
| `mode` | number \| string | No | - | - | - |
| `numOctaves` | number \| string | No | - | - | - |
| `offset` | number \| string | No | - | - | - |
| `opacity` | number \| string | No | - | - | - |
| `operator` | number \| string | No | - | - | - |
| `order` | number \| string | No | - | - | - |
| `orient` | number \| string | No | - | - | - |
| `orientation` | number \| string | No | - | - | - |
| `origin` | number \| string | No | - | - | - |
| `overflow` | number \| string | No | - | - | - |
| `overlinePosition` | number \| string | No | - | - | - |
| `overlineThickness` | number \| string | No | - | - | - |
| `paintOrder` | number \| string | No | - | - | - |
| `panose1` | number \| string | No | - | - | - |
| `path` | string | No | - | - | - |
| `pathLength` | number \| string | No | - | - | - |
| `patternContentUnits` | string | No | - | - | - |
| `patternTransform` | number \| string | No | - | - | - |
| `patternUnits` | string | No | - | - | - |
| `pointerEvents` | number \| string | No | - | - | - |
| `points` | string | No | - | - | - |
| `pointsAtX` | number \| string | No | - | - | - |
| `pointsAtY` | number \| string | No | - | - | - |
| `pointsAtZ` | number \| string | No | - | - | - |
| `preserveAlpha` | Booleanish | No | - | - | - |
| `preserveAspectRatio` | string | No | - | - | - |
| `primitiveUnits` | number \| string | No | - | - | - |
| `r` | number \| string | No | - | - | - |
| `radius` | number \| string | No | - | - | - |
| `refX` | number \| string | No | - | - | - |
| `refY` | number \| string | No | - | - | - |
| `renderingIntent` | number \| string | No | - | - | - |
| `repeatCount` | number \| string | No | - | - | - |
| `repeatDur` | number \| string | No | - | - | - |
| `requiredExtensions` | number \| string | No | - | - | - |
| `requiredFeatures` | number \| string | No | - | - | - |
| `restart` | number \| string | No | - | - | - |
| `result` | string | No | - | - | - |
| `rotate` | number \| string | No | - | - | - |
| `rx` | number \| string | No | - | - | - |
| `ry` | number \| string | No | - | - | - |
| `scale` | number \| string | No | - | - | - |
| `seed` | number \| string | No | - | - | - |
| `shapeRendering` | number \| string | No | - | - | - |
| `slope` | number \| string | No | - | - | - |
| `spacing` | number \| string | No | - | - | - |
| `specularConstant` | number \| string | No | - | - | - |
| `specularExponent` | number \| string | No | - | - | - |
| `speed` | number \| string | No | - | - | - |
| `spreadMethod` | string | No | - | - | - |
| `startOffset` | number \| string | No | - | - | - |
| `stdDeviation` | number \| string | No | - | - | - |
| `stemh` | number \| string | No | - | - | - |
| `stemv` | number \| string | No | - | - | - |
| `stitchTiles` | number \| string | No | - | - | - |
| `stopColor` | string | No | - | - | - |
| `stopOpacity` | number \| string | No | - | - | - |
| `strikethroughPosition` | number \| string | No | - | - | - |
| `strikethroughThickness` | number \| string | No | - | - | - |
| `string` | number \| string | No | - | - | - |
| `stroke` | string | No | - | - | - |
| `strokeDasharray` | string \| number | No | - | - | - |
| `strokeDashoffset` | string \| number | No | - | - | - |
| `strokeLinecap` | enum | No | - | `butt`, `round`, `square`, `inherit` | - |
| `strokeLinejoin` | enum | No | - | `miter`, `round`, `bevel`, `inherit` | - |
| `strokeMiterlimit` | number \| string | No | - | - | - |
| `strokeOpacity` | number \| string | No | - | - | - |
| `strokeWidth` | number \| string | No | - | - | - |
| `surfaceScale` | number \| string | No | - | - | - |
| `systemLanguage` | number \| string | No | - | - | - |
| `tableValues` | number \| string | No | - | - | - |
| `targetX` | number \| string | No | - | - | - |
| `targetY` | number \| string | No | - | - | - |
| `textAnchor` | enum | No | - | `start`, `middle`, `end`, `inherit` | - |
| `textDecoration` | number \| string | No | - | - | - |
| `textLength` | number \| string | No | - | - | - |
| `textRendering` | number \| string | No | - | - | - |
| `to` | number \| string | No | - | - | - |
| `transform` | string | No | - | - | - |
| `u1` | number \| string | No | - | - | - |
| `u2` | number \| string | No | - | - | - |
| `underlinePosition` | number \| string | No | - | - | - |
| `underlineThickness` | number \| string | No | - | - | - |
| `unicode` | number \| string | No | - | - | - |
| `unicodeBidi` | number \| string | No | - | - | - |
| `unicodeRange` | number \| string | No | - | - | - |
| `unitsPerEm` | number \| string | No | - | - | - |
| `vAlphabetic` | number \| string | No | - | - | - |
| `values` | string | No | - | - | - |
| `vectorEffect` | number \| string | No | - | - | - |
| `version` | string | No | - | - | - |
| `vertAdvY` | number \| string | No | - | - | - |
| `vertOriginX` | number \| string | No | - | - | - |
| `vertOriginY` | number \| string | No | - | - | - |
| `vHanging` | number \| string | No | - | - | - |
| `vIdeographic` | number \| string | No | - | - | - |
| `viewBox` | string | No | - | - | - |
| `viewTarget` | number \| string | No | - | - | - |
| `visibility` | number \| string | No | - | - | - |
| `vMathematical` | number \| string | No | - | - | - |
| `widths` | number \| string | No | - | - | - |
| `wordSpacing` | number \| string | No | - | - | - |
| `writingMode` | number \| string | No | - | - | - |
| `x1` | number \| string | No | - | - | - |
| `x2` | number \| string | No | - | - | - |
| `x` | number \| string | No | - | - | - |
| `xChannelSelector` | string | No | - | - | - |
| `xHeight` | number \| string | No | - | - | - |
| `xlinkActuate` | string | No | - | - | - |
| `xlinkArcrole` | string | No | - | - | - |
| `xlinkHref` | string | No | - | - | - |
| `xlinkRole` | string | No | - | - | - |
| `xlinkShow` | string | No | - | - | - |
| `xlinkTitle` | string | No | - | - | - |
| `xlinkType` | string | No | - | - | - |
| `xmlBase` | string | No | - | - | - |
| `xmlLang` | string | No | - | - | - |
| `xmlns` | string | No | - | - | - |
| `xmlnsXlink` | string | No | - | - | - |
| `xmlSpace` | string | No | - | - | - |
| `y1` | number \| string | No | - | - | - |
| `y2` | number \| string | No | - | - | - |
| `y` | number \| string | No | - | - | - |
| `yChannelSelector` | string | No | - | - | - |
| `z` | number \| string | No | - | - | - |
| `zoomAndPan` | string | No | - | - | - |
| `variant` | enum | No | full | `glyph`, `full` | Logo variant - `glyph`: Cloud icon only - `full`: Cloud icon with "CLOUDFLARE" wordmark below |

**Kumo Color Tokens**

`bg-kumo-base`, `ring-kumo-hairline`, `text-kumo-default`

**Examples**

Example 1:

```tsx
<CloudflareLogo className="w-72" />
```

Example 2:

```tsx
<CloudflareLogo variant="glyph" className="w-24" />
```

Example 3:

```tsx
<div className="flex flex-wrap items-center gap-8">
      <CloudflareLogo className="w-28" color="color" />
      <div className="rounded-lg bg-white p-4">
        <CloudflareLogo className="w-28" color="black" />
      </div>
      <div className="rounded-lg bg-black p-4">
        <CloudflareLogo className="w-28" color="white" />
      </div>
    </div>
```

Example 4:

```tsx
<div className="flex flex-wrap items-center gap-8">
      <CloudflareLogo variant="glyph" className="w-12" color="color" />
      <div className="rounded-lg bg-white p-4">
        <CloudflareLogo variant="glyph" className="w-12" color="black" />
      </div>
      <div className="rounded-lg bg-black p-4">
        <CloudflareLogo variant="glyph" className="w-12" color="white" />
      </div>
    </div>
```

Example 5:

```tsx
<div className="flex flex-wrap items-end gap-6">
      <CloudflareLogo className="w-20" />
      <CloudflareLogo className="w-28" />
      <CloudflareLogo className="w-44" />
    </div>
```

Example 6:

```tsx
<div className="flex items-center gap-4">
      <DropdownMenu>
        <DropdownMenu.Trigger>
          <button
            type="button"
            className="flex items-center gap-2 rounded-lg bg-black px-4 py-3 text-white transition-opacity hover:opacity-80"
          >
            <CloudflareLogo variant="glyph" color="white" className="w-8" />
            <span className="font-medium">Logo</span>
          </button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          <DropdownMenu.Item
            icon={CloudIcon}
            onClick={() =>
              copyToClipboard(
                generateCloudflareLogoSvg({ variant: "glyph" }),
                "glyph",
              )
            }
          >
            {copied === "glyph" ? "Copied!" : "Copy logo as SVG"}
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={CodeIcon}
            onClick={() =>
              copyToClipboard(
                generateCloudflareLogoSvg({ variant: "full" }),
                "full",
              )
            }
          >
            {copied === "full" ? "Copied!" : "Copy full logo as SVG"}
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={DownloadSimpleIcon}
            onClick={() =>
              window.open(
                "https://www.cloudflare.com/press-kit/",
                "_blank",
                "noopener",
              )
            }
          >
            Download brand assets
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={ArrowSquareOutIcon}
            onClick={() =>
              window.open(
                "https://www.cloudflare.com/brand-assets/",
                "_blank",
                "noopener",
              )
            }
          >
            Visit brand guidelines
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>

      <span className="text-sm text-kumo-subtle">
        Click to open the brand assets menu
      </span>
    </div>
```

Example 7:

```tsx
<PoweredByCloudflare />
```

Example 8:

```tsx
<div className="flex flex-wrap items-center gap-4">
      <PoweredByCloudflare />
      <PoweredByCloudflare color="black" />
      <div className="rounded-lg bg-black p-3">
        <PoweredByCloudflare color="white" />
      </div>
    </div>
```

Example 9:

```tsx
<footer className="flex w-full items-center justify-between rounded-lg border border-kumo-hairline bg-kumo-elevated px-6 py-4">
      <span className="text-sm text-kumo-subtle">
        &copy; 2026 Your Company. All rights reserved.
      </span>
      <PoweredByCloudflare />
    </footer>
```


## Code

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Code component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `lang` | enum | No | ts | `ts`, `tsx`, `jsonc`, `bash`, `css` | Language hint for the code content. - `"ts"` - TypeScript code - `"tsx"` - TypeScript JSX code - `"jsonc"` - JSON with comments - `"bash"` - Shell/Bash commands - `"css"` - CSS styles |
| `code` | string | Yes | - | - | The code string to display. |
| `values` | Record<string, { value: string; highlight?: boolean }> | No | - | - | Template values for `{{key}}` interpolation. Values with `highlight: true` are visually emphasized. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |

**Subcomponents**

### Code.Block

- Description: Block sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `code` | string | Yes | - | - | - |
| `lang` | CodeLang | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-base`, `border-kumo-fill`, `text-kumo-subtle`

**Styling**

```json
{
  "baseTokens": [
    "text-kumo-subtle"
  ],
  "typography": {
    "fontFamily": "font-mono",
    "fontSize": "text-sm",
    "lineHeight": "leading-[20px]"
  },
  "dimensions": {
    "margin": "m-0",
    "padding": "p-0",
    "width": "w-auto"
  },
  "appearance": {
    "borderRadius": "rounded-none",
    "border": "border-none",
    "background": "bg-transparent"
  }
}
```



## Collapsible

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Collapsible - a composable disclosure component for showing/hiding content.  Built on Base UI's Collapsible with full composition support.  ## Basic Usage  ```tsx const [open, setOpen] = useState(false);  <Collapsible.Root open={open} onOpenChange={setOpen}> <Collapsible.Trigger render={<Button variant="ghost" />}> Show details </Collapsible.Trigger> <Collapsible.Panel className="mt-2"> <Text>Hidden content revealed when expanded.</Text> </Collapsible.Panel> </Collapsible.Root> ```  ## With Default Styling  Use `DefaultTrigger` and `DefaultPanel` for the classic Kumo style:  ```tsx <Collapsible.Root> <Collapsible.DefaultTrigger>Show details</Collapsible.DefaultTrigger> <Collapsible.DefaultPanel> <Text>Content with border-left accent</Text> </Collapsible.DefaultPanel> </Collapsible.Root> ```  ## Controlled Accordion Pattern  ```tsx const [activeIndex, setActiveIndex] = useState<number | null>(null);  {items.map((item, i) => ( <Collapsible.Root key={i} open={activeIndex === i} onOpenChange={(open) => setActiveIndex(open ? i : null)} > <Collapsible.DefaultTrigger>{item.title}</Collapsible.DefaultTrigger> <Collapsible.DefaultPanel>{item.content}</Collapsible.DefaultPanel> </Collapsible.Root> ))} ```

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes |
| `onOpenChange` | (open: boolean) => void | No | - | - | Callback when collapsed state changes |

**Subcomponents**

### Collapsible.Root

- Description: Root sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |

### Collapsible.Trigger

- Description: Trigger sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |

### Collapsible.Panel

- Description: Panel sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |

### Collapsible.DefaultTrigger

- Description: DefaultTrigger sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

### Collapsible.DefaultPanel

- Description: DefaultPanel sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`border-kumo-fill`, `text-kumo-link`

**Examples**

Example 1:

```tsx
<div className="w-full">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.DefaultTrigger>What is Kumo?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Kumo is Cloudflare's new design system.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

Example 2:

```tsx
<div className="w-full">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.DefaultTrigger>What is Kumo?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Kumo is Cloudflare's new design system.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

Example 3:

```tsx
<div className="w-full space-y-2">
      <Collapsible.Root open={open1} onOpenChange={setOpen1}>
        <Collapsible.DefaultTrigger>What is Kumo?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Kumo is Cloudflare's new design system.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
      <Collapsible.Root open={open2} onOpenChange={setOpen2}>
        <Collapsible.DefaultTrigger>How do I use it?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Install the components and import them into your project.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
      <Collapsible.Root open={open3} onOpenChange={setOpen3}>
        <Collapsible.DefaultTrigger>Is it open source?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Check the repository for license information.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

Example 4:

```tsx
<div className="w-full">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.Trigger
          render={<Button variant="secondary" size="sm" />}
        >
          {isOpen ? "Hide details" : "Show details"}
        </Collapsible.Trigger>
        <Collapsible.Panel className="mt-3 rounded-lg bg-kumo-tint p-4">
          <Text>
            This panel uses custom styling instead of the default border-left accent.
          </Text>
        </Collapsible.Panel>
      </Collapsible.Root>
    </div>
```

Example 5:

```tsx
<div className="w-full space-y-4">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.DefaultTrigger>Edit details</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel keepMounted>
          <Text>
            Type something below, then collapse and re-open - your input is
            preserved because the panel stays mounted.
          </Text>
          <Input label="Name" placeholder="Type here..." />
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

Example 6:

```tsx
<div className="w-full space-y-2">
      {items.map((item, i) => (
        <Collapsible.Root
          key={i}
          open={activeIndex === i}
          onOpenChange={(open) => setActiveIndex(open ? i : null)}
        >
          <Collapsible.DefaultTrigger>{item.title}</Collapsible.DefaultTrigger>
          <Collapsible.DefaultPanel>
            <Text>{item.content}</Text>
          </Collapsible.DefaultPanel>
        </Collapsible.Root>
      ))}
    </div>
```


## Combobox

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Combobox - autocomplete input with filterable dropdown list.  Compound component: `Combobox` (Root), `.TriggerInput`, `.TriggerValue`, `.TriggerMultipleWithInput`, `.Content`, `.Item`, `.Chip`, `.Input`, `.Empty`, `.GroupLabel`, `.Group`, `.List`, `.Collection`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | Size of the combobox trigger. Matches Input component sizes. - `"xs"` - Extra small for compact UIs (h-5 / 20px) - `"sm"` - Small for secondary fields (h-6.5 / 26px) - `"base"` - Default size (h-9 / 36px) - `"lg"` - Large for prominent fields (h-10 / 40px) |
| `inputSide` | enum | No | right | `right`, `top` | Position of the text input relative to chips in multi-select mode. - `"right"` - Input inline to the right of chips - `"top"` - Input above chips |
| `items` | T[] | Yes | - | - | Array of items to display in the dropdown |
| `value` | T \| T[] | No | - | - | Currently selected value(s) |
| `children` | ReactNode | No | - | - | Combobox content (trigger, content, items) |
| `className` | string | No | - | - | Additional CSS classes |
| `label` | ReactNode | No | - | - | Label content for the combobox (enables Field wrapper) - can be a string or any React node |
| `required` | boolean | No | - | - | Whether the combobox is required |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label via an info icon |
| `description` | ReactNode | No | - | - | Helper text displayed below the combobox |
| `error` | string \| object | No | - | - | Error message or validation error object |
| `onValueChange` | (value: T \| T[]) => void | No | - | - | Callback when selection changes |
| `multiple` | boolean | No | - | - | Allow multiple selections |
| `isItemEqualToValue` | (item: T, value: T) => boolean | No | - | - | Custom equality function for comparing items |

**Subcomponents**

### Combobox.Content

- Description: Content sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |
| `align` | ComboboxBase.Positioner.Props["align"] | No | - | - | - |
| `alignOffset` | ComboboxBase.Positioner.Props["alignOffset"] | No | - | - | - |
| `side` | ComboboxBase.Positioner.Props["side"] | No | - | - | - |
| `sideOffset` | ComboboxBase.Positioner.Props["sideOffset"] | No | - | - | - |
| `container` | PortalContainer | No | - | - | - |

### Combobox.TriggerValue

- Description: TriggerValue sub-component

Props:

- None

### Combobox.TriggerInput

- Description: TriggerInput sub-component

Props:

- None

### Combobox.TriggerMultipleWithInput

- Description: TriggerMultipleWithInput sub-component

Props:

- None

### Combobox.Chip

- Description: Chip sub-component

Props:

- None

### Combobox.Item

- Description: Item sub-component

Props:

- None

### Combobox.Input

- Description: Input sub-component

Props:

- None

### Combobox.Empty

- Description: Empty sub-component

Props:

- None

### Combobox.GroupLabel

- Description: GroupLabel sub-component

Props:

- None

### Combobox.Group

- Description: Group sub-component

Props:

- None

### Combobox.List

- Description: List sub-component

Props:

- None

### Combobox.Collection

- Description: Renders filtered list items. Use when you need more control over item rendering.
- Pass through: Yes, base: `ComboboxBase.Collection`

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | (item: T, index: number) => ReactNode | Yes | - | - | Function that receives each filtered item and returns a node |

Usage examples:

Example 1:

```tsx
<Combobox.Collection>
  {(item, index) => (
    <Combobox.Item key={index} value={item}>
      {item.label}
    </Combobox.Item>
  )}
</Combobox.Collection>
```

### Combobox.Trigger

- Description: Trigger sub-component (wraps ComboboxBase)
- Pass through: Yes, base: `ComboboxBase.Trigger`

Props:

- None

### Combobox.Value

- Description: Value sub-component (wraps ComboboxBase)
- Pass through: Yes, base: `ComboboxBase.Value`

Props:

- None

### Combobox.Icon

- Description: Icon sub-component (wraps ComboboxBase)
- Pass through: Yes, base: `ComboboxBase.Icon`

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-fill-hover`, `bg-kumo-overlay`, `bg-kumo-tint`, `border-kumo-hairline`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-placeholder`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      items={fruits}
    >
      <Combobox.TriggerInput placeholder="Please select" />
      <Combobox.Content>
        <Combobox.Empty />
        <Combobox.List>
          {(item: string) => (
            <Combobox.Item key={item} value={item}>
              {item}
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```

Example 2:

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as Language)}
      items={languages}
    >
      <Combobox.TriggerValue className="w-[200px]" />
      <Combobox.Content>
        <Combobox.Input placeholder="Search languages" />
        <Combobox.Empty />
        <Combobox.List>
          {(item: Language) => (
            <Combobox.Item key={item.value} value={item}>
              {item.emoji} {item.label}
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```

Example 3:

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as Language | null)}
      items={languages}
    >
      <Combobox.TriggerValue
        className="w-[200px]"
        placeholder="Select a language"
      />
      <Combobox.Content>
        <Combobox.Input placeholder="Search languages" />
        <Combobox.Empty />
        <Combobox.List>
          {(item: Language) => (
            <Combobox.Item key={item.value} value={item}>
              {item.emoji} {item.label}
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```

Example 4:

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as ServerLocation | null)}
      items={servers}
    >
      <Combobox.TriggerInput
        className="w-[200px]"
        placeholder="Select server"
      />
      <Combobox.Content>
        <Combobox.Empty />
        <Combobox.List>
          {(group: ServerLocationGroup) => (
            <Combobox.Group key={group.value} items={group.items}>
              <Combobox.GroupLabel>{group.value}</Combobox.GroupLabel>
              <Combobox.Collection>
                {(item: ServerLocation) => (
                  <Combobox.Item key={item.value} value={item}>
                    {item.label}
                  </Combobox.Item>
                )}
              </Combobox.Collection>
            </Combobox.Group>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```

Example 5:

```tsx
<div className="flex gap-2">
      <Combobox
        value={value}
        onValueChange={setValue}
        items={bots}
        isItemEqualToValue={(bot: BotItem, selected: BotItem) =>
          bot.value === selected.value
        }
        multiple
      >
        <Combobox.TriggerMultipleWithInput
          className="w-[400px]"
          placeholder="Select bots"
          renderItem={(selected: BotItem) => (
            <Combobox.Chip key={selected.value}>{selected.label}</Combobox.Chip>
          )}
          inputSide="right"
        />
        <Combobox.Content className="max-h-[200px] min-w-auto overflow-y-auto">
          <Combobox.Empty />
          <Combobox.List>
            {(item: BotItem) => (
              <Combobox.Item key={item.value} value={item}>
                <div className="flex gap-2">
                  <Text>{item.label}</Text>
                  <Text variant="secondary">{item.author}</Text>
                </div>
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
      <Button variant="primary">Submit</Button>
    </div>
```

Example 6:

```tsx
<div className="w-80">
      <Combobox
        items={databases}
        value={value}
        onValueChange={setValue}
        label="Database"
        description="Select your preferred database"
      >
        <Combobox.TriggerInput placeholder="Select database" />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: DatabaseItem) => (
              <Combobox.Item key={item.value} value={item}>
                {item.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 7:

```tsx
<div className="flex flex-wrap gap-4 items-start">
      <Combobox value="Apple" items={fruits} disabled>
        <Combobox.TriggerInput
          className="w-[200px]"
          placeholder="Select fruit"
        />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: string) => (
              <Combobox.Item key={item} value={item}>
                {item}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>

      <Combobox value={languages[0]} items={languages} disabled>
        <Combobox.TriggerValue className="w-[200px]" />
        <Combobox.Content>
          <Combobox.Input placeholder="Search" />
          <Combobox.Empty />
          <Combobox.List>
            {(item: Language) => (
              <Combobox.Item key={item.value} value={item}>
                {item.emoji} {item.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 8:

```tsx
<div className="w-80">
      <Combobox value={value} onValueChange={setValue} items={items}>
        <Combobox.TriggerInput placeholder="Select database" />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: DatabaseItemWithDisabled) => (
              <Combobox.Item
                key={item.value}
                value={item}
                disabled={item.disabled}
              >
                <span>
                  {item.label}
                  {item.reason && (
                    <Text variant="secondary" size="xs" as="span">
                      {" - "}
                      {item.reason}
                    </Text>
                  )}
                </span>
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 9:

```tsx
<div className="w-80">
      <Combobox
        items={databases}
        value={value}
        onValueChange={setValue}
        label="Database"
        error={{ message: "Please select a database", match: true }}
      >
        <Combobox.TriggerInput placeholder="Select database" />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: DatabaseItem) => (
              <Combobox.Item key={item.value} value={item}>
                {item.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 10:

```tsx
<div className="flex flex-wrap items-center gap-4">
      <Combobox
        size="sm"
        value={smValue}
        onValueChange={(v) => setSmValue(v as string | null)}
        items={fruits.slice(0, 8)}
      >
        <Combobox.TriggerInput placeholder="Small (sm)" />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: string) => (
              <Combobox.Item key={item} value={item}>
                {item}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
      <Combobox
        size="base"
        value={baseValue}
        onValueChange={(v) => setBaseValue(v as string | null)}
        items={fruits.slice(0, 8)}
      >
        <Combobox.TriggerInput placeholder="Base (default)" />
        <Combobox.Content>
          <Combobox.Empty />
          <Combobox.List>
            {(item: string) => (
              <Combobox.Item key={item} value={item}>
                {item}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 11:

```tsx
<div className="flex flex-wrap items-center gap-4">
      <Combobox
        size="sm"
        value={smValue}
        onValueChange={(v) => setSmValue(v as Language)}
        items={languages}
      >
        <Combobox.TriggerValue className="w-[160px]" />
        <Combobox.Content>
          <Combobox.Input placeholder="Search" />
          <Combobox.Empty />
          <Combobox.List>
            {(item: Language) => (
              <Combobox.Item key={item.value} value={item}>
                {item.emoji} {item.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
      <Combobox
        size="base"
        value={baseValue}
        onValueChange={(v) => setBaseValue(v as Language)}
        items={languages}
      >
        <Combobox.TriggerValue className="w-[180px]" />
        <Combobox.Content>
          <Combobox.Input placeholder="Search" />
          <Combobox.Empty />
          <Combobox.List>
            {(item: Language) => (
              <Combobox.Item key={item.value} value={item}>
                {item.emoji} {item.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
```

Example 12:

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as Language)}
      items={languages}
    >
      <Combobox.Trigger
        render={
          <Button variant="ghost" size="sm" />
        }
      >
        <Combobox.Value>
          <span className="truncate">
            {value.emoji} {value.label}
          </span>
        </Combobox.Value>
        <CaretUpDownIcon size={14} className="text-kumo-subtle shrink-0" />
      </Combobox.Trigger>
      <Combobox.Content>
        <Combobox.Input placeholder="Search languages" />
        <Combobox.Empty />
        <Combobox.List>
          {(item: Language) => (
            <Combobox.Item key={item.value} value={item}>
              {item.emoji} {item.label}
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```


## CommandPalette

- Type: component
- Category: Navigation
- Import path: `@cloudflare/kumo`
- Description: CommandPalette - accessible command palette / spotlight search overlay.  Compound component: `CommandPalette.Root` (or `.Dialog` + `.Panel`), `.Input`, `.List`, `.Results`, `.Items`, `.Group`, `.GroupLabel`, `.Item`, `.ResultItem`, `.HighlightedText`, `.Empty`, `.Loading`, `.Footer`.  Built on `@base-ui/react/autocomplete` + `@base-ui/react/dialog`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `open` | boolean | Yes | - | - | Whether the dialog is open |
| `children` | ReactNode | No | - | - | Child content - typically one or more Panel components |
| `container` | PortalContainer | No | - | - | Container element for the portal. Use this to render the command palette inside a Shadow DOM or custom container. Overrides `KumoPortalProvider` context. |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-elevated`, `bg-kumo-overlay`, `bg-kumo-warning`, `ring-kumo-brand`, `ring-kumo-hairline`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<div className="flex flex-col items-start gap-4">
      <Button onClick={() => setOpen(true)}>Open Command Palette</Button>
      {selectedItem && (
        <p className="text-sm text-kumo-subtle">
          Last selected:{" "}
          <span className="text-kumo-default">{selectedItem}</span>
        </p>
      )}

      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={filteredGroups}
        value={search}
        onValueChange={setSearch}
        itemToStringValue={(group) => group.label}
        onSelect={(item, { newTab }) => {
          console.log("Selected:", item.title, newTab ? "(new tab)" : "");
          handleSelect(item);
        }}
        getSelectableItems={getSelectableItems}
      >
        <CommandPalette.Input placeholder="Type a command or search..." />
        <CommandPalette.List>
          <CommandPalette.Results>
            {(group: CommandGroup) => (
              <CommandPalette.Group key={group.id} items={group.items}>
                <CommandPalette.GroupLabel>
                  {group.label}
                </CommandPalette.GroupLabel>
                <CommandPalette.Items>
                  {(item: CommandItem) => (
                    <CommandPalette.Item
                      key={item.id}
                      value={item}
                      onClick={() => handleSelect(item)}
                    >
                      <span className="flex items-center gap-3">
                        {item.icon && (
                          <span className="text-kumo-subtle">{item.icon}</span>
                        )}
                        <span>{item.title}</span>
                      </span>
                    </CommandPalette.Item>
                  )}
                </CommandPalette.Items>
              </CommandPalette.Group>
            )}
          </CommandPalette.Results>
          <CommandPalette.Empty>No commands found</CommandPalette.Empty>
        </CommandPalette.List>
        <CommandPalette.Footer>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ??
            </kbd>
            <span>Navigate</span>
          </span>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ?
            </kbd>
            <span>Select</span>
          </span>
        </CommandPalette.Footer>
      </CommandPalette.Root>
    </div>
```

Example 2:

```tsx
<div>
      <Button onClick={() => setOpen(true)}>Open Simple Palette</Button>

      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={simpleItems}
        value={search}
        onValueChange={setSearch}
        itemToStringValue={(item) => item.title}
        onSelect={(item) => {
          console.log("Selected:", item.title);
          setOpen(false);
        }}
        getSelectableItems={(items) => items}
      >
        <CommandPalette.Input placeholder="Search actions..." />
        <CommandPalette.List>
          <CommandPalette.Results>
            {(item: SimpleItem) => (
              <CommandPalette.Item
                key={item.id}
                value={item}
                onClick={() => {
                  console.log("Clicked:", item.title);
                  setOpen(false);
                }}
              >
                {item.title}
              </CommandPalette.Item>
            )}
          </CommandPalette.Results>
          <CommandPalette.Empty>No actions found</CommandPalette.Empty>
        </CommandPalette.List>
      </CommandPalette.Root>
    </div>
```

Example 3:

```tsx
<div>
      <Button onClick={handleOpen}>Open with Loading</Button>

      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={loading ? [] : filteredGroups}
        value={search}
        onValueChange={setSearch}
        itemToStringValue={(group) => group.label}
        getSelectableItems={getSelectableItems}
      >
        <CommandPalette.Input placeholder="Search..." />
        <CommandPalette.List>
          {loading ? (
            <CommandPalette.Loading />
          ) : (
            <>
              <CommandPalette.Results>
                {(group: CommandGroup) => (
                  <CommandPalette.Group key={group.id} items={group.items}>
                    <CommandPalette.GroupLabel>
                      {group.label}
                    </CommandPalette.GroupLabel>
                    <CommandPalette.Items>
                      {(item: CommandItem) => (
                        <CommandPalette.Item
                          key={item.id}
                          value={item}
                          onClick={() => setOpen(false)}
                        >
                          <span className="flex items-center gap-3">
                            {item.icon && (
                              <span className="text-kumo-subtle">
                                {item.icon}
                              </span>
                            )}
                            <span>{item.title}</span>
                          </span>
                        </CommandPalette.Item>
                      )}
                    </CommandPalette.Items>
                  </CommandPalette.Group>
                )}
              </CommandPalette.Results>
              <CommandPalette.Empty>No results found</CommandPalette.Empty>
            </>
          )}
        </CommandPalette.List>
      </CommandPalette.Root>
    </div>
```

Example 4:

```tsx
<div className="flex flex-col items-start gap-4">
      <Button onClick={() => setOpen(true)}>
        Open Palette (No Autocomplete)
      </Button>

      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={filteredGroups}
        value={search}
        onValueChange={setSearch}
        itemToStringValue={(group) => group.label}
        onSelect={(item) => {
          console.log("Selected:", item.title);
          setOpen(false);
          setSearch("");
        }}
        getSelectableItems={getSelectableItems}
      >
        <CommandPalette.Input
          placeholder="Search commands..."
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="none"
          spellCheck={false}
          data-1p-ignore="true"
          data-lpignore="true"
        />
        <CommandPalette.List>
          <CommandPalette.Results>
            {(group: CommandGroup) => (
              <CommandPalette.Group key={group.id} items={group.items}>
                <CommandPalette.GroupLabel>
                  {group.label}
                </CommandPalette.GroupLabel>
                <CommandPalette.Items>
                  {(item: CommandItem) => (
                    <CommandPalette.Item
                      key={item.id}
                      value={item}
                      onClick={() => {
                        setOpen(false);
                        setSearch("");
                      }}
                    >
                      <span className="flex items-center gap-3">
                        {item.icon && (
                          <span className="text-kumo-subtle">{item.icon}</span>
                        )}
                        <span>{item.title}</span>
                      </span>
                    </CommandPalette.Item>
                  )}
                </CommandPalette.Items>
              </CommandPalette.Group>
            )}
          </CommandPalette.Results>
          <CommandPalette.Empty>No commands found</CommandPalette.Empty>
        </CommandPalette.List>
      </CommandPalette.Root>
    </div>
```

Example 5:

```tsx
<div>
      <Button onClick={() => setOpen(true)}>Open with ResultItem</Button>

      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={searchResults}
        value={search}
        onValueChange={setSearch}
        itemToStringValue={(item) => item.title}
        getSelectableItems={(items) => items}
      >
        <CommandPalette.Input placeholder="Search documentation..." />
        <CommandPalette.List>
          <CommandPalette.Results>
            {(item: SearchResult) => (
              <CommandPalette.ResultItem
                key={item.id}
                value={item}
                title={item.title}
                breadcrumbs={item.breadcrumbs}
                icon={item.icon}
                onClick={() => {
                  console.log("Navigate to:", item.title);
                  setOpen(false);
                }}
              />
            )}
          </CommandPalette.Results>
          <CommandPalette.Empty>No pages found</CommandPalette.Empty>
        </CommandPalette.List>
        <CommandPalette.Footer>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ??
            </kbd>
            <span>Navigate</span>
          </span>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ??
            </kbd>
            <span>Open in new tab</span>
          </span>
        </CommandPalette.Footer>
      </CommandPalette.Root>
    </div>
```


## DatePicker

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: DatePicker - a date selection calendar.  Built on [react-day-picker](https://daypicker.dev) with Kumo styling. Supports three selection modes: single, multiple, and range.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes |
| `children` | ReactNode | No | - | - | Child elements |

**Kumo Color Tokens**

`bg-kumo-base`

**Examples**

Example 1:

```tsx
<div className="flex flex-col gap-4">
      <DatePicker
        mode="single"
        selected={date}
        onChange={(d) => {
          if (d) {
            setDate(d);
          }
        }}
      />
      <p className="text-sm text-kumo-subtle">
        Selected: {date ? date.toLocaleDateString() : "None"}
      </p>
    </div>
```

Example 2:

```tsx
<div className="flex flex-col gap-4">
      <DatePicker
        mode="multiple"
        selected={dates}
        onChange={setDates}
        max={5}
      />
      <p className="text-sm text-kumo-subtle">
        Selected: {dates?.length ?? 0} date(s)
      </p>
    </div>
```

Example 3:

```tsx
<div className="flex flex-col gap-4">
      <DatePicker
        mode="range"
        selected={range}
        onChange={setRange}
        numberOfMonths={2}
      />
      <p className="text-sm text-kumo-subtle">
        Range:{" "}
        {range?.from
          ? `${range.from.toLocaleDateString()} - ${range.to?.toLocaleDateString() ?? "..."}`
          : "None"}
      </p>
    </div>
```

Example 4:

```tsx
<div className="flex flex-col gap-4">
      <DatePicker
        mode="range"
        selected={range}
        onChange={setRange}
        min={3}
        max={7}
        footer={
          <span className="text-xs text-kumo-subtle">Select 3-7 nights</span>
        }
      />
    </div>
```

Example 5:

```tsx
<Popover>
      <Popover.Trigger
        render={<Button variant="outline" icon={CalendarDotsIcon} />}
      >
        {date ? date.toLocaleDateString() : "Pick a date"}
      </Popover.Trigger>
      <Popover.Content className="p-3">
        <DatePicker mode="single" selected={date} onChange={setDate} />
      </Popover.Content>
    </Popover>
```

Example 6:

```tsx
<Popover>
      <Popover.Trigger
        render={<Button variant="outline" icon={CalendarDotsIcon} />}
      >
        {formatRange()}
      </Popover.Trigger>
      <Popover.Content className="p-3">
        <DatePicker
          mode="range"
          selected={range}
          onChange={setRange}
          numberOfMonths={2}
        />
      </Popover.Content>
    </Popover>
```

Example 7:

```tsx
<Popover>
      <Popover.Trigger
        render={<Button variant="outline" icon={CalendarDotsIcon} />}
      >
        {formatRange()}
      </Popover.Trigger>
      <Popover.Content className="p-0">
        <div className="flex">
          <div className="flex flex-col gap-1 border-r border-kumo-hairline p-2 text-sm">
            {presets.map((preset) => {
              const isActive = isPresetActive(preset);
              return (
                <button
                  key={preset.label}
                  type="button"
                  onClick={() => handlePresetClick(preset)}
                  className={`rounded-md px-3 py-1.5 text-left whitespace-nowrap ${
                    isActive
                      ? "bg-kumo-bg-inverse text-kumo-text-inverse"
                      : "text-kumo-subtle hover:bg-kumo-control"
                  }`}
                >
                  {preset.label}
                </button>
              );
            })}
          </div>
          <div className="p-3">
            <DatePicker
              mode="range"
              selected={range}
              onChange={setRange}
              month={month}
              onMonthChange={setMonth}
              numberOfMonths={2}
            />
          </div>
        </div>
      </Popover.Content>
    </Popover>
```

Example 8:

```tsx
<DatePicker
      mode="multiple"
      selected={dates}
      onChange={setDates}
      max={maxDays}
      disabled={unavailableDates}
      fixedWeeks
      footer={
        <p className="text-xs text-kumo-subtle pt-2 w-full">
          {selectedCount}/{maxDays} days selected. Grayed dates are unavailable.
        </p>
      }
    />
```


## DateRangePicker

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: DateRangePicker - dual-calendar date range selector.  Renders two side-by-side month calendars with click-to-select start/end dates, hover preview of the range, a timezone footer, and a reset button.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | base | `sm`, `base`, `lg` | Calendar size. - `"sm"` - Compact calendar for tight spaces - `"base"` - Default calendar size - `"lg"` - Large calendar for prominent date selection |
| `variant` | enum | No | default | `default`, `subtle` | Visual variant. - `"default"` - Standard appearance with overlay background - `"subtle"` - Minimal background |
| `timezone` | string | No | - | - | Display timezone string shown in the footer. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `onStartDateChange` | (date: Date \| null) => void | No | - | - | Callback when start date changes |
| `onEndDateChange` | (date: Date \| null) => void | No | - | - | Callback when end date changes |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-fill`, `bg-kumo-interact`, `bg-kumo-overlay`, `ring-kumo-focus`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Styling**

```json
{
  "sizeVariants": {
    "sm": {
      "height": 0,
      "classes": "p-3 gap-2",
      "dimensions": {
        "calendarWidth": 168,
        "cellHeight": 22,
        "cellWidth": 24,
        "textSize": 12,
        "iconSize": 14,
        "padding": 12,
        "gap": 8
      }
    },
    "base": {
      "height": 0,
      "classes": "p-4 gap-2.5",
      "dimensions": {
        "calendarWidth": 196,
        "cellHeight": 26,
        "cellWidth": 28,
        "textSize": 14,
        "iconSize": 16,
        "padding": 16,
        "gap": 10
      }
    },
    "lg": {
      "height": 0,
      "classes": "p-5 gap-3",
      "dimensions": {
        "calendarWidth": 252,
        "cellHeight": 32,
        "cellWidth": 36,
        "textSize": 16,
        "iconSize": 18,
        "padding": 20,
        "gap": 12
      }
    }
  }
}
```



## Dialog

- Type: component
- Category: Overlay
- Import path: `@cloudflare/kumo`
- Description: Dialog component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `children` | ReactNode | No | - | - | Dialog content (typically Title, Description, Close, and action buttons). |
| `container` | PortalContainer | No | - | - | Container element for the portal. Use this to render the dialog inside a Shadow DOM or custom container. Overrides `KumoPortalProvider` context. |
| `size` | enum | No | base | `base`, `sm`, `lg`, `xl` | Dialog width. - `"sm"` - Small (min 288px) for simple confirmations - `"base"` - Default (min 384px) - `"lg"` - Large (min 512px) for complex content - `"xl"` - Extra large (min 768px) for detailed views |

**Subcomponents**

### Dialog.Root

- Description: Root sub-component

Props:

- None

### Dialog.Trigger

- Description: Trigger sub-component

Props:

- None

### Dialog.Title

- Description: Title sub-component

Props:

- None

### Dialog.Description

- Description: Description sub-component

Props:

- None

### Dialog.Close

- Description: Close sub-component

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-recessed`, `ring-kumo-line`, `text-kumo-default`

**Styling**

```json
{
  "dimensions": {
    "sm": {
      "width": 350,
      "titleSize": 20,
      "descSize": 16,
      "padding": 16,
      "gap": 8,
      "buttonSize": "sm"
    },
    "base": {
      "width": 384,
      "titleSize": 20,
      "descSize": 16,
      "padding": 24,
      "gap": 16,
      "buttonSize": "base"
    },
    "lg": {
      "width": 512,
      "titleSize": 20,
      "descSize": 16,
      "padding": 24,
      "gap": 16,
      "buttonSize": "base"
    },
    "xl": {
      "width": 768,
      "titleSize": 20,
      "descSize": 16,
      "padding": 24,
      "gap": 16,
      "buttonSize": "base"
    }
  },
  "baseTokens": {
    "background": "color-surface",
    "text": "text-color-surface",
    "borderRadius": 12,
    "shadow": "shadow-m"
  },
  "backdrop": {
    "background": "color-surface-secondary",
    "opacity": 0.8
  },
  "header": {
    "title": {
      "fontWeight": 600,
      "color": "text-color-surface"
    },
    "closeIcon": {
      "name": "ph-x",
      "size": 20,
      "color": "text-color-muted"
    }
  },
  "description": {
    "fontWeight": 400,
    "color": "text-color-muted"
  },
  "buttons": {
    "primary": {
      "background": "color-primary",
      "text": "white"
    },
    "secondary": {
      "ring": "color-border",
      "text": "text-color-surface"
    }
  }
}
```


**Examples**

Example 1:

```tsx
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
```

Example 2:

```tsx
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
```

Example 3:

```tsx
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
```

Example 4:

```tsx
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
```

Example 5:

```tsx
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
```

Example 6:

```tsx
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
```

Example 7:

```tsx
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
```


## DropdownMenu

- Type: component
- Category: Overlay
- Import path: `@cloudflare/kumo`
- Description: DropdownMenu - accessible dropdown menu anchored to a trigger.  Compound component: `DropdownMenu` (Root), `.Trigger`, `.Content`, `.Item`, `.LinkItem`, `.CheckboxItem`, `.RadioGroup`, `.RadioItem`, `.RadioItemIndicator`, `.Sub`, `.SubTrigger`, `.SubContent`, `.Label`, `.Separator`, `.Shortcut`, `.Group`.  Built on `@base-ui/react/menu`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | default | `default`, `danger` | Visual style of the dropdown item. - `"default"` - Standard item appearance - `"danger"` - Destructive action with red text |

**Subcomponents**

### DropdownMenu.Trigger

- Description: Trigger sub-component

Props:

- None

### DropdownMenu.Portal

- Description: Portal sub-component (wraps DropdownMenuPrimitive)
- Pass through: Yes, base: `DropdownMenuPrimitive.Portal`

Props:

- None

### DropdownMenu.Sub

- Description: Sub sub-component (wraps DropdownMenuPrimitive)
- Pass through: Yes, base: `DropdownMenuPrimitive.SubmenuRoot`

Props:

- None

### DropdownMenu.SubTrigger

- Description: SubTrigger sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | Icon | No | - | - | Icon displayed before the label. |
| `inset` | boolean | No | - | - | Adds left padding to align with items that have icons. |

### DropdownMenu.SubContent

- Description: SubContent sub-component

Props:

- None

### DropdownMenu.Content

- Description: Content sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `sideOffset` | number | No | 8 | - | Distance in pixels from the trigger. |
| `container` | PortalContainer | No | - | - | Container element for the portal. Use this to render inside a Shadow DOM or custom container. |

### DropdownMenu.Item

- Description: Item sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | Icon \| ReactNode | No | - | - | Icon displayed before the label. |
| `variant` | "default" \| "danger" | No | "default" | - | Visual style of the item. |
| `selected` | boolean | No | - | - | Shows a check mark indicator when true. |
| `inset` | boolean | No | - | - | Adds left padding to align with items that have icons. |
| `onClick` | (event: React.MouseEvent) => void | No | - | - | Callback when the item is clicked. |
| `closeOnClick` | boolean | No | true | - | Whether the menu closes after clicking this item. |
| `disabled` | boolean | No | - | - | When true, the item cannot be interacted with. |

### DropdownMenu.LinkItem

- Description: LinkItem sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `href` | string | No | - | - | URL to navigate to when clicked. |
| `icon` | Icon \| ReactNode | No | - | - | Icon displayed before the label. |
| `variant` | "default" \| "danger" | No | "default" | - | Visual style of the item. |
| `inset` | boolean | No | - | - | Adds left padding to align with items that have icons. |
| `target` | string | No | - | - | Link target attribute (e.g. "_blank" for new tab). |
| `render` | ReactElement \| ((props, state) => ReactElement) | No | - | - | Custom element to render as the link. Use to integrate with framework routers (e.g. Next.js Link). |

### DropdownMenu.CheckboxItem

- Description: CheckboxItem sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `checked` | boolean | No | - | - | Whether the item is checked. |
| `defaultChecked` | boolean | No | false | - | Whether the item is initially checked (uncontrolled). |
| `onCheckedChange` | (checked: boolean, event: ChangeEventDetails) => void | No | - | - | Callback when the checked state changes. |
| `closeOnClick` | boolean | No | false | - | Whether the menu closes after clicking this item. |
| `disabled` | boolean | No | - | - | When true, the item cannot be interacted with. |

### DropdownMenu.RadioGroup

- Description: RadioGroup sub-component (wraps DropdownMenuPrimitive)
- Pass through: Yes, base: `DropdownMenuPrimitive.RadioGroup`

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `value` | any | No | - | - | The controlled value of the currently selected radio item. |
| `defaultValue` | any | No | - | - | The initially selected value (uncontrolled). |
| `onValueChange` | (value: any, event: ChangeEventDetails) => void | No | - | - | Callback when the selected value changes. |
| `disabled` | boolean | No | - | - | When true, all radio items in the group are disabled. |

### DropdownMenu.RadioItem

- Description: RadioItem sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `value` | any | Yes | - | - | The value of this radio item. |
| `icon` | Icon \| ReactNode | No | - | - | Icon displayed before the label. |
| `inset` | boolean | No | - | - | Adds left padding to align with items that have icons. |
| `closeOnClick` | boolean | No | false | - | Whether the menu closes after clicking this item. |
| `disabled` | boolean | No | - | - | When true, the item cannot be interacted with. |

### DropdownMenu.RadioItemIndicator

- Description: RadioItemIndicator sub-component

Props:

- None

### DropdownMenu.Label

- Description: Label sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `inset` | boolean | No | - | - | Adds left padding to align with items that have icons. |

### DropdownMenu.Separator

- Description: Separator sub-component

Props:

- None

### DropdownMenu.Shortcut

- Description: Shortcut sub-component

Props:

- None

### DropdownMenu.Group

- Description: Group sub-component (wraps DropdownMenuPrimitive)
- Pass through: Yes, base: `DropdownMenuPrimitive.Group`

Props:

- None

**Kumo Color Tokens**

`bg-kumo-control`, `bg-kumo-danger`, `bg-kumo-hairline`, `bg-kumo-overlay`, `bg-kumo-tint`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`

**Examples**

Example 1:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button icon={PlusIcon}>Add</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Item>Worker</DropdownMenu.Item>
        <DropdownMenu.Item>Pages</DropdownMenu.Item>
        <DropdownMenu.Item>KV Namespace</DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
```

Example 2:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button>View Options</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Group>
          <DropdownMenu.Label>Display</DropdownMenu.Label>
          <DropdownMenu.CheckboxItem
            checked={showSidebar}
            onCheckedChange={setShowSidebar}
          >
            Show sidebar
          </DropdownMenu.CheckboxItem>
          <DropdownMenu.CheckboxItem
            checked={showLineNumbers}
            onCheckedChange={setShowLineNumbers}
          >
            Show line numbers
          </DropdownMenu.CheckboxItem>
          <DropdownMenu.CheckboxItem
            checked={wordWrap}
            onCheckedChange={setWordWrap}
          >
            Word wrap
          </DropdownMenu.CheckboxItem>
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
```

Example 3:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button icon={UserIcon}>Account</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={UserIcon}>Profile</DropdownMenu.Item>
        <DropdownMenu.Item icon={CreditCardIcon}>Billing</DropdownMenu.Item>
        <DropdownMenu.Item icon={MoonIcon}>Dark mode</DropdownMenu.Item>

        {/* Language submenu with RadioGroup */}
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger>Language</DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent>
            <DropdownMenu.Group>
              <DropdownMenu.RadioGroup
                value={language}
                onValueChange={setLanguage}
              >
                {languages.map((lang) => (
                  <DropdownMenu.RadioItem key={lang.code} value={lang.code}>
                    {lang.label}
                    <DropdownMenu.RadioItemIndicator />
                  </DropdownMenu.RadioItem>
                ))}
              </DropdownMenu.RadioGroup>
            </DropdownMenu.Group>
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>

        {/* Timezone submenu with RadioGroup */}
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger>Set Timezone</DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent>
            <DropdownMenu.Group>
              <DropdownMenu.RadioGroup
                value={timezone}
                onValueChange={setTimezone}
              >
                {timezones.map((tz) => (
                  <DropdownMenu.RadioItem key={tz.value} value={tz.value}>
                    {tz.label}
                    <DropdownMenu.RadioItemIndicator />
                  </DropdownMenu.RadioItem>
                ))}
              </DropdownMenu.RadioGroup>
            </DropdownMenu.Group>
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>

        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={SignOutIcon} variant="danger">
          Log out
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
```

Example 4:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button>Edit</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={PencilSimpleIcon}>Rename</DropdownMenu.Item>
        <DropdownMenu.Item icon={CopyIcon}>Duplicate</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item inset>Move to folder</DropdownMenu.Item>
        <DropdownMenu.Item inset>Add to favorites</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={TrashIcon} variant="danger">
          Delete
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
```

Example 5:

```tsx
<div className="flex flex-col items-start gap-2">
      <DropdownMenu>
        <DropdownMenu.Trigger render={<Button>Actions</Button>} />
        <DropdownMenu.Content>
          <DropdownMenu.Item
            icon={CopyIcon}
            onClick={() => setLastAction("Duplicated")}
          >
            Duplicate
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={PencilSimpleIcon}
            onClick={() => setLastAction("Renamed")}
          >
            Rename
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            onClick={() => setLastAction("Deleted")}
          >
            Delete
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
      {lastAction && (
        <p className="text-sm text-kumo-subtle">
          Last action: <span className="text-kumo-default">{lastAction}</span>
        </p>
      )}
    </div>
```

Example 6:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger
        render={<button type="button" className="rounded-full" />}
      >
        <span className="flex h-8 w-8 items-center justify-center rounded-full bg-kumo-brand text-sm font-medium text-white">
          MR
        </span>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={UserIcon}>Profile</DropdownMenu.Item>
        <DropdownMenu.Item icon={GearIcon}>Settings</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={SignOutIcon} variant="danger">
          Log out
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
```

Example 7:

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button>Resources</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.LinkItem href="/settings" icon={GearIcon}>
          Settings
        </DropdownMenu.LinkItem>
        <DropdownMenu.LinkItem href="/docs" icon={BookOpenIcon}>
          Documentation
        </DropdownMenu.LinkItem>
        <DropdownMenu.Separator />
        <DropdownMenu.LinkItem
          href="https://developers.cloudflare.com"
          target="_blank"
          icon={ArrowSquareOutIcon}
        >
          Developer Docs
        </DropdownMenu.LinkItem>
      </DropdownMenu.Content>
    </DropdownMenu>
```


## Empty

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Placeholder shown when a list, table, or page has no content to display.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | base | `sm`, `base`, `lg` | Size of the empty state container. - `"sm"` - Compact empty state for smaller containers - `"base"` - Default empty state size - `"lg"` - Large empty state for prominent placement |
| `icon` | ReactNode | No | - | - | Decorative icon displayed above the title (e.g. from `@phosphor-icons/react`). |
| `title` | string | Yes | - | - | Primary heading text for the empty state. |
| `description` | string | No | - | - | Secondary description text displayed below the title. |
| `commandLine` | string | No | - | - | Shell command displayed in a copyable code block. |
| `contents` | ReactNode | No | - | - | Additional content (buttons, links) rendered below the description. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |

**Kumo Color Tokens**

`bg-kumo-control`, `bg-kumo-overlay`, `border-kumo-fill`, `border-kumo-interact`, `text-kumo-brand`, `text-kumo-default`, `text-kumo-inactive`, `text-kumo-subtle`, `text-kumo-success`

**Examples**

Example 1:

```tsx
<Empty
      icon={<PackageIcon size={48} />}
      title="No packages found"
      description="Get started by installing your first package."
      commandLine="npm install @cloudflare/kumo"
      contents={
        <div className="flex items-center gap-2">
          <Button icon={<CodeIcon />}>See examples</Button>
          <Button icon={<GlobeIcon />} variant="primary">
            View documentation
          </Button>
        </div>
      }
    />
```

Example 2:

```tsx
<div className="flex flex-col gap-8">
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Small</p>
        <Empty
          size="sm"
          icon={<Database size={32} className="text-kumo-inactive" />}
          title="No data available"
          description="There is no data to display."
        />
      </div>
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Base</p>
        <Empty
          size="base"
          icon={<Database size={48} className="text-kumo-inactive" />}
          title="No data available"
          description="There is no data to display."
        />
      </div>
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Large</p>
        <Empty
          size="lg"
          icon={<Database size={64} className="text-kumo-inactive" />}
          title="No data available"
          description="There is no data to display."
        />
      </div>
    </div>
```

Example 3:

```tsx
<Empty
      icon={<FolderOpen size={48} className="text-kumo-inactive" />}
      title="No projects found"
      description="Get started by creating your first project using the command below."
      commandLine="npm create kumo-project"
    />
```

Example 4:

```tsx
<Empty
      icon={<CloudSlash size={48} className="text-kumo-inactive" />}
      title="No connection"
      description="Unable to connect to the server. Please check your connection and try again."
      contents={
        <div className="flex gap-2">
          <Button variant="primary">Retry</Button>
          <Button variant="secondary">Go Back</Button>
        </div>
      }
    />
```

Example 5:

```tsx
<Empty title="Nothing here" />
```

Example 6:

```tsx
<Empty
      title="No results found"
      description="Try adjusting your search or filter to find what you're looking for."
    />
```


## Field

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Form field wrapper that provides a label, optional description, and error display around any form control. Built on Base UI Field primitives.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `controlFirst` | boolean | No | - | - | When `true`, places the control before the label (for checkbox/switch layouts). |
| `children` | ReactNode | No | - | - | The form control element(s) to wrap (Input, Select, Checkbox, etc.). |
| `label` | ReactNode | No | - | - | The label content - can be a string or any React node. |
| `required` | boolean | No | - | - | When explicitly `false`, shows gray "(optional)" text after the label. When `true` or `undefined`, no indicator is shown. |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content displayed next to the label via an info icon. |
| `error` | object | No | - | - | Validation error with a message and a browser `ValidityState` match key. |
| `description` | ReactNode | No | - | - | Helper text displayed below the control (hidden when `error` is present). |
| `hideLabel` | boolean | No | - | - | When `true`, Field renders layout, description, and error but skips the `<label>` element. Use when the child component provides its own accessible label (e.g. Select uses Base UI's `Select.Label` to avoid hover/focus coupling from native `<label>`). |

**Kumo Color Tokens**

`text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`


## Grid

- Type: component
- Category: Layout
- Import path: `@cloudflare/kumo`
- Description: Responsive CSS grid layout container with preset column configurations.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | No | - | - | Grid items to render. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |
| `mobileDivider` | boolean | No | - | - | Show dividers between grid items on mobile (only works with `"4up"` variant). |
| `gap` | enum | No | base | `none`, `sm`, `base`, `lg` | Gap size between grid items. - `"none"` - No gap - `"sm"` - 12px gap - `"base"` - Responsive gap (8px -> 24px -> 32px) - `"lg"` - 32px gap |
| `variant` | enum | No | - | `2up`, `side-by-side`, `2-1`, `1-2`, `1-3up`, `3up`, `4up`, `6up`, `1-2-4up` | Responsive column layout variant. - `"2up"` - 1 col -> 2 cols at md - `"side-by-side"` - Always 2 cols - `"2-1"` - 66%/33% split at md - `"1-2"` - 33%/66% split at md - `"3up"` - 1 -> 2 -> 3 cols - `"4up"` - 1 -> 2 -> 3 -> 4 cols - `"6up"` - 2 -> 3 -> 4 -> 6 cols - `"1-2-4up"` - 1 -> 2 -> 4 cols |

**Kumo Color Tokens**

`border-kumo-hairline`

**Examples**

Example 1:

```tsx
<Grid variant="2up" gap="base">
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 1</Text>
          <div className="mt-1">
            <Text variant="secondary">First grid item</Text>
          </div>
        </Surface>
      </GridItem>
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 2</Text>
          <div className="mt-1">
            <Text variant="secondary">Second grid item</Text>
          </div>
        </Surface>
      </GridItem>
    </Grid>
```

Example 2:

```tsx
<div className="flex flex-col gap-8">
      <div>
        <p className="mb-2 text-kumo-subtle">variant="2up"</p>
        <Grid variant="2up" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">variant="3up"</p>
        <Grid variant="3up" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>3</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">variant="4up"</p>
        <Grid variant="4up" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>3</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>4</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>
    </div>
```

Example 3:

```tsx
<div className="flex flex-col gap-8">
      <div>
        <p className="mb-2 text-kumo-subtle">variant="2-1" (66% / 33%)</p>
        <Grid variant="2-1" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4">
              <Text bold>Main Content</Text>
              <div className="mt-1">
                <Text variant="secondary">Two-thirds width</Text>
              </div>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4">
              <Text bold>Sidebar</Text>
              <div className="mt-1">
                <Text variant="secondary">One-third width</Text>
              </div>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">variant="1-2" (33% / 66%)</p>
        <Grid variant="1-2" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4">
              <Text bold>Sidebar</Text>
              <div className="mt-1">
                <Text variant="secondary">One-third width</Text>
              </div>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4">
              <Text bold>Main Content</Text>
              <div className="mt-1">
                <Text variant="secondary">Two-thirds width</Text>
              </div>
            </Surface>
          </GridItem>
        </Grid>
      </div>
    </div>
```

Example 4:

```tsx
<div className="flex flex-col gap-8">
      <div>
        <p className="mb-2 text-kumo-subtle">gap="none"</p>
        <Grid variant="side-by-side" gap="none">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">gap="sm"</p>
        <Grid variant="side-by-side" gap="sm">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">
          gap="base" (default, responsive)
        </p>
        <Grid variant="side-by-side" gap="base">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>

      <div>
        <p className="mb-2 text-kumo-subtle">gap="lg"</p>
        <Grid variant="side-by-side" gap="lg">
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>1</Text>
            </Surface>
          </GridItem>
          <GridItem>
            <Surface className="rounded-lg p-4 text-center">
              <Text>2</Text>
            </Surface>
          </GridItem>
        </Grid>
      </div>
    </div>
```

Example 5:

```tsx
<Grid variant="4up" gap="base" mobileDivider>
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 1</Text>
          <div className="mt-1">
            <Text variant="secondary">Has divider on mobile</Text>
          </div>
        </Surface>
      </GridItem>
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 2</Text>
          <div className="mt-1">
            <Text variant="secondary">Has divider on mobile</Text>
          </div>
        </Surface>
      </GridItem>
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 3</Text>
          <div className="mt-1">
            <Text variant="secondary">Has divider on mobile</Text>
          </div>
        </Surface>
      </GridItem>
      <GridItem>
        <Surface className="rounded-lg p-4">
          <Text bold>Item 4</Text>
          <div className="mt-1">
            <Text variant="secondary">Has divider on mobile</Text>
          </div>
        </Surface>
      </GridItem>
    </Grid>
```


## Input

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Input component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `label` | ReactNode | No | - | - | Label content for the input (enables Field wrapper) - can be a string or any React node |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label via an info icon |
| `description` | ReactNode | No | - | - | Helper text displayed below the input |
| `error` | string \| object | No | - | - | Error message or validation error object |
| `passwordManagerIgnore` | boolean | No | - | - | Suppress browser extension password manager overlays on non-credential inputs. |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | Input size. - `"xs"` - Extra small for compact UIs - `"sm"` - Small for secondary fields - `"base"` - Default size - `"lg"` - Large for prominent fields |
| `variant` | enum | No | default | `default`, `error` | Visual variant. - `"default"` - Standard input - `"error"` - Error state for validation failures |

**Kumo Color Tokens**

`bg-kumo-control`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-default`

**Styling**

```json
{
  "dimensions": {
    "xs": {
      "height": 20,
      "paddingX": 6,
      "fontSize": 12,
      "borderRadius": 2,
      "width": 160
    },
    "sm": {
      "height": 26,
      "paddingX": 8,
      "fontSize": 12,
      "borderRadius": 6,
      "width": 200
    },
    "base": {
      "height": 36,
      "paddingX": 12,
      "fontSize": 16,
      "borderRadius": 8,
      "width": 280
    },
    "lg": {
      "height": 40,
      "paddingX": 16,
      "fontSize": 16,
      "borderRadius": 8,
      "width": 320
    }
  },
  "baseTokens": {
    "background": "color-secondary",
    "text": "text-color-surface",
    "placeholder": "text-color-muted",
    "ring": "color-border"
  },
  "stateTokens": {
    "focus": {
      "ring": "color-active"
    },
    "error": {
      "ring": "color-error"
    },
    "disabled": {
      "opacity": 0.5,
      "text": "text-color-muted"
    }
  }
}
```


**Examples**

Example 1:

```tsx
<Input
      label="Email"
      placeholder="you@example.com"
      description="We'll never share your email"
    />
```

Example 2:

```tsx
<Input
      label="Username"
      placeholder="Choose a username"
      description="3-20 characters, alphanumeric only"
    />
```

Example 3:

```tsx
<Input
      label="Email"
      placeholder="you@example.com"
      value="invalid-email"
      error="Please enter a valid email address"
    />
```

Example 4:

```tsx
<Input
      label="Password"
      type="password"
      value="short"
      error={{
        message: "Password must be at least 8 characters",
        match: "tooShort",
      }}
      minLength={8}
    />
```

Example 5:

```tsx
<div className="flex flex-col gap-4">
      <Input size="xs" label="Extra Small" placeholder="Extra small input" />
      <Input size="sm" label="Small" placeholder="Small input" />
      <Input label="Base" placeholder="Base input (default)" />
      <Input size="lg" label="Large" placeholder="Large input" />
    </div>
```

Example 6:

```tsx
<Input label="Disabled field" placeholder="Cannot edit" disabled />
```

Example 7:

```tsx
<Input placeholder="Search..." aria-label="Search products" />
```

Example 8:

```tsx
<div className="flex flex-col gap-4">
      <Input
        aria-label="Hostname"
        placeholder="example.com"
        value="not a host"
        error="Please enter a valid hostname"
      />
      <Input
        aria-label="Path"
        placeholder="/api/v1/users"
        value="missing-slash"
        error={{ message: "Path must start with /", match: true }}
      />
    </div>
```

Example 9:

```tsx
<div className="flex flex-col gap-4">
      <Input
        label="API Key (default)"
        type="password"
        placeholder="<API_KEY>"
      />
      <Input
        label="API Key (passwordManagerIgnore)"
        type="password"
        placeholder="<API_KEY>"
        passwordManagerIgnore
      />
    </div>
```

Example 10:

```tsx
<div className="flex flex-col gap-4">
      <Input type="email" label="Email" placeholder="you@example.com" />
      <Input type="password" label="Password" placeholder="????????" />
      <Input type="number" label="Age" placeholder="18" />
      <Input type="tel" label="Phone" placeholder="+1 (555) 000-0000" />
    </div>
```

Example 11:

```tsx
<Input
      label="Phone Number"
      required={false}
      placeholder="+1 (555) 000-0000"
    />
```

Example 12:

```tsx
<Input
      label="API Key"
      labelTooltip="Find this in your dashboard under Settings > API Keys"
      placeholder="<API_KEY>"
    />
```

Example 13:

```tsx
<Input
      label={
        <span>
          Email for <strong>billing</strong>
        </span>
      }
      required
      placeholder="billing@company.com"
      type="email"
    />
```

Example 14:

```tsx
<Input
      label="With onChange"
      placeholder="Type something..."
      description={value ? `Value: ${value}` : "Uses e.target.value"}
      value={value}
      onChange={(e) => setValue(e.target.value)}
    />
```

Example 15:

```tsx
<Input
      label="With onValueChange"
      placeholder="Type something..."
      description={value ? `Value: ${value}` : "Receives the value directly"}
      value={value}
      onValueChange={(v) => setValue(v)}
    />
```


## InputArea

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo (synthetic - uses Input component)`
- Description: Multi-line textarea input with Input variants and InputArea-specific dimensions

**Props**

- None

**Styling**

```json
{
  "sizeVariants": {
    "xs": {
      "minHeight": 60,
      "width": 200
    },
    "sm": {
      "minHeight": 72,
      "width": 240
    },
    "base": {
      "minHeight": 88,
      "width": 320
    },
    "lg": {
      "minHeight": 100,
      "width": 360
    }
  }
}
```



## InputGroup

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: InputGroup component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `label` | ReactNode | No | - | - | The label content - can be a string or any React node. |
| `description` | ReactNode | No | - | - | Helper text displayed below the control (hidden when `error` is present). |
| `error` | object | No | - | - | Validation error with a message and a browser `ValidityState` match key. |
| `required` | boolean | No | - | - | When explicitly `false`, shows gray "(optional)" text after the label. When `true` or `undefined`, no indicator is shown. |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content displayed next to the label via an info icon. |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | `{"xs":"Extra small size.","sm":"Small size.","base":"Default size.","lg":"Large size."}` |
| `disabled` | boolean | No | - | - | - |

**Subcomponents**

### InputGroup.Input

- Description: Input sub-component

Props:

- None

### InputGroup.Button

- Description: Button sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `tooltip` | ReactNode | No | - | - | When provided, wraps the button in a Tooltip. Automatically sets aria-label from a string value. |
| `tooltipSide` | "top" \| "right" \| "bottom" \| "left" | No | "bottom" | - | Preferred side for the tooltip popup. |
| `variant` | "primary" \| "secondary" \| "ghost" \| "destructive" \| "secondary-destructive" \| "outline" | No | "ghost" | - | Button visual style. Defaults to ghost. |
| `size` | "xs" \| "sm" \| "base" \| "lg" | No | "sm" | - | Button size. |

### InputGroup.Addon

- Description: Addon sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `align` | "start" \| "end" | No | "start" | - | Position relative to the input. |
| `className` | string | No | - | - | Additional CSS classes. |

### InputGroup.Suffix

- Description: Suffix sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes. |

### InputGroup.Label

- Description: Label sub-component

Props:

- None

### InputGroup.Description

- Description: Description sub-component

Props:

- None

**Kumo Color Tokens**

`border-kumo-focus`, `border-kumo-line`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`

**Examples**

Example 1:

```tsx
<div className="w-full max-w-2xs">
      <InputGroup>
        <InputGroup.Input
          maxLength={20}
          onChange={handleChange}
          value={value}
        />
        <InputGroup.Suffix>.workers.dev</InputGroup.Suffix>
        {status !== "idle" && (
          <InputGroup.Addon align="end">
            {status === "loading" ? (
              <Loader />
            ) : (
              <CheckCircleIcon weight="duotone" className="text-kumo-success" />
            )}
          </InputGroup.Addon>
        )}
      </InputGroup>
    </div>
```

Example 2:

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Addon>
        <LinkIcon />
      </InputGroup.Addon>
      <InputGroup.Input placeholder="Paste a link..." aria-label="Link" />
    </InputGroup>
```

Example 3:

```tsx
<div className="flex flex-col gap-4">
      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Addon>@</InputGroup.Addon>
        <InputGroup.Input placeholder="username" aria-label="Username" />
      </InputGroup>

      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Input placeholder="email" aria-label="Email" />
        <InputGroup.Addon align="end">@example.com</InputGroup.Addon>
      </InputGroup>

      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Addon>/api/</InputGroup.Addon>
        <InputGroup.Input placeholder="endpoint" aria-label="API path" />
        <InputGroup.Addon align="end">.json</InputGroup.Addon>
      </InputGroup>
    </div>
```

Example 4:

```tsx
<div className="flex flex-col gap-4">
      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Input
          type={show ? "text" : "password"}
          defaultValue="<PASSWORD>"
          aria-label="Password"
        />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            shape="square"
            className="text-kumo-subtle"
            icon={show ? EyeSlashIcon : EyeIcon}
            aria-label={show ? "Hide password" : "Show password"}
            onClick={() => setShow(!show)}
          />
        </InputGroup.Addon>
      </InputGroup>

      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input
          value={searchValue}
          placeholder="Search"
          aria-label="Search"
          onChange={(e) => setSearchValue(e.target.value)}
        />
        {searchValue && (
          <InputGroup.Addon align="end" className="pr-1">
            <InputGroup.Button
              shape="square"
              icon={XIcon}
              aria-label="Clear search"
              onClick={() => setSearchValue("")}
            />
          </InputGroup.Addon>
        )}
        <InputGroup.Button variant="secondary" onClick={() => {}}>
          Search
        </InputGroup.Button>
      </InputGroup>
    </div>
```

Example 5:

```tsx
<InputGroup className="w-full max-w-2xs">
      <InputGroup.Addon>
        <MagnifyingGlassIcon />
      </InputGroup.Addon>
      <InputGroup.Input
        placeholder="Search with query language..."
        aria-label="Search"
      />
      <InputGroup.Addon align="end">
        <InputGroup.Button
          shape="square"
          className="text-kumo-subtle"
          icon={QuestionIcon}
          aria-label="Query language help"
          tooltip="Query language help"
          onClick={() => {}}
        />
      </InputGroup.Addon>
    </InputGroup>
```

Example 6:

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Addon>
        <MagnifyingGlassIcon />
      </InputGroup.Addon>
      <InputGroup.Input placeholder="Search..." aria-label="Search" />
      <InputGroup.Addon align="end">
        <kbd className="bg-none! border-none!">?K</kbd>
      </InputGroup.Addon>
    </InputGroup>
```

Example 7:

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Input defaultValue="kumo" aria-label="kumo" />
      <InputGroup.Addon align="end">
        <Loader />
      </InputGroup.Addon>
    </InputGroup>
```

Example 8:

```tsx
<div className="flex w-full max-w-2xs flex-col gap-4">
      <InputGroup label="Subdomain">
        <InputGroup.Input
          aria-label="Subdomain"
          defaultValue="kumo"
          maxLength={20}
        />
        <InputGroup.Suffix>.workers.dev</InputGroup.Suffix>
        <InputGroup.Addon align="end">
          <CheckCircleIcon weight="duotone" className="text-kumo-success" />
        </InputGroup.Addon>
      </InputGroup>

      <InputGroup
        label="Subdomain"
        error={{ message: "This subdomain is unavailable", match: true }}
      >
        <InputGroup.Input
          aria-label="Subdomain"
          defaultValue="kumo"
          maxLength={20}
        />
        <InputGroup.Suffix>.workers.dev</InputGroup.Suffix>
        <InputGroup.Addon align="end">
          <XCircleIcon weight="duotone" className="text-kumo-danger" />
        </InputGroup.Addon>
      </InputGroup>
    </div>
```

Example 9:

```tsx
<div className="flex w-full max-w-3xs flex-col gap-4">
      <InputGroup size="xs" label="Extra Small">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Extra small input" />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            className="text-kumo-subtle"
            icon={QuestionIcon}
            shape="square"
            aria-label="Help"
          />
        </InputGroup.Addon>
      </InputGroup>

      <InputGroup size="sm" label="Small">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Small input" />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            className="text-kumo-subtle"
            icon={QuestionIcon}
            shape="square"
            aria-label="Help"
          />
        </InputGroup.Addon>
      </InputGroup>

      <InputGroup label="Base (default)">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Base input" />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            className="text-kumo-subtle"
            icon={QuestionIcon}
            shape="square"
            aria-label="Help"
          />
        </InputGroup.Addon>
      </InputGroup>

      <InputGroup size="lg" label="Large">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Large input" />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            className="text-kumo-subtle"
            icon={QuestionIcon}
            shape="square"
            aria-label="Help"
          />
        </InputGroup.Addon>
      </InputGroup>
    </div>
```

Example 10:

```tsx
<div className="flex w-full max-w-3xs flex-col gap-4">
      <InputGroup
        label="Error State"
        error={{ message: "Please enter a valid email address", match: true }}
      >
        <InputGroup.Input type="email" defaultValue="invalid-email" />
        <InputGroup.Addon align="end">@example.com</InputGroup.Addon>
      </InputGroup>

      <InputGroup label="Disabled" disabled>
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Search..." />
      </InputGroup>

      <InputGroup label="Optional Field" required={false}>
        <InputGroup.Addon>$</InputGroup.Addon>
        <InputGroup.Input placeholder="0.00" />
      </InputGroup>

      <InputGroup
        label="With Description"
        description="Must be at least 8 characters"
        labelTooltip="Your password is stored securely"
      >
        <InputGroup.Input
          type={show ? "text" : "password"}
          placeholder="Password"
        />
        <InputGroup.Addon align="end">
          <InputGroup.Button
            shape="square"
            className="text-kumo-subtle"
            icon={show ? EyeSlashIcon : EyeIcon}
            aria-label={show ? "Hide password" : "Show password"}
            onClick={() => setShow(!show)}
          />
        </InputGroup.Addon>
      </InputGroup>
    </div>
```


## Label

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Label component for form fields.  Provides a standardized way to display labels with optional indicators: - Optional indicator: gray "(optional)" text when `showOptional={true}` - Tooltip: info icon with hover tooltip for additional context

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | No | - | - | The label content - can be a string or any React node. |
| `showOptional` | boolean | No | - | - | When `true`, shows gray "(optional)" text after the label. |
| `tooltip` | ReactNode | No | - | - | Tooltip content displayed next to the label via an info icon. |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `htmlFor` | string | No | - | - | The id of the form element this label is associated with |
| `asContent` | boolean | No | - | - | When true, only renders the inline content (indicators, tooltip) without the outer label element with font styling. Useful when composed inside another label element that already provides the text styling. |

**Kumo Color Tokens**

`text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<div className="flex flex-col gap-4">
      <Label>Default Label</Label>
      <Label showOptional>Optional Label</Label>
      <Label tooltip="More information about this field">
        Label with Tooltip
      </Label>
    </div>
```

Example 2:

```tsx
<Input label="Phone Number" required={false} placeholder="+1 555-0000" />
```

Example 3:

```tsx
<Input
      label="API Key"
      labelTooltip="Find this in your dashboard settings under API > Keys"
      placeholder="<API_KEY>"
    />
```

Example 4:

```tsx
<Checkbox
      label={
        <span>
          I agree to the <strong>Terms of Service</strong>
        </span>
      }
    />
```

Example 5:

```tsx
<div className="flex max-w-md flex-col gap-4">
      <Input label="Full Name" placeholder="John Doe" />
      <Input
        label="Email"
        labelTooltip="We'll send your receipt here"
        placeholder="john@example.com"
        type="email"
      />
      <Input label="Company" required={false} placeholder="Acme Inc." />
      <Select label="Country" placeholder="Select a country">
        <Select.Option value="us">United States</Select.Option>
        <Select.Option value="uk">United Kingdom</Select.Option>
        <Select.Option value="ca">Canada</Select.Option>
      </Select>
    </div>
```

Example 6:

```tsx
<div className="flex flex-col gap-3">
      <Label>Default</Label>
      <Label showOptional>Optional</Label>
      <Label tooltip="Important field">With Tooltip</Label>
    </div>
```


## LayerCard

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: LayerCard component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `render` | ReactNode | No | - | - | Allows you to replace the component's HTML element with a different tag, or compose it with another component.  Accepts a `ReactElement` or a function that returns the element to render. |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |

**Subcomponents**

### LayerCard.Primary

- Description: Primary sub-component

Props:

- None

### LayerCard.Secondary

- Description: Secondary sub-component

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-elevated`, `ring-kumo-fill`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-subtle`

**Styling**

```json
{
  "container": {
    "width": 280,
    "borderRadius": 8
  },
  "secondary": {
    "paddingX": 8,
    "paddingY": 8,
    "gap": 8,
    "fontSize": 16,
    "fontWeight": 500
  },
  "primary": {
    "paddingX": 16,
    "paddingY": 16,
    "paddingRight": 12,
    "gap": 8,
    "fontSize": 16,
    "fontWeight": 400,
    "borderRadius": 8
  }
}
```


**Examples**

Example 1:

```tsx
<LayerCard>
      <LayerCard.Secondary className="flex items-center justify-between">
        <div>Next Steps</div>
        <Button
          variant="ghost"
          size="sm"
          shape="square"
          aria-label="Go to next steps"
        >
          <ArrowRightIcon size={16} />
        </Button>
      </LayerCard.Secondary>

      <LayerCard.Primary>Get started with Kumo</LayerCard.Primary>
    </LayerCard>
```

Example 2:

```tsx
<LayerCard className="w-[250px]">
      <LayerCard.Secondary>Getting Started</LayerCard.Secondary>
      <LayerCard.Primary>
        <p className="text-sm text-kumo-subtle">
          Quick start guide for new users
        </p>
      </LayerCard.Primary>
    </LayerCard>
```

Example 3:

```tsx
<LayerCard className="w-[250px] p-4">
      <p className="text-sm text-kumo-subtle">Quick start guide for new users</p>
    </LayerCard>
```

Example 4:

```tsx
<LayerCard className="w-[250px]">
      <LayerCard.Secondary data-testid="card-header">
        Getting Started
      </LayerCard.Secondary>
      <LayerCard.Primary data-testid="card-body">
        <p className="text-sm text-kumo-subtle">
          Quick start guide for new users
        </p>
      </LayerCard.Primary>
    </LayerCard>
```

Example 5:

```tsx
<div className="flex gap-4">
      <LayerCard className="w-[200px]">
        <LayerCard.Secondary>Components</LayerCard.Secondary>
        <LayerCard.Primary>
          <p className="text-sm">Browse all components</p>
        </LayerCard.Primary>
      </LayerCard>
      <LayerCard className="w-[200px]">
        <LayerCard.Secondary>Examples</LayerCard.Secondary>
        <LayerCard.Primary>
          <p className="text-sm">View code examples</p>
        </LayerCard.Primary>
      </LayerCard>
    </div>
```


## Link

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Link component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | inline | `inline`, `current`, `plain` | Visual style of the link. - `"inline"` - Inline text link that flows with content - `"current"` - Link that inherits color from parent text - `"plain"` - Link without underline decoration |
| `to` | string | No | - | - | - |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |
| `download` | unknown | No | - | - | - |
| `href` | string | No | - | - | - |
| `hrefLang` | string | No | - | - | - |
| `media` | string | No | - | - | - |
| `ping` | string | No | - | - | - |
| `target` | React.HTMLAttributeAnchorTarget | No | - | - | - |
| `type` | string | No | - | - | - |
| `referrerPolicy` | enum | No | - | ``, `no-referrer`, `no-referrer-when-downgrade`, `origin`, `origin-when-cross-origin`, `same-origin`, `strict-origin`, `strict-origin-when-cross-origin`, `unsafe-url` | - |
| `render` | ReactNode | No | - | - | Allows you to replace the component's HTML element with a different tag, or compose it with another component.  Accepts a `ReactElement` or a function that returns the element to render. |

**Subcomponents**

### Link.ExternalIcon

- Description: ExternalIcon sub-component

Props:

- None

**Kumo Color Tokens**

`text-kumo-link`

**Examples**

Example 1:

```tsx
<div className="grid gap-x-6 gap-y-4 text-base md:grid-cols-3">
      <Link href="#">Default inline link</Link>
      <Link href="#" variant="current">
        Current color link
      </Link>
      <Link href="#" variant="plain">
        Plain inline link
      </Link>
    </div>
```

Example 2:

```tsx
<p className="mx-auto max-w-md text-base leading-relaxed text-kumo-default">
      This is a paragraph with an <Link href="#">inline link</Link> that flows
      naturally with the surrounding text. Links maintain proper underline
      offset for readability.
    </p>
```

Example 3:

```tsx
<Link
      href="https://cloudflare.com"
      target="_blank"
      rel="noopener noreferrer"
      className="text-base"
    >
      Visit Cloudflare <Link.ExternalIcon />
    </Link>
```

Example 4:

```tsx
<p className="text-base text-kumo-danger">
      This error message contains a{" "}
      <Link href="#" variant="current">
        link
      </Link>{" "}
      that inherits the red color from its parent.
    </p>
```

Example 5:

```tsx
<div className="flex flex-col gap-x-6 gap-y-4 text-base md:flex-row">
      <Link render={<CustomRouterLink href="/dashboard" />} variant="inline">
        Dashboard (via render)
      </Link>
      <Link
        render={
          <CustomRouterLink
            href="https://developers.cloudflare.com"
            target="_blank"
            rel="noopener noreferrer"
          />
        }
        variant="inline"
      >
        Cloudflare Docs <Link.ExternalIcon />
      </Link>
    </div>
```


## Loader

- Type: component
- Category: Feedback
- Import path: `@cloudflare/kumo`
- Description: Animated circular spinner for indicating loading states.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `size` | enum | No | base | `sm`, `base`, `lg` | Size of the spinner. Use a preset name or a custom pixel number. - `"sm"` - 16px, for inline use - `"base"` - 24px, default size - `"lg"` - 32px, for prominent loading states |

**Examples**

Example 1:

```tsx
<Loader />
```

Example 2:

```tsx
<div className="flex items-center gap-4">
      <Loader size="sm" />
      <Loader size="base" />
      <Loader size="lg" />
    </div>
```

Example 3:

```tsx
<Loader size={24} />
```

Example 4:

```tsx
<Loader className="text-kumo-subtle" />
```


## MenuBar

- Type: component
- Category: Navigation
- Import path: `@cloudflare/kumo`
- Description: MenuBar - horizontal icon-button toolbar with keyboard arrow-key navigation.  Each option renders as a `<button>` with a Tooltip. The active option is visually highlighted with an elevated background.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `isActive` | number \| boolean \| string | No | - | - | The currently active option value - matched against option index or `id`. |
| `options` | MenuOptionProps[] | Yes | - | - | Array of menu option configurations. |
| `optionIds` | boolean | No | - | - | When true, each option's `id` field is used for matching instead of its array index. |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-recessed`, `border-kumo-recessed`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-line`

**Styling**

```json
{
  "container": {
    "height": 32,
    "borderRadius": 8,
    "padding": 2,
    "gap": 2
  },
  "button": {
    "width": 36,
    "borderRadius": 6,
    "iconSize": 18
  }
}
```


**Examples**

Example 1:

```tsx
<MenuBar
      isActive={active}
      optionIds
      options={[
        {
          icon: <TextBolderIcon />,
          id: "bold",
          tooltip: "Bold",
          onClick: () => setActive(active === "bold" ? undefined : "bold"),
        },
        {
          icon: <TextItalicIcon />,
          id: "italic",
          tooltip: "Italic",
          onClick: () => setActive(active === "italic" ? undefined : "italic"),
        },
      ]}
    />
```

Example 2:

```tsx
<MenuBar
      isActive={active}
      optionIds
      options={[
        {
          icon: <TextBolderIcon />,
          id: "bold",
          tooltip: "Bold",
          onClick: () => setActive(active === "bold" ? undefined : "bold"),
        },
        {
          icon: <TextItalicIcon />,
          id: "italic",
          tooltip: "Italic",
          onClick: () => setActive(active === "italic" ? undefined : "italic"),
        },
      ]}
    />
```

Example 3:

```tsx
<MenuBar
      isActive={active}
      optionIds
      options={[
        {
          icon: <TextBolderIcon />,
          id: "bold",
          tooltip: "Bold",
          onClick: () => setActive(active === "bold" ? undefined : "bold"),
        },
        {
          icon: <TextItalicIcon />,
          id: "italic",
          tooltip: "Italic",
          onClick: () => setActive(active === "italic" ? undefined : "italic"),
        },
      ]}
    />
```


## Meter

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Progress bar showing a measured value within a known range (e.g. quota usage).

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `customValue` | string | No | - | - | Custom formatted value text (e.g. "750 / 1,000") displayed instead of percentage. |
| `label` | string | Yes | - | - | Label text displayed above the meter track. |
| `showValue` | boolean | No | - | - | Whether to display the percentage value next to the label. |
| `trackClassName` | string | No | - | - | Additional CSS classes for the track (background bar). |
| `indicatorClassName` | string | No | - | - | Additional CSS classes for the indicator (filled bar). |
| `value` | number | No | - | - | Current value of the meter |
| `max` | number | No | - | - | Maximum value of the meter (default: 100) |
| `min` | number | No | - | - | Minimum value of the meter (default: 0) |

**Kumo Color Tokens**

`bg-kumo-fill`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<Meter label="Storage used" value={65} />
```

Example 2:

```tsx
<Meter label="API requests" value={75} customValue="750 / 1,000" />
```

Example 3:

```tsx
<Meter label="Progress" value={40} showValue={false} />
```

Example 4:

```tsx
<Meter label="Quota reached" value={100} />
```

Example 5:

```tsx
<Meter label="Memory usage" value={15} />
```

Example 6:

```tsx
<Meter
      label="Upload progress"
      value={80}
      indicatorClassName="from-kumo-success via-kumo-success to-kumo-success"
    />
```


## Pagination

- Type: component
- Category: Navigation
- Import path: `@cloudflare/kumo`
- Description: Pagination component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `setPage` | (page: number) => void | Yes | - | - | Callback when page changes |
| `page` | number | No | - | - | Current page number (1-indexed). |
| `perPage` | number | No | - | - | Number of items displayed per page. |
| `totalCount` | number | No | - | - | Total number of items across all pages. |
| `className` | string | No | - | - | Additional CSS classes for the container |
| `labels` | PaginationLabels | No | - | - | Labels for internationalization of aria-labels. All labels have English defaults.  For visible text like "Showing X of Y", use render props on sub-components: - `Pagination.Info` children for the info text - `Pagination.PageSize` label prop for the "Per page:" text |
| `children` | ReactNode | No | - | - | Compound component children for custom layouts. Use Pagination.Info, Pagination.PageSize, Pagination.Controls, and Pagination.Separator. |
| `controls` | enum | No | full | `full`, `simple` | `{"full":"Full pagination controls with first, previous, page input, next, and last buttons","simple":"Simple pagination controls with only previous and next buttons"}` |
| `text` | object | No | - | - | - |

**Subcomponents**

### Pagination.Info

- Description: Info sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | (props: { | No | - | - | - |
| `page` | number | Yes | - | - | - |
| `perPage` | number | No | - | - | - |
| `totalCount` | number | No | - | - | - |
| `pageShowingRange` | string | Yes | - | - | - |

### Pagination.PageSize

- Description: PageSize sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `value` | number | Yes | - | - | - |
| `options` | number[] | No | - | - | - |
| `page` | " | Yes | - | - | - |
| `label` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |

### Pagination.Controls

- Description: Controls sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `Note` | ** `"dropdown"` renders an option for every page | Yes | - | - | - |
| `pageSelector` | "input" \| "dropdown" | No | - | - | - |
| `className` | string | No | - | - | - |

### Pagination.Separator

- Description: Separator sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`border-kumo-hairline`, `ring-kumo-hairline`, `text-kumo-subtle`

**Styling**

```json
{
  "layout": {
    "height": 36,
    "buttonSize": 36,
    "inputWidth": 50,
    "iconSize": 16,
    "gap": 8,
    "borderRadius": 6
  }
}
```


**Examples**

Example 1:

```tsx
<Pagination page={page} setPage={setPage} perPage={10} totalCount={100} />
```

Example 2:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={10}
      totalCount={100}
      controls="simple"
    />
```

Example 3:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={10}
      totalCount={100}
      controls="full"
    />
```

Example 4:

```tsx
<Pagination page={page} setPage={setPage} perPage={10} totalCount={100} />
```

Example 5:

```tsx
<Pagination page={page} setPage={setPage} perPage={25} totalCount={1250} />
```

Example 6:

```tsx
<Pagination
      text={({ perPage }: { perPage?: number }) =>
        `Page ${page} - showing ${perPage} per page`
      }
      page={page}
      setPage={setPage}
      perPage={25}
      totalCount={100}
    />
```

Example 7:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={perPage}
      totalCount={500}
    >
      <Pagination.Info />
      <Pagination.Separator />
      <Pagination.PageSize
        value={perPage}
        onChange={(size) => {
          setPerPage(size);
          setPage(1);
        }}
      />
      <Pagination.Controls />
    </Pagination>
```

Example 8:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={perPage}
      totalCount={200}
    >
      <Pagination.Info />
      <Pagination.Separator />
      <Pagination.PageSize
        value={perPage}
        onChange={(size) => {
          setPerPage(size);
          setPage(1);
        }}
        options={[10, 20, 50]}
      />
      <Pagination.Controls />
    </Pagination>
```

Example 9:

```tsx
<Pagination page={page} setPage={setPage} perPage={25} totalCount={100}>
      <Pagination.Info>
        {({ page, totalCount }) =>
          `Page ${page} of ${Math.ceil((totalCount ?? 1) / 25)}`
        }
      </Pagination.Info>
      <Pagination.Controls />
    </Pagination>
```

Example 10:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={perPage}
      totalCount={500}
    >
      <Pagination.Info />
      <Pagination.Separator />
      <Pagination.PageSize
        value={perPage}
        onChange={(size) => {
          setPerPage(size);
          setPage(1);
        }}
      />
      <Pagination.Controls pageSelector="dropdown" />
    </Pagination>
```

Example 11:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={perPage}
      totalCount={500}
    >
      <Pagination.Info />
      <div className="flex items-center gap-2">
        <Pagination.Controls />
        <Pagination.Separator />
        <Pagination.PageSize
          value={perPage}
          onChange={(size) => {
            setPerPage(size);
            setPage(1);
          }}
        />
      </div>
    </Pagination>
```

Example 12:

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={10}
      totalCount={100}
      labels={{
        firstPage: "Premie?re page",
        previousPage: "Page pre?ce?dente",
        nextPage: "Page suivante",
        lastPage: "Dernie?re page",
        pageNumber: "Nume?ro de page",
        pageSize: "Taille de page",
      }}
    >
      <Pagination.Info>
        {({ pageShowingRange, totalCount }) => (
          <>
            Affichage de{" "}
            <span className="tabular-nums">{pageShowingRange}</span> sur{" "}
            <span className="tabular-nums">{totalCount}</span>
          </>
        )}
      </Pagination.Info>
      <Pagination.Controls />
    </Pagination>
```


## Popover

- Type: component
- Category: Overlay
- Import path: `@cloudflare/kumo`
- Description: Popover component for displaying accessible popup content anchored to a trigger.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `side` | enum | No | bottom | `top`, `bottom`, `left`, `right` | Which side of the trigger the popover appears on. - `"top"` - Above the trigger - `"bottom"` - Below the trigger - `"left"` - Left of the trigger - `"right"` - Right of the trigger |

**Subcomponents**

### Popover.Trigger

- Description: Trigger sub-component

Props:

- None

### Popover.Content

- Description: Content sub-component

Props:

- None

### Popover.Title

- Description: Title sub-component

Props:

- None

### Popover.Description

- Description: Description sub-component

Props:

- None

### Popover.Close

- Description: Close sub-component

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `fill-kumo-base`, `fill-kumo-tip-shadow`, `fill-kumo-tip-stroke`, `outline-kumo-fill`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<Popover>
      <Popover.Trigger
        render={
          <Button shape="square" icon={BellIcon} aria-label="Notifications" />
        }
      />
      <Popover.Content>
        <Popover.Title>Notifications</Popover.Title>
        <Popover.Description>
          You are all caught up. Good job!
        </Popover.Description>
      </Popover.Content>
    </Popover>
```

Example 2:

```tsx
<Popover>
      <Popover.Trigger render={<Button />}>Open Popover</Popover.Trigger>
      <Popover.Content>
        <Popover.Title>Popover Title</Popover.Title>
        <Popover.Description>
          This is a basic popover with a title and description.
        </Popover.Description>
      </Popover.Content>
    </Popover>
```

Example 3:

```tsx
<Popover>
      <Popover.Trigger render={<Button />}>Open Settings</Popover.Trigger>
      <Popover.Content>
        <Popover.Title>Settings</Popover.Title>
        <Popover.Description>
          Configure your preferences below.
        </Popover.Description>
        <div className="mt-3">
          <Popover.Close render={<Button variant="secondary" size="sm" />}>
            Close
          </Popover.Close>
        </div>
      </Popover.Content>
    </Popover>
```

Example 4:

```tsx
<div className="flex flex-wrap gap-4">
      <Popover>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Bottom
        </Popover.Trigger>
        <Popover.Content side="bottom">
          <Popover.Title>Bottom</Popover.Title>
          <Popover.Description>
            Popover on bottom (default).
          </Popover.Description>
        </Popover.Content>
      </Popover>

      <Popover>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Top
        </Popover.Trigger>
        <Popover.Content side="top">
          <Popover.Title>Top</Popover.Title>
          <Popover.Description>Popover on top.</Popover.Description>
        </Popover.Content>
      </Popover>

      <Popover>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Left
        </Popover.Trigger>
        <Popover.Content side="left">
          <Popover.Title>Left</Popover.Title>
          <Popover.Description>Popover on left.</Popover.Description>
        </Popover.Content>
      </Popover>

      <Popover>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Right
        </Popover.Trigger>
        <Popover.Content side="right">
          <Popover.Title>Right</Popover.Title>
          <Popover.Description>Popover on right.</Popover.Description>
        </Popover.Content>
      </Popover>
    </div>
```

Example 5:

```tsx
<Popover>
      <Popover.Trigger render={<Button />}>User Profile</Popover.Trigger>
      <Popover.Content className="w-64">
        <div className="flex items-center gap-3">
          <div className="size-10 rounded-full bg-kumo-recessed" />
          <div>
            <Popover.Title>Jane Doe</Popover.Title>
            <p className="text-sm text-kumo-subtle">jane@example.com</p>
          </div>
        </div>
        <div className="mt-3 flex gap-2 border-t border-kumo-hairline pt-3">
          <Button variant="secondary" size="sm" className="flex-1">
            Profile
          </Button>
          <Popover.Close
            render={<Button variant="ghost" size="sm" className="flex-1" />}
          >
            Sign Out
          </Popover.Close>
        </div>
      </Popover.Content>
    </Popover>
```

Example 6:

```tsx
<Popover>
      <Popover.Trigger
        openOnHover
        delay={200}
        render={<Button variant="secondary" />}
      >
        Hover Me
      </Popover.Trigger>
      <Popover.Content>
        <Popover.Title>Hover Triggered</Popover.Title>
        <Popover.Description>
          This popover opens on hover with a 200ms delay. It can still contain
          interactive content like buttons and links.
        </Popover.Description>
        <div className="mt-3">
          <Popover.Close render={<Button variant="secondary" size="sm" />}>
            Got it
          </Popover.Close>
        </div>
      </Popover.Content>
    </Popover>
```

Example 7:

```tsx
<div className="w-full">
      <div className="overflow-hidden rounded-lg border border-kumo-hairline">
        <table className="w-full text-sm">
          <thead className="bg-kumo-elevated">
            <tr>
              <th className="px-4 py-2 text-left font-medium">Name</th>
              <th className="px-4 py-2 text-left font-medium">Status</th>
              <th className="w-12 px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-kumo-hairline">
            {rows.map((row) => (
              <tr
                key={row.id}
                ref={(el) => {
                  if (el) rowRefs.current.set(row.id, el);
                }}
                className={
                  selectedRow === row.id ? "bg-kumo-recessed" : "bg-kumo-base"
                }
              >
                <td className="px-4 py-2 font-mono">{row.name}</td>
                <td className="px-4 py-2 text-kumo-subtle">{row.status}</td>
                <td className="px-4 py-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    shape="square"
                    icon={DotsThree}
                    aria-label={`Actions for ${row.name}`}
                    onClick={() => handleEdit(row.id)}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Popover
        open={!!selectedRow}
        onOpenChange={(open) => !open && setSelectedRow(null)}
      >
        <Popover.Content
          side="left"
          anchor={
            anchorRect ? { getBoundingClientRect: () => anchorRect } : undefined
          }
        >
          <Popover.Title>
            Edit {rows.find((r) => r.id === selectedRow)?.name}
          </Popover.Title>
          <Popover.Description>
            The popover anchors to the selected row, not the icon button.
          </Popover.Description>
          <div className="mt-3">
            <Popover.Close render={<Button size="sm" variant="secondary" />}>
              Close
            </Popover.Close>
          </div>
        </Popover.Content>
      </Popover>
    </div>
```


## Radio

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Radio - radio button group for single-select choices.  Compound component: `Radio.Group` (with built-in Fieldset), `Radio.Item`, and `Radio.Legend`. Built on `@base-ui/react/radio-group` + `@base-ui/react/radio`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `legend` | string | No | - | - | Legend text for the group (required for accessibility). For more control over legend styling, omit this prop and use `<Radio.Legend>` as a child instead. |
| `children` | ReactNode | No | - | - | Child Radio.Item components (and optionally a Radio.Legend) |
| `orientation` | enum | No | - | `vertical`, `horizontal` | Layout direction of the radio items |
| `appearance` | enum | No | default | `default`, `card` | Visual appearance applied to all Radio.Item children. - `"default"` - Standard inline radio items - `"card"` - Choice card with border, padding, and highlighted selection state  Individual items can override this with their own `appearance` prop. |
| `error` | string | No | - | - | Error message for the group |
| `description` | ReactNode | No | - | - | Helper text for the group |
| `value` | string | No | - | - | Value of the radio that should be selected (controlled) |
| `disabled` | boolean | No | - | - | Whether all radios in the group are disabled |
| `controlPosition` | enum | No | - | `start`, `end` | Position of radio control relative to label: "start" puts radio before label, "end" puts label before radio. Defaults to "start" for default appearance and "end" for card appearance. |
| `name` | string | No | - | - | Form submission name for the radio group |
| `className` | string | No | - | - | Additional CSS classes |

**Subcomponents**

### Radio.Item

- Description: Item sub-component

Props:

- None

### Radio.Group

- Description: Group sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `legend` | string | No | - | - | - |
| `children` | ReactNode | Yes | - | - | - |
| `orientation` | "vertical" \| "horizontal" | No | - | - | - |
| `appearance` | KumoRadioAppearance | No | - | - | - |
| `error` | string | No | - | - | - |
| `description` | ReactNode | No | - | - | - |
| `value` | string | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `label` | "start" puts radio before label | Yes | - | - | - |
| `controlPosition` | RadioControlPosition | No | - | - | - |
| `name` | string | No | - | - | - |
| `className` | string | No | - | - | - |

### Radio.Legend

- Description: Legend sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-tint`, `border-kumo-danger`, `border-kumo-hairline`, `border-kumo-interact`, `ring-kumo-brand`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<Radio.Group
      legend="Notification preference"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item label="Email" value="email" />
      <Radio.Item label="SMS" value="sms" />
      <Radio.Item label="Push notification" value="push" />
    </Radio.Group>
```

Example 2:

```tsx
<Radio.Group legend="Account type" value={value} onValueChange={setValue}>
      <Radio.Item label="Personal" value="personal" />
      <Radio.Item label="Business" value="business" />
      <Radio.Item label="Enterprise" value="enterprise" />
    </Radio.Group>
```

Example 3:

```tsx
<Radio.Group
      legend="Size"
      orientation="horizontal"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item label="Small" value="sm" />
      <Radio.Item label="Medium" value="md" />
      <Radio.Item label="Large" value="lg" />
    </Radio.Group>
```

Example 4:

```tsx
<Radio.Group
      legend="Shipping method"
      description="Choose how you'd like to receive your order"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item label="Standard (5-7 days)" value="standard" />
      <Radio.Item label="Express (2-3 days)" value="express" />
      <Radio.Item label="Overnight" value="overnight" />
    </Radio.Group>
```

Example 5:

```tsx
<div className="grid grid-cols-2 gap-6">
      <Radio.Group
        legend="Payment method"
        error="Please select a payment method to continue"
      >
        <Radio.Item label="Credit Card" value="card" variant="error" />
        <Radio.Item label="PayPal" value="paypal" variant="error" />
      </Radio.Group>
      <Radio.Group
        legend="Payment method"
        appearance="card"
        error="Please select a payment method to continue"
      >
        <Radio.Item
          label="Credit Card"
          description="Pay with Visa, Mastercard, American Express, or Elo."
          value="card"
          variant="error"
        />
        <Radio.Item
          label="PayPal"
          description="Pay with your PayPal account."
          value="paypal"
          variant="error"
        />
      </Radio.Group>
    </div>
```

Example 6:

```tsx
<div className="grid grid-cols-2 gap-6">
      <Radio.Group legend="Disabled group" disabled defaultValue="a">
        <Radio.Item label="Option A" value="a" />
        <Radio.Item label="Option B" value="b" />
      </Radio.Group>
      <Radio.Group legend="Individual disabled" defaultValue="available">
        <Radio.Item label="Available" value="available" />
        <Radio.Item label="Unavailable" value="unavailable" disabled />
      </Radio.Group>
      <Radio.Group
        legend="Disabled card group"
        appearance="card"
        disabled
        defaultValue="a"
      >
        <Radio.Item
          label="Option A"
          description="This option is disabled."
          value="a"
        />
        <Radio.Item
          label="Option B"
          description="This option is disabled."
          value="b"
        />
      </Radio.Group>
      <Radio.Group
        legend="Individual disabled card"
        appearance="card"
        defaultValue="available"
      >
        <Radio.Item
          label="Available"
          description="This option can be selected."
          value="available"
        />
        <Radio.Item
          label="Unavailable"
          description="This option is not available."
          value="unavailable"
          disabled
        />
      </Radio.Group>
    </div>
```

Example 7:

```tsx
<Radio.Group legend="Preferences" controlPosition="end" defaultValue="a">
      <Radio.Item label="Label before radio" value="a" />
      <Radio.Item label="Another option" value="b" />
    </Radio.Group>
```

Example 8:

```tsx
<Radio.Group
      legend="Choose a plan"
      appearance="card"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item
        label="Free"
        description="For personal or hobby projects that aren't business-critical."
        value="free"
      />
      <Radio.Item
        label="Pro"
        description="For professional websites that aren't business-critical."
        value="pro"
      />
      <Radio.Item
        label="Business"
        description="For small businesses operating online."
        value="business"
      />
      <Radio.Item
        label="Contract"
        description="For mission-critical applications that are core to your business."
        value="contract"
      />
    </Radio.Group>
```

Example 9:

```tsx
<Radio.Group defaultValue="all" value={value} onValueChange={setValue}>
      <Radio.Legend className="sr-only">Paths</Radio.Legend>
      <Radio.Item label="Allow all paths" value="all" />
      <Radio.Item label="Restrict to specific paths" value="specific" />
    </Radio.Group>
```

Example 10:

```tsx
<Radio.Group value={value} onValueChange={setValue}>
      <Radio.Legend className="text-sm font-normal text-kumo-subtle">
        Notification preference
      </Radio.Legend>
      <Radio.Item label="Email" value="email" />
      <Radio.Item label="SMS" value="sms" />
      <Radio.Item label="Push notification" value="push" />
    </Radio.Group>
```

Example 11:

```tsx
<Radio.Group
      legend="Choose a plan"
      appearance="card"
      controlPosition="start"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item
        label="Free"
        description="For personal or hobby projects that aren't business-critical."
        value="free"
      />
      <Radio.Item
        label="Pro"
        description="For professional websites that aren't business-critical."
        value="pro"
      />
    </Radio.Group>
```

Example 12:

```tsx
<Radio.Group
      legend="Choose a plan"
      appearance="card"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item
        label={
          <span className="flex items-center gap-2">
            Free
            <Badge variant="neutral">$0</Badge>
          </span>
        }
        description="For personal or hobby projects."
        value="free"
      />
      <Radio.Item
        label={
          <span className="flex items-center gap-2">
            Pro
            <Badge variant="primary">Popular</Badge>
          </span>
        }
        description="For professional websites."
        value="pro"
      />
    </Radio.Group>
```

Example 13:

```tsx
<Radio.Group
      legend="Choose a plan"
      appearance="card"
      orientation="horizontal"
      value={value}
      onValueChange={setValue}
    >
      <Radio.Item
        label="Free"
        description="For personal or hobby projects that aren't business-critical."
        value="free"
      />
      <Radio.Item
        label="Pro"
        description="For professional websites that aren't business-critical."
        value="pro"
      />
      <Radio.Item
        label="Business"
        description="For small businesses operating online."
        value="business"
      />
      <Radio.Item
        label="Contract"
        description="For mission-critical applications that are core to your business."
        value="contract"
      />
    </Radio.Group>
```


## Select

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Select component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | Additional CSS classes merged via `cn()`. |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | Size of the select trigger. Matches Input component sizes. |
| `label` | ReactNode | No | - | - | Label content for the select. When provided, enables the Field wrapper with a visible label above the select. For accessibility without a visible label, use `aria-label` instead. |
| `hideLabel` | boolean | No | - | - | - |
| `placeholder` | string | No | - | - | Placeholder text shown when no value is selected. |
| `loading` | boolean | No | - | - | When `true`, shows a skeleton loader in place of the selected value. |
| `disabled` | boolean | No | - | - | Whether the select is disabled. |
| `required` | boolean | No | - | - | Whether the select is required. When `false`, shows "(optional)" text. |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content displayed next to the label via an info icon. |
| `value` | T | No | - | - | Currently selected value (controlled mode). |
| `children` | ReactNode | No | - | - | `Select.Option` elements to render in the dropdown. |
| `description` | ReactNode | No | - | - | Helper text displayed below the select. |
| `error` | string \| object | No | - | - | Error message string or validation error object with `match` key. |
| `onValueChange` | (value: T) => void | No | - | - | Callback when selection changes |
| `defaultValue` | T | No | - | - | Initial value for uncontrolled mode |
| `renderValue` | (value: T) => ReactNode | No | - | - | A function that returns a ReactNode to format the selected value in the trigger. Required when using object values. Use `placeholder` for the empty state. |
| `items` | Record<string, string> \| Array<{ label: ReactNode; value: T }> | No | - | - | Data structure of items rendered in the popup. Accepts a plain object map (`{ key: "Label" }`) or an array of `{ label, value }` for object/complex values. |
| `isItemEqualToValue` | (item: T, value: T) => boolean | No | - | - | Custom equality function for comparing items. Required when value is an object, since object identity (`===`) won't match across renders. |

**Subcomponents**

### Select.Option

- Description: Option sub-component

Props:

- None

### Select.Group

- Description: Group sub-component

Props:

- None

### Select.GroupLabel

- Description: GroupLabel sub-component

Props:

- None

### Select.Separator

- Description: Separator sub-component

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-elevated`, `bg-kumo-hairline`, `bg-kumo-tint`, `border-kumo-line`, `ring-kumo-brand`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-placeholder`, `text-kumo-subtle`

**Styling**

```json
{
  "trigger": {
    "height": 36,
    "paddingX": 12,
    "borderRadius": 8,
    "background": "bg-kumo-elevated",
    "text": "text-color-surface",
    "ring": "color-border",
    "fontSize": 16,
    "fontWeight": 400
  },
  "stateTokens": {
    "focus": {
      "ring": "color-active"
    },
    "disabled": {
      "opacity": 0.5
    }
  },
  "icons": {
    "caret": {
      "name": "ph-caret-up-down",
      "size": 20
    },
    "check": {
      "name": "ph-check",
      "size": 20
    }
  },
  "popup": {
    "background": "bg-kumo-elevated",
    "ring": "border-kumo-line",
    "borderRadius": 8,
    "padding": 6
  },
  "option": {
    "paddingX": 8,
    "paddingY": 6,
    "borderRadius": 4,
    "fontSize": 16,
    "highlightBackground": "color-surface-secondary"
  }
}
```


**Examples**

Example 1:

```tsx
<Select
      label="Favorite Fruit"
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v ?? "apple")}
      items={{ apple: "Apple", banana: "Banana", cherry: "Cherry" }}
    />
```

Example 2:

```tsx
<div className="grid gap-4">
      <div className="flex items-center gap-3">
        <span className="w-10 text-sm text-kumo-subtle">xs</span>
        <Select
          aria-label="Select size xs"
          size="xs"
          className="w-[200px]"
          placeholder="Choose..."
          items={{ a: "Option A", b: "Option B" }}
        />
      </div>
      <div className="flex items-center gap-3">
        <span className="w-10 text-sm text-kumo-subtle">sm</span>
        <Select
          aria-label="Select size sm"
          size="sm"
          className="w-[200px]"
          placeholder="Choose..."
          items={{ a: "Option A", b: "Option B" }}
        />
      </div>
      <div className="flex items-center gap-3">
        <span className="w-10 text-sm text-kumo-subtle">base</span>
        <Select
          aria-label="Select size base"
          size="base"
          className="w-[200px]"
          placeholder="Choose..."
          items={{ a: "Option A", b: "Option B" }}
        />
      </div>
      <div className="flex items-center gap-3">
        <span className="w-10 text-sm text-kumo-subtle">lg</span>
        <Select
          aria-label="Select size lg"
          size="lg"
          className="w-[200px]"
          placeholder="Choose..."
          items={{ a: "Option A", b: "Option B" }}
        />
      </div>
    </div>
```

Example 3:

```tsx
<Select
      aria-label="Select a fruit"
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v ?? "apple")}
      items={{ apple: "Apple", banana: "Banana", cherry: "Cherry" }}
    />
```

Example 4:

```tsx
<Select
      label="Issue Type"
      description="Choose the category that best describes your issue"
      className="w-[280px]"
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      items={{
        bug: "Bug",
        documentation: "Documentation",
        feature: "Feature",
      }}
    />
```

Example 5:

```tsx
<Select
      label="Issue Type"
      error="Please select an issue type"
      className="w-[280px]"
      value={null}
      items={{
        bug: "Bug",
        documentation: "Documentation",
        feature: "Feature",
      }}
    />
```

Example 6:

```tsx
<Select
      label="Category"
      placeholder="Choose a category..."
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      items={{
        bug: "Bug",
        documentation: "Documentation",
        feature: "Feature",
      }}
    />
```

Example 7:

```tsx
<Select
      label="Priority"
      labelTooltip="Higher priority issues are addressed first"
      placeholder="Select priority"
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      items={{
        low: "Low",
        medium: "Medium",
        high: "High",
        critical: "Critical",
      }}
    />
```

Example 8:

```tsx
<Select
      label="Language"
      className="w-[200px]"
      renderValue={(v) => (
        <span>
          {v.emoji} {v.label}
        </span>
      )}
      value={value}
      onValueChange={(v) => setValue(v as (typeof languages)[0])}
    >
      {languages.map((language) => (
        <Select.Option key={language.value} value={language}>
          {language.emoji} {language.label}
        </Select.Option>
      ))}
    </Select>
```

Example 9:

```tsx
<Select aria-label="Loading select" className="w-[200px]" loading />
```

Example 10:

```tsx
<Select
      label="Assignee"
      className="w-[200px]"
      loading={loading}
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      placeholder="Select assignee"
      items={items}
    />
```

Example 11:

```tsx
<Select
      label="Visible Columns"
      className="w-[250px]"
      multiple
      renderValue={(value) => {
        if (value.length > 3) {
          return (
            <span className="line-clamp-1">
              {value.slice(0, 2).join(", ") + ` and ${value.length - 2} more`}
            </span>
          );
        }
        return <span>{value.join(", ")}</span>;
      }}
      value={value}
      onValueChange={(v) => setValue(v as string[])}
    >
      <Select.Option value="Name">Name</Select.Option>
      <Select.Option value="Location">Location</Select.Option>
      <Select.Option value="Size">Size</Select.Option>
      <Select.Option value="Read">Read</Select.Option>
      <Select.Option value="Write">Write</Select.Option>
      <Select.Option value="CreatedAt">Created At</Select.Option>
    </Select>
```

Example 12:

```tsx
<Select
      label="Compliance Frameworks"
      className="w-[280px]"
      multiple
      value={value}
      onValueChange={(v) => setValue(v as string[])}
    >
      <Select.Option value="European Union Privacy Regulation">
        European Union Privacy Regulation
      </Select.Option>
      <Select.Option value="California Consumer Protection Act">
        California Consumer Protection Act
      </Select.Option>
      <Select.Option value="Health Insurance Portability Act">
        Health Insurance Portability Act
      </Select.Option>
      <Select.Option value="Payment Card Industry Standard">
        Payment Card Industry Standard
      </Select.Option>
    </Select>
```

Example 13:

```tsx
<Select
      label="Issue Types"
      className="w-[220px]"
      multiple
      renderValue={(selected) => (
        <span className="flex items-center gap-2">
          <span>Issue Types</span>
          {selected.length > 0 && (
            <Badge variant="neutral">{selected.length}</Badge>
          )}
        </span>
      )}
      value={value}
      onValueChange={(v) => setValue(v as string[])}
    >
      {allOptions.map((option) => (
        <Select.Option key={option.value} value={option.value}>
          {option.label}
        </Select.Option>
      ))}
    </Select>
```

Example 14:

```tsx
<Select
      label="Author"
      description="Select the primary author for this document"
      placeholder="Select an author"
      className="w-[200px]"
      onValueChange={(v) => setValue(v as (typeof authors)[0] | null)}
      value={value}
      isItemEqualToValue={(item, value) => item?.id === value?.id}
      renderValue={(author) => author.name}
    >
      {authors.map((author) => (
        <Select.Option key={author.id} value={author}>
          <div className="flex w-[300px] items-center justify-between gap-2">
            <Text>{author.name}</Text>
            <Text variant="secondary">{author.title}</Text>
          </div>
        </Select.Option>
      ))}
    </Select>
```

Example 15:

```tsx
<Select
      label="Deployment Region"
      placeholder="Choose a region..."
      className="w-[250px]"
      value={value}
      onValueChange={(v) => setValue(v as Region | null)}
      isItemEqualToValue={(item, val) => item.value === val.value}
    >
      {regions.map((region) => (
        <Select.Option
          key={region.value}
          value={region}
          disabled={region.disabled}
        >
          {region.label}
        </Select.Option>
      ))}
    </Select>
```

Example 16:

```tsx
<Select
      label="Plan"
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v as string | null)}
      items={{
        free: "Free",
        pro: "Pro",
        business: { label: "Business", disabled: true },
        enterprise: { label: "Enterprise", disabled: true },
      }}
    />
```

Example 17:

```tsx
<Select
      label="Food"
      placeholder="Pick a food..."
      className="w-[220px]"
      value={value}
      onValueChange={(v) => setValue(v as Food | null)}
      isItemEqualToValue={(item, val) => item.value === val.value}
    >
      <Select.Group>
        <Select.GroupLabel>Fruits</Select.GroupLabel>
        {foods.fruits.map((food) => (
          <Select.Option key={food.value} value={food}>
            {food.label}
          </Select.Option>
        ))}
      </Select.Group>
      <Select.Separator />
      <Select.Group>
        <Select.GroupLabel>Vegetables</Select.GroupLabel>
        {foods.vegetables.map((food) => (
          <Select.Option key={food.value} value={food}>
            {food.label}
          </Select.Option>
        ))}
      </Select.Group>
    </Select>
```

Example 18:

```tsx
<Select
      label="Server Region"
      placeholder="Select a region..."
      className="w-[260px]"
      value={value}
      onValueChange={(v) => setValue(v as ServerRegion | null)}
      isItemEqualToValue={(item, val) => item.value === val.value}
    >
      <Select.Group>
        <Select.GroupLabel>Available</Select.GroupLabel>
        {serverRegions.available.map((region) => (
          <Select.Option key={region.value} value={region}>
            {region.label}
          </Select.Option>
        ))}
      </Select.Group>
      <Select.Separator />
      <Select.Group>
        <Select.GroupLabel>Unavailable</Select.GroupLabel>
        {serverRegions.unavailable.map((region) => (
          <Select.Option key={region.value} value={region} disabled>
            {region.label}
          </Select.Option>
        ))}
      </Select.Group>
    </Select>
```

Example 19:

```tsx
<Select
      label="Long List Select"
      description="Tests scrolling behavior with many options"
      placeholder="Choose an option..."
      className="w-[220px]"
      value={value}
      onValueChange={(v) => setValue(v as LongListItem | null)}
      isItemEqualToValue={(item, val) => item.value === val.value}
    >
      {longListItems.map((item) => (
        <Select.Option key={item.value} value={item}>
          {item.label}
        </Select.Option>
      ))}
    </Select>
```


## SensitiveInput

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Password/secret input that masks its value by default and reveals on click. Includes a built-in copy-to-clipboard button on hover.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `alt` | string | No | - | - | - |
| `autoComplete` | React.HTMLInputAutoCompleteAttribute | No | - | - | - |
| `checked` | boolean | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `height` | number \| string | No | - | - | - |
| `list` | string | No | - | - | - |
| `name` | string | No | - | - | - |
| `placeholder` | string | No | - | - | - |
| `readOnly` | boolean | No | - | - | - |
| `required` | boolean | No | - | - | - |
| `width` | number \| string | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |
| `children` | ReactNode | No | - | - | - |
| `value` | string | No | - | - | Controlled value |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | Size of the input. - `"xs"` - Extra small for compact UIs - `"sm"` - Small for secondary fields - `"base"` - Default input size - `"lg"` - Large for prominent fields |
| `variant` | enum | No | default | `default`, `error` | Style variant of the input. - `"default"` - Default input appearance - `"error"` - Error state for validation failures |
| `label` | ReactNode | No | - | - | Label content for the input (enables Field wrapper and sets masked state label) - can be a string or any React node |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label via an info icon |
| `description` | ReactNode | No | - | - | Helper text displayed below the input |
| `error` | string \| object | No | - | - | Error message or validation error object |

**Kumo Color Tokens**

`bg-kumo-brand`, `bg-kumo-control`, `outline-kumo-focus`, `ring-kumo-brand`, `ring-kumo-focus`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<div className="w-80">
      <SensitiveInput label="API Key" defaultValue="sk_live_abc123xyz789" />
    </div>
```

Example 2:

```tsx
<div className="flex flex-col gap-4">
      {sizes.map((size) => (
        <div key={size} className="flex items-center gap-2">
          <span className="w-12 text-sm text-kumo-subtle">{size}</span>
          <SensitiveInput
            label={`${size} size`}
            size={size}
            defaultValue="<SECRET>"
          />
        </div>
      ))}
    </div>
```

Example 3:

```tsx
<div className="flex w-80 flex-col gap-4">
      <SensitiveInput
        label="Controlled Secret"
        value={value}
        onValueChange={setValue}
      />
      <div className="text-sm text-kumo-subtle">
        Current value: <code className="text-kumo-default">{value}</code>
      </div>
      <div className="flex gap-2">
        <Button
          onClick={() => setValue("<SECRET>-" + Date.now())}
          variant="primary"
          size="sm"
        >
          Change value
        </Button>
        <Button onClick={() => setValue("")} variant="secondary" size="sm">
          Clear
        </Button>
      </div>
    </div>
```

Example 4:

```tsx
<div className="flex w-80 flex-col gap-4">
      <SensitiveInput
        label="Error State"
        variant="error"
        defaultValue="invalid-key"
        error="This API key is not valid"
      />
      <SensitiveInput label="Disabled" defaultValue="cannot-edit" disabled />
      <SensitiveInput
        label="Read-only"
        defaultValue="<SECRET>"
        readOnly
      />
      <SensitiveInput
        label="With Description"
        defaultValue="<SECRET>"
        description="Keep this value secure and don't share it"
      />
    </div>
```


## Sidebar

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Sidebar - responsive navigation panel with expand/collapse support.  Compound component: `Sidebar` (root `<aside>`), `.Provider`, `.Header`, `.Content`, `.Footer`, `.Group`, `.GroupLabel`, `.Menu`, `.MenuItem`, `.MenuButton`, `.MenuBadge`, `.MenuSub`, `.MenuSubItem`, `.MenuSubButton`, `.Separator`, `.Trigger`, `.Rail`, `.MenuChevron`, `.Collapsible`, `.CollapsibleTrigger`, `.CollapsibleContent`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | Initial open state when uncontrolled. |
| `open` | boolean | No | - | - | Controlled open state. |
| `variant` | enum | No | sidebar | `sidebar`, `floating`, `inset` | Sidebar layout variant. |
| `side` | enum | No | left | `left`, `right` | Which side the sidebar is on. |
| `collapsible` | enum | No | icon | `icon`, `offcanvas`, `none` | `{"icon":"Collapses to show icons only","offcanvas":"Slides off screen when collapsed","none":"Cannot be collapsed"}` |
| `resizable` | boolean | No | - | - | Enable drag-to-resize on the sidebar edge. |
| `defaultWidth` | number | No | - | - | Initial width in pixels when resizable. |
| `minWidth` | number | No | - | - | Minimum width in pixels when resizing. |
| `maxWidth` | number | No | - | - | Maximum width in pixels when resizing. |
| `contained` | boolean | No | - | - | When true, the collapsed sidebar uses absolute positioning instead of fixed, keeping it scoped inside a bounded parent. Useful for demos and embedded sidebars. |
| `peekable` | boolean | No | - | - | When true, hovering or focusing the collapsed sidebar temporarily expands it. The `state` will be `"peeking"` during the peek. Moving away collapses it back. |
| `animationDuration` | number | No | - | - | Duration of sidebar expand/collapse animation in milliseconds. |
| `mobileBreakpoint` | number | No | - | - | Viewport width (in px) below which the sidebar renders as a mobile dialog sheet instead of the desktop aside rail. |
| `children` | ReactNode | No | - | - | Content - typically `<Sidebar>` + main content. |
| `className` | string | No | - | - | Additional CSS classes for the wrapper div. |

**Subcomponents**

### Sidebar.Provider

- Description: Provider sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `variant` | SidebarVariant | No | - | - | - |
| `side` | SidebarSide | No | - | - | - |
| `collapsible` | "icon" \| "offcanvas" \| "none" | No | - | - | - |
| `resizable` | boolean | No | - | - | - |
| `defaultWidth` | number | No | - | - | - |
| `minWidth` | number | No | - | - | - |
| `maxWidth` | number | No | - | - | - |
| `contained` | boolean | No | - | - | - |
| `peekable` | boolean | No | - | - | - |
| `animationDuration` | number | No | - | - | - |
| `mobileBreakpoint` | number | No | - | - | - |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

### Sidebar.Header

- Description: Header sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.Content

- Description: Content sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.Footer

- Description: Footer sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.Group

- Description: Group sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.GroupLabel

- Description: GroupLabel sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.Menu

- Description: Menu sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.MenuItem

- Description: MenuItem sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.MenuButton

- Description: MenuButton sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `icon` | React.ComponentType<{ className?: string | No | - | - | - |

### Sidebar.MenuBadge

- Description: MenuBadge sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `active` | boolean | No | - | - | - |
| `href` | string | No | - | - | - |

### Sidebar.MenuSub

- Description: MenuSub sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `active` | boolean | No | - | - | - |
| `href` | string | No | - | - | - |

### Sidebar.MenuSubItem

- Description: MenuSubItem sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `active` | boolean | No | - | - | - |
| `href` | string | No | - | - | - |

### Sidebar.MenuSubButton

- Description: MenuSubButton sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `active` | boolean | No | - | - | - |
| `href` | string | No | - | - | - |

### Sidebar.Separator

- Description: Separator sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `autoScrollOnOpen` | boolean | No | - | - | - |

### Sidebar.Trigger

- Description: Trigger sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `autoScrollOnOpen` | boolean | No | - | - | - |

### Sidebar.Rail

- Description: Rail sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `autoScrollOnOpen` | boolean | No | - | - | - |

### Sidebar.ResizeHandle

- Description: ResizeHandle sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `autoScrollOnOpen` | boolean | No | - | - | - |

### Sidebar.MenuChevron

- Description: MenuChevron sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `className` | string | No | - | - | - |

### Sidebar.Collapsible

- Description: Collapsible sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `defaultOpen` | boolean | No | - | - | - |
| `open` | boolean | No | - | - | - |
| `autoScrollOnOpen` | boolean | No | - | - | - |

### Sidebar.CollapsibleTrigger

- Description: CollapsibleTrigger sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `render` | React.ReactElement | Yes | - | - | - |

### Sidebar.CollapsibleContent

- Description: CollapsibleContent sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `activeKey` | string | Yes | - | - | - |
| `direction` | "left" \| "right" | No | - | - | - |

### Sidebar.SlidingViews

- Description: SlidingViews sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `activeKey` | string | Yes | - | - | - |
| `direction` | "left" \| "right" | No | - | - | - |

### Sidebar.SlidingView

- Description: SlidingView sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `value` | string | Yes | - | - | - |

**Kumo Color Tokens**

`bg-kumo-brand`, `bg-kumo-hairline`, `bg-kumo-line`, `bg-kumo-recessed`, `border-kumo-line`, `ring-kumo-brand`, `text-kumo-default`, `text-kumo-strong`, `text-kumo-subtle`

**Styling**

```json
{
  "width": {
    "expanded": "16.25rem",
    "icon": "57px"
  },
  "mobile": {
    "breakpoint": 768
  }
}
```


**Examples**

Example 1:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.GroupLabel>Overview</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon}>
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={GlobeIcon}>
                  Domains
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>

            <Sidebar.Group>
              <Sidebar.GroupLabel>Build</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuItem>
                  <Sidebar.Collapsible defaultOpen>
                    <Sidebar.CollapsibleTrigger
                      render={
                        <Sidebar.MenuButton icon={CodeIcon}>
                          Compute
                          <Sidebar.MenuChevron />
                        </Sidebar.MenuButton>
                      }
                    />
                    <Sidebar.CollapsibleContent>
                      <Sidebar.MenuSub>
                        <Sidebar.MenuSubItem>
                          <Sidebar.Collapsible>
                            <Sidebar.CollapsibleTrigger
                              render={
                                <Sidebar.MenuSubButton>
                                  Workers & Pages
                                  <Sidebar.MenuChevron />
                                </Sidebar.MenuSubButton>
                              }
                            />
                            <Sidebar.CollapsibleContent>
                              <Sidebar.MenuSub>
                                <Sidebar.MenuSubButton>
                                  Overview
                                </Sidebar.MenuSubButton>
                                <Sidebar.MenuSubButton>
                                  Workers
                                </Sidebar.MenuSubButton>
                                <Sidebar.MenuSubButton>
                                  Pages
                                </Sidebar.MenuSubButton>
                              </Sidebar.MenuSub>
                            </Sidebar.CollapsibleContent>
                          </Sidebar.Collapsible>
                        </Sidebar.MenuSubItem>
                        <Sidebar.MenuSubButton>
                          Durable Objects
                        </Sidebar.MenuSubButton>
                      </Sidebar.MenuSub>
                    </Sidebar.CollapsibleContent>
                  </Sidebar.Collapsible>
                </Sidebar.MenuItem>
                <Sidebar.MenuButton icon={DatabaseIcon}>
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
        </Sidebar>
        <DemoMain />
      </Sidebar.Provider>
    </DemoContainer>
```

Example 2:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} tooltip="Home" active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon} tooltip="Analytics">
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={CodeIcon} tooltip="Compute">
                  Compute
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={DatabaseIcon} tooltip="Storage">
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <DemoMain>
          <ToggleButton />
          <p>
            Click the button or the sidebar trigger to toggle
          </p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

Example 3:

```tsx
<DemoContainer>
      <Sidebar.Provider contained
        defaultOpen
        resizable
        defaultWidth={240}
        minWidth={180}
        maxWidth={400}
        className="min-h-0! h-full"
      >
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon}>
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={DatabaseIcon}>
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
          <Sidebar.ResizeHandle />
        </Sidebar>
        <DemoMain>
          <p>Drag the sidebar edge to resize</p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

Example 4:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen side="right" className="min-h-0! h-full">
        <DemoMain />
        <Sidebar>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.GroupLabel>Details</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={GearIcon} active>
                  Properties
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon}>
                  Metrics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={BellIcon}>Alerts</Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
        </Sidebar>
      </Sidebar.Provider>
    </DemoContainer>
```

Example 5:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen peekable className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} tooltip="Home" active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon} tooltip="Analytics">
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={CodeIcon} tooltip="Compute">
                  Compute
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={DatabaseIcon} tooltip="Storage">
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <DemoMain>
          <PeekStateIndicator />
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

Example 6:

```tsx
<div className="relative h-[420px] w-full overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
      <Sidebar.Provider contained defaultOpen className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.GroupLabel>Overview</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon}>
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={GlobeIcon}>
                  Domains
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>

            <Sidebar.Group>
              <Sidebar.GroupLabel>Platform</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={DatabaseIcon}>
                  Storage
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ShieldCheckIcon}>
                  Security
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={LockIcon}>
                  Zero Trust
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={GearIcon}>
                  Settings
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>

            <Sidebar.Group>
              <Sidebar.GroupLabel>Build</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuItem>
                  <Sidebar.Collapsible autoScrollOnOpen>
                    <Sidebar.CollapsibleTrigger
                      render={
                        <Sidebar.MenuButton icon={CodeIcon}>
                          Workers
                          <Sidebar.MenuChevron />
                        </Sidebar.MenuButton>
                      }
                    />
                    <Sidebar.CollapsibleContent>
                      <Sidebar.MenuSub>
                        <Sidebar.MenuSubButton>Overview</Sidebar.MenuSubButton>
                        <Sidebar.MenuSubButton>Deployments</Sidebar.MenuSubButton>
                        <Sidebar.MenuSubButton>Observability</Sidebar.MenuSubButton>
                        <Sidebar.MenuSubButton>Settings</Sidebar.MenuSubButton>
                      </Sidebar.MenuSub>
                    </Sidebar.CollapsibleContent>
                  </Sidebar.Collapsible>
                </Sidebar.MenuItem>
                <Sidebar.MenuButton icon={CubeIcon}>
                  Containers
                  <Sidebar.MenuBadge>Beta</Sidebar.MenuBadge>
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <DemoMain>
          <p>Open Workers near the bottom of the list</p>
        </DemoMain>
      </Sidebar.Provider>
    </div>
```

Example 7:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Header>
            <button
              type="button"
              onClick={() =>
                setSurface((s) => (s === "account" ? "zone" : "account"))
              }
              className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium text-kumo-default hover:bg-kumo-tint transition-colors"
            >
              <ArrowsLeftRightIcon className="size-4 shrink-0 text-kumo-brand" />
              <span className="flex-1 text-left font-semibold text-kumo-strong">
                {surface === "account" ? "Account Nav" : "Zone Nav"}
              </span>
            </button>
          </Sidebar.Header>

          <Sidebar.SlidingViews
            activeKey={surface}
            direction={surface === "zone" ? "left" : "right"}
          >
            <Sidebar.SlidingView value="account">
              <Sidebar.Content>
                <Sidebar.Group>
                  <Sidebar.GroupLabel>Account</Sidebar.GroupLabel>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton icon={HouseIcon} active>
                      Home
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={UserIcon}>
                      Members
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={ChartBarIcon}>
                      Analytics
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={GearIcon}>
                      Settings
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
              </Sidebar.Content>
            </Sidebar.SlidingView>

            <Sidebar.SlidingView value="zone">
              <Sidebar.Content>
                <Sidebar.Group>
                  <Sidebar.GroupLabel>Zone</Sidebar.GroupLabel>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton icon={GlobeIcon} active>
                      Overview
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={ShieldCheckIcon}>
                      Security
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={LockIcon}>
                      SSL/TLS
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={DatabaseIcon}>
                      Caching
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
              </Sidebar.Content>
            </Sidebar.SlidingView>
          </Sidebar.SlidingViews>
        </Sidebar>
        <DemoMain>
          <div className="flex flex-col items-center gap-2">
            <p className="font-medium text-kumo-default">
              Active: {surface === "account" ? "Account" : "Zone"} surface
            </p>
            <p>
              Click the header button to slide between views
            </p>
          </div>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

Example 8:

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen peekable className="min-h-0! h-full">
        <Sidebar>
          <Sidebar.Header>
            <AccountSwitcher />
          </Sidebar.Header>
          <Sidebar.SlidingViews
            activeKey={surface}
            direction={surface === "domain" ? "left" : "right"}
          >
            <Sidebar.SlidingView value="account">
              <Sidebar.Content>
                <Sidebar.Group>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton
                      icon={MagnifyingGlassIcon}
                      tooltip="Search"
                      className="ring ring-kumo-line group-data-[state=collapsed]/sidebar:ring-transparent mb-3 group-data-[state=collapsed]/sidebar:mb-0 transition-[margin] duration-(--sidebar-animation-duration)"
                    >
                      Quick search&hellip;
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
                <Sidebar.Group>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton icon={HouseIcon} active>
                      Home
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={ChartBarIcon}>
                      Analytics & Logs
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton
                      icon={GlobeIcon}
                      onClick={() => setSurface("domain")}
                    >
                      Domains
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>

                <Sidebar.Group>
                  <Sidebar.GroupLabel>Build</Sidebar.GroupLabel>
                  <Sidebar.Menu>
                    <Sidebar.MenuItem>
                      <Sidebar.Collapsible defaultOpen>
                        <Sidebar.CollapsibleTrigger
                          render={
                            <Sidebar.MenuButton icon={CodeIcon}>
                              Compute
                              <Sidebar.MenuChevron />
                            </Sidebar.MenuButton>
                          }
                        />
                        <Sidebar.CollapsibleContent>
                          <Sidebar.MenuSub>
                            <Sidebar.MenuSubItem>
                              <Sidebar.Collapsible>
                                <Sidebar.CollapsibleTrigger
                                  render={
                                    <Sidebar.MenuSubButton>
                                      Workers & Pages
                                      <Sidebar.MenuChevron />
                                    </Sidebar.MenuSubButton>
                                  }
                                />
                                <Sidebar.CollapsibleContent>
                                  <Sidebar.MenuSub>
                                    <Sidebar.MenuSubButton>
                                      Overview
                                    </Sidebar.MenuSubButton>
                                    <Sidebar.MenuSubButton>
                                      Workers
                                    </Sidebar.MenuSubButton>
                                    <Sidebar.MenuSubButton>
                                      Pages
                                    </Sidebar.MenuSubButton>
                                  </Sidebar.MenuSub>
                                </Sidebar.CollapsibleContent>
                              </Sidebar.Collapsible>
                            </Sidebar.MenuSubItem>
                            <Sidebar.MenuSubButton>
                              Durable Objects
                            </Sidebar.MenuSubButton>
                            <Sidebar.MenuSubButton>
                              Containers
                              <Sidebar.MenuBadge>Beta</Sidebar.MenuBadge>
                            </Sidebar.MenuSubButton>
                          </Sidebar.MenuSub>
                        </Sidebar.CollapsibleContent>
                      </Sidebar.Collapsible>
                    </Sidebar.MenuItem>
                    <Sidebar.MenuButton icon={DatabaseIcon}>
                      Storage
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>

                <Sidebar.Group>
                  <Sidebar.GroupLabel>Protect & Connect</Sidebar.GroupLabel>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton icon={ShieldCheckIcon}>
                      Security
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={LockIcon}>
                      Zero Trust
                      <Sidebar.MenuBadge>Beta</Sidebar.MenuBadge>
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
              </Sidebar.Content>
            </Sidebar.SlidingView>

            <Sidebar.SlidingView value="domain">
              <Sidebar.Content>
                <Sidebar.Group>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton
                      icon={ArrowLeftIcon}
                      onClick={() => setSurface("account")}
                    >
                      Back
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
                <Sidebar.Group>
                  <Sidebar.GroupLabel>example.com</Sidebar.GroupLabel>
                  <Sidebar.Menu>
                    <Sidebar.MenuButton icon={GlobeIcon} active>
                      Overview
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={ShieldCheckIcon}>
                      Security
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={LockIcon}>
                      SSL/TLS
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={ChartBarIcon}>
                      Analytics
                    </Sidebar.MenuButton>
                    <Sidebar.MenuButton icon={DatabaseIcon}>
                      Caching
                    </Sidebar.MenuButton>
                  </Sidebar.Menu>
                </Sidebar.Group>
              </Sidebar.Content>
            </Sidebar.SlidingView>
          </Sidebar.SlidingViews>

          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <DemoMain />
      </Sidebar.Provider>
    </DemoContainer>
```

Example 9:

```tsx
<div className="relative h-[540px] w-full overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
      <Sidebar.Provider contained mobileBreakpoint={9999} className="h-full">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.GroupLabel>Overview</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={ChartBarIcon}>
                  Analytics
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={GlobeIcon}>
                  Domains
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>

            <Sidebar.Group>
              <Sidebar.GroupLabel>Build</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={CodeIcon}>
                  Compute
                </Sidebar.MenuButton>
                <Sidebar.MenuButton icon={DatabaseIcon}>
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <Sidebar.Footer>
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <DemoMain>
          <MobileToggleButton />
          <p>Click the button to open the mobile sidebar</p>
          <p className="text-sm text-kumo-subtle">
            Press Escape or click the backdrop to close
          </p>
        </DemoMain>
      </Sidebar.Provider>
    </div>
```


## Surface

- Type: component
- Category: Layout
- Import path: `@cloudflare/kumo`
- Description: Surface component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `as` | React.ElementType | No | - | - | - |
| `render` | ReactNode | No | - | - | Allows you to replace the component's HTML element with a different tag, or compose it with another component.  Accepts a `ReactElement` or a function that returns the element to render. |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |


## Switch

- Type: component
- Category: Input
- Import path: `@cloudflare/kumo`
- Description: Switch component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | default | `default`, `neutral` | Visual variant: "default" (pill, brand color) or "neutral" (squircle, monochrome) |
| `label` | ReactNode | No | - | - | Label content for the switch (Field wrapper is built-in) - can be a string or any React node. Optional when used standalone for visual-only purposes. |
| `labelTooltip` | ReactNode | No | - | - | Tooltip content to display next to the label via an info icon |
| `required` | boolean | No | - | - | Whether the switch is required. When explicitly false, shows "(optional)" text after the label. |
| `controlFirst` | boolean | No | - | - | When true (default), switch appears before label. When false, label appears before switch. |
| `size` | enum | No | base | `sm`, `base`, `lg` | `{"sm":"Small switch for compact UIs","base":"Default switch size","lg":"Large switch for prominent toggles"}` |
| `checked` | boolean | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `transitioning` | boolean | No | - | - | - |
| `name` | string | No | - | - | - |
| `type` | enum | No | - | `submit`, `reset`, `button` | - |
| `value` | string \| string[] \| number | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |
| `onClick` | (event: React.MouseEvent) => void | Yes | - | - | Callback when switch is clicked |

**Subcomponents**

### Switch.Item

- Description: Item sub-component

Props:

- None

### Switch.Group

- Description: Group sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `legend` | string | No | - | - | - |
| `children` | ReactNode | Yes | - | - | - |
| `error` | string | No | - | - | - |
| `description` | ReactNode | No | - | - | - |
| `disabled` | boolean | No | - | - | - |
| `controlFirst` | boolean | No | - | - | - |
| `className` | string | No | - | - | - |

### Switch.Legend

- Description: Legend sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | Yes | - | - | - |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-base`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-hairline`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<Switch label="Switch" checked={checked} onCheckedChange={setChecked} />
```

Example 2:

```tsx
<Switch label="Switch" checked={false} onCheckedChange={() => {}} />
```

Example 3:

```tsx
<Switch label="Switch" checked={true} onCheckedChange={() => {}} />
```

Example 4:

```tsx
<Switch label="Disabled" checked={false} disabled />
```

Example 5:

```tsx
<Switch
      label="Neutral switch"
      variant="neutral"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

Example 6:

```tsx
<div className="flex flex-col gap-4">
      <Switch
        label="Neutral off"
        variant="neutral"
        checked={false}
        onCheckedChange={() => {}}
      />
      <Switch
        label="Neutral on"
        variant="neutral"
        checked={true}
        onCheckedChange={() => {}}
      />
      <Switch
        label="Neutral disabled"
        variant="neutral"
        checked={false}
        disabled
      />
    </div>
```

Example 7:

```tsx
<div className="grid grid-cols-2 gap-x-8 gap-y-4">
      <Switch label="Default off" checked={false} onCheckedChange={() => {}} />
      <Switch label="Default on" checked={true} onCheckedChange={() => {}} />
      <Switch
        label="Neutral off"
        variant="neutral"
        checked={false}
        onCheckedChange={() => {}}
      />
      <Switch
        label="Neutral on"
        variant="neutral"
        checked={true}
        onCheckedChange={() => {}}
      />
    </div>
```

Example 8:

```tsx
<Switch
      id="my-custom-switch"
      label="Custom ID"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

Example 9:

```tsx
<Switch.Group legend="Notification settings">
      <Switch.Item label="Email notifications" />
      <Switch.Item label="SMS notifications" />
      <Switch.Item label="Push notifications" />
    </Switch.Group>
```

Example 10:

```tsx
<Switch.Group>
      <Switch.Legend className="sr-only">Notification settings</Switch.Legend>
      <Switch.Item label="Email notifications" />
      <Switch.Item label="SMS notifications" />
      <Switch.Item label="Push notifications" />
    </Switch.Group>
```

Example 11:

```tsx
<Switch.Group>
      <Switch.Legend className="text-sm font-normal text-kumo-subtle">
        Notification settings
      </Switch.Legend>
      <Switch.Item label="Email notifications" />
      <Switch.Item label="SMS notifications" />
      <Switch.Item label="Push notifications" />
    </Switch.Group>
```

Example 12:

```tsx
<div className="flex flex-col gap-4">
      <Switch
        label="Small"
        size="sm"
        checked={true}
        onCheckedChange={() => {}}
      />
      <Switch
        label="Base (default)"
        size="base"
        checked={true}
        onCheckedChange={() => {}}
      />
      <Switch
        label="Large"
        size="lg"
        checked={true}
        onCheckedChange={() => {}}
      />
    </div>
```


## Table

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: Table - semantic HTML table with styled rows, cells, and selection support.  Compound component: `Table` (Root), `.Header`, `.Head`, `.Body`, `.Row`, `.Cell`, `.Footer`, `.CheckCell`, `.CheckHead`, `.ResizeHandle`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `layout` | enum | No | auto | `auto`, `fixed` | `{"auto":"Auto table layout - columns resize based on content","fixed":"Fixed table layout - columns have equal width, controlled via colgroup"}` |
| `variant` | enum | No | default | `default`, `selected` | `{"default":"Default row variant","selected":"Selected row variant"}` |
| `sticky` | enum | No | - | `left`, `right` | `{"left":"Pin column to the left edge of the scroll container","right":"Pin column to the right edge of the scroll container"}` |
| `className` | string | No | - | - | Additional CSS classes |
| `children` | ReactNode | No | - | - | Child elements |

**Subcomponents**

### Table.Header

- Description: Header sub-component

Props:

- None

### Table.Head

- Description: Head sub-component

Props:

- None

### Table.Row

- Description: Row sub-component

Props:

- None

### Table.Body

- Description: Body sub-component

Props:

- None

### Table.Cell

- Description: Cell sub-component

Props:

- None

### Table.CheckCell

- Description: CheckCell sub-component

Props:

- None

### Table.CheckHead

- Description: CheckHead sub-component

Props:

- None

### Table.Footer

- Description: Footer sub-component

Props:

- None

### Table.ResizeHandle

- Description: ResizeHandle sub-component

Props:

- None

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-elevated`, `bg-kumo-hairline`, `bg-kumo-tint`, `border-kumo-fill`, `ring-kumo-brand`, `text-kumo-default`, `text-kumo-strong`

**Examples**

Example 1:

```tsx
<LayerCard className="p-0">
      <Table>
        <Table.Header>
          <Table.Row>
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.slice(0, 3).map((row) => (
            <Table.Row key={row.id}>
              <Table.Cell>{row.subject}</Table.Cell>
              <Table.Cell>{row.from}</Table.Cell>
              <Table.Cell>{row.date}</Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 2:

```tsx
<LayerCard className="p-0">
      <Table>
        <Table.Header>
          <Table.Row>
            <Table.CheckHead
              checked={selectedIds.size === rows.length}
              indeterminate={
                selectedIds.size > 0 && selectedIds.size < rows.length
              }
              onCheckedChange={toggleAll}
              aria-label="Select all rows"
            />
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.map((row) => (
            <Table.Row key={row.id}>
              <Table.CheckCell
                checked={selectedIds.has(row.id)}
                onCheckedChange={() => toggleRow(row.id)}
                aria-label={`Select ${row.subject}`}
              />
              <Table.Cell>{row.subject}</Table.Cell>
              <Table.Cell>{row.from}</Table.Cell>
              <Table.Cell>{row.date}</Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 3:

```tsx
<LayerCard className="p-0">
      <Table>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.slice(0, 3).map((row) => (
            <Table.Row key={row.id}>
              <Table.Cell>{row.subject}</Table.Cell>
              <Table.Cell>{row.from}</Table.Cell>
              <Table.Cell>{row.date}</Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 4:

```tsx
<LayerCard className="p-0">
      <Table>
        <Table.Header>
          <Table.Row>
            <Table.CheckHead
              checked={selectedIds.size === rows.length}
              indeterminate={
                selectedIds.size > 0 && selectedIds.size < rows.length
              }
              onCheckedChange={toggleAll}
              aria-label="Select all rows"
            />
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.map((row) => (
            <Table.Row
              key={row.id}
              variant={selectedIds.has(row.id) ? "selected" : "default"}
            >
              <Table.CheckCell
                checked={selectedIds.has(row.id)}
                onCheckedChange={() => toggleRow(row.id)}
                aria-label={`Select ${row.subject}`}
              />
              <Table.Cell>{row.subject}</Table.Cell>
              <Table.Cell>{row.from}</Table.Cell>
              <Table.Cell>{row.date}</Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 5:

```tsx
<LayerCard className="p-0">
      <Table layout="fixed">
        <colgroup>
          <col />
          <col className="w-[150px]" />
          <col className="w-[150px]" />
        </colgroup>
        <Table.Header>
          <Table.Row>
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.map((row) => (
            <Table.Row key={row.id}>
              <Table.Cell>{row.subject}</Table.Cell>
              <Table.Cell>{row.from}</Table.Cell>
              <Table.Cell>{row.date}</Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 6:

```tsx
<LayerCard className="w-full max-w-md overflow-x-auto p-0">
      <Table>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
            <Table.Head>Tags</Table.Head>
            <Table.Head sticky="right">
              <span className="sr-only">Actions</span>
            </Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.map((row) => (
            <Table.Row key={row.id}>
              <Table.Cell className="whitespace-nowrap">
                {row.subject}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.from}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.date}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.tags ? (
                  <div className="inline-flex gap-1">
                    {row.tags.map((tag) => (
                      <Badge key={tag}>{tag}</Badge>
                    ))}
                  </div>
                ) : (
                  "-"
                )}
              </Table.Cell>
              <Table.Cell sticky="right" className="text-right">
                <DropdownMenu>
                  <DropdownMenu.Trigger
                    render={
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label="More options"
                      >
                        <DotsThree weight="bold" size={16} />
                      </Button>
                    }
                  />
                  <DropdownMenu.Content>
                    <DropdownMenu.Item icon={Eye}>View</DropdownMenu.Item>
                    <DropdownMenu.Item icon={PencilSimple}>
                      Edit
                    </DropdownMenu.Item>
                    <DropdownMenu.Separator />
                    <DropdownMenu.Item icon={Trash} variant="danger">
                      Delete
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 7:

```tsx
<LayerCard className="w-full max-w-md overflow-x-auto p-0">
      <Table>
        <Table.Header>
          <Table.Row>
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
            <Table.Head>Tags</Table.Head>
            <Table.Head sticky="right">
              <span className="sr-only">Actions</span>
            </Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.map((row) => (
            <Table.Row key={row.id}>
              <Table.Cell className="whitespace-nowrap">
                {row.subject}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.from}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.date}
              </Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.tags ? (
                  <div className="inline-flex gap-1">
                    {row.tags.map((tag) => (
                      <Badge key={tag}>{tag}</Badge>
                    ))}
                  </div>
                ) : (
                  "-"
                )}
              </Table.Cell>
              <Table.Cell sticky="right" className="text-right">
                <DropdownMenu>
                  <DropdownMenu.Trigger
                    render={
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label="More options"
                      >
                        <DotsThree weight="bold" size={16} />
                      </Button>
                    }
                  />
                  <DropdownMenu.Content>
                    <DropdownMenu.Item icon={Eye}>View</DropdownMenu.Item>
                    <DropdownMenu.Item icon={PencilSimple}>
                      Edit
                    </DropdownMenu.Item>
                    <DropdownMenu.Separator />
                    <DropdownMenu.Item icon={Trash} variant="danger">
                      Delete
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```

Example 8:

```tsx
<LayerCard className="w-full overflow-x-auto p-0">
      <Table layout="fixed">
        <colgroup>
          <col />{" "}
          {/* Checkbox column - width handled by Table.CheckHead/CheckCell */}
          <col />
          <col style={{ width: "150px" }} />
          <col style={{ width: "120px" }} />
          <col style={{ width: "50px" }} />
        </colgroup>
        <Table.Header>
          <Table.Row>
            <Table.CheckHead
              checked={selectedIds.size === emailData.length}
              indeterminate={
                selectedIds.size > 0 && selectedIds.size < emailData.length
              }
              onCheckedChange={toggleAll}
              aria-label="Select all rows"
            />
            <Table.Head>Subject</Table.Head>
            <Table.Head>From</Table.Head>
            <Table.Head>Date</Table.Head>
            <Table.Head></Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {emailData.map((row) => (
            <Table.Row
              key={row.id}
              variant={selectedIds.has(row.id) ? "selected" : "default"}
            >
              <Table.CheckCell
                checked={selectedIds.has(row.id)}
                onCheckedChange={() => toggleRow(row.id)}
                aria-label={`Select ${row.subject}`}
              />
              <Table.Cell>
                <div className="flex items-center gap-2">
                  <EnvelopeSimple size={16} />
                  <span className="truncate">{row.subject}</span>
                  {row.tags && (
                    <div className="ml-2 inline-flex gap-1">
                      {row.tags.map((tag) => (
                        <Badge key={tag}>{tag}</Badge>
                      ))}
                    </div>
                  )}
                </div>
              </Table.Cell>
              <Table.Cell>
                <span className="truncate">{row.from}</span>
              </Table.Cell>
              <Table.Cell>
                <span className="truncate">{row.date}</span>
              </Table.Cell>
              <Table.Cell className="text-right">
                <DropdownMenu>
                  <DropdownMenu.Trigger
                    render={
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label="More options"
                      >
                        <DotsThree weight="bold" size={16} />
                      </Button>
                    }
                  />
                  <DropdownMenu.Content>
                    <DropdownMenu.Item icon={Eye}>View</DropdownMenu.Item>
                    <DropdownMenu.Item icon={PencilSimple}>
                      Edit
                    </DropdownMenu.Item>
                    <DropdownMenu.Separator />
                    <DropdownMenu.Item icon={Trash} variant="danger">
                      Delete
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </LayerCard>
```


## TableOfContents

- Type: component
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: TableOfContents - presentational compound component for section navigation.  Purely visual; all interaction logic (scroll tracking, active state management) is left to the consumer.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |
| `id` | string | No | - | - | - |
| `lang` | string | No | - | - | - |
| `title` | string | No | - | - | - |

**Subcomponents**

### TableOfContents.Title

- Description: Title sub-component

Props:

- None

### TableOfContents.List

- Description: List sub-component

Props:

- None

### TableOfContents.Item

- Description: Item sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `active` | boolean | No | - | - | - |

### TableOfContents.Group

- Description: Group sub-component

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `label` | string | Yes | - | - | - |
| `href` | string | No | - | - | - |
| `active` | boolean | No | - | - | - |

**Kumo Color Tokens**

`border-kumo-brand`, `border-kumo-hairline`, `border-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Examples**

Example 1:

```tsx
<DemoWrapper>
      <TableOfContents>
        <TableOfContents.Title>On this page</TableOfContents.Title>
        <TableOfContents.List>
          {headings.map((heading) => (
            <TableOfContents.Item
              key={heading.text}
              active={heading.text === "Usage"}
              className="cursor-pointer"
            >
              {heading.text}
            </TableOfContents.Item>
          ))}
        </TableOfContents.List>
      </TableOfContents>
    </DemoWrapper>
```

Example 2:

```tsx
<DemoWrapper>
      <TableOfContents>
        <TableOfContents.Title>On this page</TableOfContents.Title>
        <TableOfContents.List>
          {headings.map((heading) => (
            <TableOfContents.Item
              key={heading.text}
              active={heading.text === active}
              onClick={() => setActive(heading.text)}
              className="cursor-pointer"
            >
              {heading.text}
            </TableOfContents.Item>
          ))}
        </TableOfContents.List>
      </TableOfContents>
    </DemoWrapper>
```

Example 3:

```tsx
<DemoWrapper>
      <TableOfContents>
        <TableOfContents.Title>On this page</TableOfContents.Title>
        <TableOfContents.List>
          {headings.map((heading) => (
            <TableOfContents.Item key={heading.text} className="cursor-pointer">
              {heading.text}
            </TableOfContents.Item>
          ))}
        </TableOfContents.List>
      </TableOfContents>
    </DemoWrapper>
```

Example 4:

```tsx
<DemoWrapper>
      <TableOfContents>
        <TableOfContents.Title>On this page</TableOfContents.Title>
        <TableOfContents.List>
          <TableOfContents.Item active className="cursor-pointer">
            Overview
          </TableOfContents.Item>
          <TableOfContents.Group label="Examples" href="#examples-demo">
            <TableOfContents.Item className="cursor-pointer">
              Basic example
            </TableOfContents.Item>
            <TableOfContents.Item className="cursor-pointer">
              Advanced example
            </TableOfContents.Item>
          </TableOfContents.Group>
          <TableOfContents.Group label="Getting Started">
            <TableOfContents.Item className="cursor-pointer">
              Installation
            </TableOfContents.Item>
            <TableOfContents.Item className="cursor-pointer">
              Configuration
            </TableOfContents.Item>
          </TableOfContents.Group>
          <TableOfContents.Group label="API" href="#api-demo">
            <TableOfContents.Item className="cursor-pointer">
              Props
            </TableOfContents.Item>
            <TableOfContents.Item className="cursor-pointer">
              Events
            </TableOfContents.Item>
          </TableOfContents.Group>
        </TableOfContents.List>
      </TableOfContents>
    </DemoWrapper>
```

Example 5:

```tsx
<DemoWrapper>
      <TableOfContents>
        <TableOfContents.List>
          {headings.slice(0, 3).map((heading) => (
            <TableOfContents.Item
              key={heading.text}
              active={heading.text === "Introduction"}
              className="cursor-pointer"
            >
              {heading.text}
            </TableOfContents.Item>
          ))}
        </TableOfContents.List>
      </TableOfContents>
    </DemoWrapper>
```

Example 6:

```tsx
<DemoWrapper>
      <div className="space-y-3">
        <TableOfContents>
          <TableOfContents.List>
            {["Introduction", "Installation", "Usage"].map((text) => (
              <TableOfContents.Item
                key={text}
                render={<button type="button" />}
                onClick={() => setClicked(text)}
                active={text === "Introduction"}
              >
                {text}
              </TableOfContents.Item>
            ))}
          </TableOfContents.List>
        </TableOfContents>
        {clicked && (
          <p className="text-xs text-kumo-subtle">Clicked: {clicked}</p>
        )}
      </div>
    </DemoWrapper>
```


## Tabs

- Type: component
- Category: Navigation
- Import path: `@cloudflare/kumo`
- Description: Tab navigation component with segmented or underline style. Built on Base UI Tabs with animated active indicator.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `tabs` | TabsItem[] | No | - | - | Array of tab items to render. |
| `value` | string | No | - | - | Controlled value. When set, component becomes controlled. |
| `selectedValue` | string | No | - | - | Default selected value for uncontrolled mode. Ignored when `value` is set. |
| `activateOnFocus` | boolean | No | - | - | When `true`, tabs are activated immediately upon receiving focus via arrow keys. When `false` (default), tabs receive focus but require Enter/Space to activate. |
| `className` | string | No | - | - | Additional CSS classes for the root element. |
| `listClassName` | string | No | - | - | Additional CSS classes for the tab list element. |
| `indicatorClassName` | string | No | - | - | Additional CSS classes for the indicator element. |
| `variant` | enum | No | segmented | `segmented`, `underline` | Tab style. - `"segmented"` - Pill-shaped indicator on a filled track - `"underline"` - Underline indicator below tab text |
| `size` | enum | No | base | `base`, `sm` | Tab size. - `"base"` - Default size (h-9, text-base) - `"sm"` - Compact size (h-6.5, text-xs) - matches Input size="sm" |
| `onValueChange` | (value: string) => void | No | - | - | Callback when active tab changes |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-brand`, `bg-kumo-recessed`, `bg-kumo-tint`, `border-kumo-hairline`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Styling**

```json
{
  "container": {
    "height": 34,
    "borderRadius": 8,
    "background": "color-accent",
    "padding": 1
  },
  "tab": {
    "paddingX": 10,
    "verticalMargin": 1,
    "fontSize": 16,
    "fontWeight": 500,
    "borderRadius": 8,
    "activeColor": "text-color-surface",
    "inactiveColor": "text-color-label"
  },
  "indicator": {
    "background": "color-surface-secondary",
    "ring": "color-color-2",
    "borderRadius": 6,
    "shadow": "shadow-sm"
  }
}
```


**Examples**

Example 1:

```tsx
<div className="flex flex-col gap-6">
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Segmented (default)</p>
        <Tabs
          variant="segmented"
          tabs={[
            { value: "tab1", label: "Tab 1" },
            { value: "tab2", label: "Tab 2" },
            { value: "tab3", label: "Tab 3" },
          ]}
          selectedValue="tab1"
        />
      </div>
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Underline</p>
        <Tabs
          variant="underline"
          tabs={[
            { value: "tab1", label: "Tab 1" },
            { value: "tab2", label: "Tab 2" },
            { value: "tab3", label: "Tab 3" },
          ]}
          selectedValue="tab1"
        />
      </div>
    </div>
```

Example 2:

```tsx
<Tabs
      variant="segmented"
      tabs={[
        { value: "tab1", label: "Tab 1" },
        { value: "tab2", label: "Tab 2" },
        { value: "tab3", label: "Tab 3" },
      ]}
      selectedValue="tab1"
    />
```

Example 3:

```tsx
<Tabs
      variant="underline"
      tabs={[
        { value: "tab1", label: "Tab 1" },
        { value: "tab2", label: "Tab 2" },
        { value: "tab3", label: "Tab 3" },
      ]}
      selectedValue="tab1"
    />
```

Example 4:

```tsx
<div className="space-y-4">
      <Tabs
        tabs={[
          { value: "tab1", label: "Tab 1" },
          { value: "tab2", label: "Tab 2" },
          { value: "tab3", label: "Tab 3" },
        ]}
        value={activeTab}
        onValueChange={setActiveTab}
      />
      <p className="text-sm text-kumo-subtle">
        Active tab: <code className="text-sm">{activeTab}</code>
      </p>
    </div>
```

Example 5:

```tsx
<div className="w-full max-w-md">
      <Tabs
        tabs={[
          { value: "overview", label: "Overview" },
          { value: "analytics", label: "Analytics" },
          { value: "reports", label: "Reports" },
          { value: "notifications", label: "Notifications" },
          { value: "settings", label: "Settings" },
          { value: "billing", label: "Billing" },
          { value: "security", label: "Security" },
          { value: "integrations", label: "Integrations" },
        ]}
        selectedValue="overview"
      />
    </div>
```

Example 6:

```tsx
<div className="flex flex-col gap-6">
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Segmented sm</p>
        <Tabs
          variant="segmented"
          size="sm"
          tabs={[
            { value: "tab1", label: "Tab 1" },
            { value: "tab2", label: "Tab 2" },
            { value: "tab3", label: "Tab 3" },
          ]}
          selectedValue="tab1"
        />
      </div>
      <div>
        <p className="mb-2 text-sm text-kumo-subtle">Underline sm</p>
        <Tabs
          variant="underline"
          size="sm"
          tabs={[
            { value: "tab1", label: "Tab 1" },
            { value: "tab2", label: "Tab 2" },
            { value: "tab3", label: "Tab 3" },
          ]}
          selectedValue="tab1"
        />
      </div>
    </div>
```

Example 7:

```tsx
<Tabs
      tabs={[
        {
          value: "tab1",
          label: "Regular Tab",
        },
        {
          value: "tab2",
          label: "Link Tab",
          render: (props) => <a {...props} href="#tab2" />,
        },
        {
          value: "tab3",
          label: "Cloudflare",
          render: (props) => (
            <a {...props} href="https://cloudflare.com" target="_blank" />
          ),
        },
      ]}
      selectedValue="tab1"
    />
```


## Text

- Type: component
- Category: Display
- Import path: `@cloudflare/kumo`
- Description: Text component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | body | `heading1`, `heading2`, `heading3`, `body`, `secondary`, `success`, `error`, `mono`, `mono-secondary` | Text style variant. Determines color, font, and weight. - `"heading1"` - Large page title (30px, semibold) - `"heading2"` - Section title (24px, semibold) - `"heading3"` - Subsection title (18px, semibold) - `"body"` - Default body text - `"secondary"` - Muted text for secondary information - `"success"` - Success state text - `"error"` - Error state text - `"mono"` - Monospace text for code - `"mono-secondary"` - Muted monospace text |
| `size` | enum | No | base | `xs`, `sm`, `base`, `lg` | Text size (only applies to body/secondary/success/error variants). - `"xs"` - 12px - `"sm"` - 14px - `"base"` - 16px - `"lg"` - 18px |
| `bold` | boolean | No | - | - | Whether to use bold font weight (only applies to body variants). |
| `truncate` | boolean | No | - | - | Whether to truncate overflowing text with an ellipsis. Adds `truncate min-w-0` classes. |
| `as` | enum | No | - | `h1`, `h2`, `h3`, `h4`, `h5`, `h6`, `p`, `span`, `label`, `dt`, `dd`, `li`, `figcaption`, `legend`, `pre`, `code`, `em`, `strong`, `small`, `abbr`, `time` | The HTML element to render. Accepts headings (`"h1"`-`"h6"`), block text (`"p"`, `"pre"`), inline text (`"span"`, `"code"`, `"em"`, `"strong"`, `"small"`, `"abbr"`, `"time"`), form-related (`"label"`, `"legend"`), list/definition (`"dt"`, `"dd"`, `"li"`), and `"figcaption"`.  - **Required** for heading variants (`"heading1"`, `"heading2"`,   `"heading3"`) - pick the element that reflects this text's place in   the document outline, or `"span"` for decorative heading-styled text   that is not a section heading. - **Optional** for body variants (defaults to `"p"`) and monospace   variants (defaults to `"span"`). |
| `children` | ReactNode | No | - | - | Text content. |

**Kumo Color Tokens**

`text-kumo-danger`, `text-kumo-default`, `text-kumo-link`, `text-kumo-subtle`

**Styling**

```json
{
  "fontSizes": {
    "xs": 12,
    "sm": 14,
    "base": 16,
    "lg": 18,
    "xl": 20,
    "2xl": 24,
    "3xl": 30
  },
  "fontWeights": {
    "normal": 400,
    "medium": 500,
    "semibold": 600
  },
  "baseColor": "text-kumo-default",
  "variantColors": {
    "body": "text-kumo-default",
    "secondary": "text-kumo-subtle",
    "success": "text-kumo-link",
    "error": "text-kumo-danger",
    "mono": "text-kumo-default",
    "mono-secondary": "text-kumo-subtle"
  },
  "fontFamilies": {
    "default": "sans-serif",
    "mono": "monospace"
  }
}
```


**Examples**

Example 1:

```tsx
<div className="grid w-full grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="heading1" as="h1">
          Heading 1
        </Text>
        <Text variant="mono-secondary">text-3xl (30px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="heading2" as="h2">
          Heading 2
        </Text>
        <Text variant="mono-secondary">text-2xl (24px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="heading3" as="h3">
          Heading 3
        </Text>
        <Text variant="mono-secondary">text-lg (16px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text>Body</Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text bold>Body bold</Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text size="lg">Body lg</Text>
        <Text variant="mono-secondary">text-lg (16px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text size="sm">Body sm</Text>
        <Text variant="mono-secondary">text-sm (13px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text size="xs">Body xs</Text>
        <Text variant="mono-secondary">text-xs (12px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="secondary">Body secondary</Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="mono">Monospace</Text>
        <Text variant="mono-secondary">text-sm (13px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="mono" size="lg">
          Monospace lg
        </Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="mono-secondary">Monospace secondary</Text>
        <Text variant="mono-secondary">text-sm (13px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="success">Success</Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="error">Error</Text>
        <Text variant="mono-secondary">text-base (14px)</Text>
      </div>
    </div>
```

Example 2:

```tsx
<div className="w-64 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
      <Text truncate>
        This is a long piece of text that will be truncated with an ellipsis
        when it overflows its container.
      </Text>
    </div>
```


## Toasty

- Type: component
- Category: Feedback
- Import path: `@cloudflare/kumo`
- Description: Toasty - toast notification provider and viewport.  Renders a `Toast.Provider` with a fixed-position viewport in the bottom-right corner. Toasts stack with smooth enter/exit animations, swipe-to-dismiss, and expand-on-hover.  Built on `@base-ui/react/toast`.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `variant` | enum | No | default | `default`, `success`, `error`, `warning`, `info` | `{"default":"Default toast style","success":"Success toast for confirmations and positive outcomes","error":"Error toast for critical issues","warning":"Warning toast for cautionary messages","info":"Info toast for neutral informational messages"}` |
| `className` | string | No | - | - | Additional CSS classes |
| `children` | ReactNode | No | - | - | Child elements |

**Kumo Color Tokens**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-control`, `bg-kumo-danger-tint`, `bg-kumo-fill-hover`, `bg-kumo-info-tint`, `bg-kumo-success-tint`, `bg-kumo-warning-tint`, `border-kumo-fill`, `ring-kumo-danger`, `ring-kumo-hairline`, `ring-kumo-info`, `ring-kumo-line`, `ring-kumo-success`, `ring-kumo-warning`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-info`, `text-kumo-subtle`, `text-kumo-success`, `text-kumo-warning`

**Styling**

```json
{
  "container": {
    "width": 300,
    "padding": 16,
    "borderRadius": 8,
    "background": "bg-kumo-base",
    "border": "ring-[0.3px] ring-kumo-hairline",
    "shadow": "shadow-lg",
    "gap": 4
  },
  "title": {
    "fontSize": 16,
    "fontWeight": 500,
    "color": "text-color-surface"
  },
  "description": {
    "fontSize": 15,
    "fontWeight": 400,
    "color": "text-color-muted"
  },
  "closeButton": {
    "size": 20,
    "iconSize": 16,
    "iconName": "ph-x",
    "iconColor": "text-color-muted",
    "hoverBackground": "color-color-2",
    "hoverColor": "text-color-label",
    "borderRadius": 4
  }
}
```


**Examples**

Example 1:

```tsx
<Toasty>
      <ToastTriggerButton />
    </Toasty>
```

Example 2:

```tsx
<Toasty>
      <ToastTitleOnlyButton />
    </Toasty>
```

Example 3:

```tsx
<Toasty>
      <ToastDescriptionOnlyButton />
    </Toasty>
```

Example 4:

```tsx
<Toasty>
      <ToastSuccessButton />
    </Toasty>
```

Example 5:

```tsx
<Toasty>
      <ToastMultipleButton />
    </Toasty>
```

Example 6:

```tsx
<Toasty>
      <ToastErrorButton />
    </Toasty>
```

Example 7:

```tsx
<Toasty>
      <ToastWarningButton />
    </Toasty>
```

Example 8:

```tsx
<Toasty>
      <ToastInfoButton />
    </Toasty>
```

Example 9:

```tsx
<Toasty>
      <ToastCustomContentButton />
    </Toasty>
```

Example 10:

```tsx
<Toasty>
      <ToastActionsButton />
    </Toasty>
```

Example 11:

```tsx
<Toasty>
      <ToastPromiseButton />
    </Toasty>
```


## Tooltip

- Type: component
- Category: Overlay
- Import path: `@cloudflare/kumo`
- Description: Accessible popup that shows additional information on hover/focus. Wrap your app or section with `<TooltipProvider>` to enable delay grouping.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `side` | enum | No | top | `top`, `bottom`, `left`, `right` | `{"top":"Tooltip appears above the trigger","bottom":"Tooltip appears below the trigger","left":"Tooltip appears to the left of the trigger","right":"Tooltip appears to the right of the trigger"}` |
| `className` | string | No | - | - | Additional CSS classes |
| `children` | ReactNode | No | - | - | Child elements |
| `content` | ReactNode | Yes | - | - | Content to display in the tooltip |

**Subcomponents**

### Tooltip.Provider

- Description: Groups multiple tooltips so that after the first tooltip is shown, switching to another skips the open delay. Place once at your app root or layout.
- Pass through: Yes, base: `TooltipBase.Provider`

Props:

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `delay` | number | No | 600 | - | How long to wait (ms) before opening a tooltip once the pointer enters the trigger. |
| `closeDelay` | number | No | 0 | - | How long to wait (ms) before closing a tooltip. |
| `timeout` | number | No | 400 | - | Grace period (ms) during which a just-closed tooltip's delay is skipped when another tooltip opens. |

Usage examples:

Example 1:

```tsx
<TooltipProvider>
  <App />
</TooltipProvider>
```

**Kumo Color Tokens**

`bg-kumo-base`, `fill-kumo-base`, `fill-kumo-tip-shadow`, `fill-kumo-tip-stroke`, `outline-kumo-fill`, `text-kumo-default`

**Examples**

Example 1:

```tsx
<TooltipProvider>
      <Tooltip
        content="Add new item"
        render={
          <Button shape="square" icon={PlusIcon} aria-label="Add new item" />
        }
      />
    </TooltipProvider>
```

Example 2:

```tsx
<TooltipProvider>
      <Tooltip
        content="Add"
        render={<Button shape="square" icon={PlusIcon} aria-label="Add" />}
      />
    </TooltipProvider>
```

Example 3:

```tsx
<TooltipProvider>
      <div className="flex gap-2">
        <Tooltip
          content="Add"
          render={<Button shape="square" icon={PlusIcon} aria-label="Add" />}
        />
        <Tooltip
          content="Change language"
          render={
            <Button
              shape="square"
              icon={TranslateIcon}
              aria-label="Change language"
            />
          }
        />
      </div>
    </TooltipProvider>
```

Example 4:

```tsx
<TooltipProvider>
      <Tooltip
        content="Click to learn more"
        className="inline-flex items-center gap-1.5 rounded-full bg-kumo-brand px-3 py-1.5 text-sm font-medium text-white shadow-md transition-transform hover:scale-105 active:scale-95"
      >
        <Info className="size-4" />
        <span>Help</span>
      </Tooltip>
    </TooltipProvider>
```

Example 5:

```tsx
<TooltipProvider>
      <div className="flex w-full justify-between">
        <Tooltip
          content={longContent}
          side="bottom"
          render={<Button variant="secondary" />}
        >
          Near left edge
        </Tooltip>
        <Tooltip
          content={longContent}
          side="bottom"
          render={<Button variant="secondary" />}
        >
          Centered
        </Tooltip>
        <Tooltip
          content={longContent}
          side="bottom"
          render={<Button variant="secondary" />}
        >
          Near right edge
        </Tooltip>
      </div>
    </TooltipProvider>
```

Example 6:

```tsx
<TooltipProvider>
      <div className="flex gap-4">
        <Tooltip
          content="Opens after 1 second"
          delay={1000}
          render={<Button variant="secondary" />}
        >
          1s open delay
        </Tooltip>
        <Tooltip
          content="Stays open 500ms after leaving"
          closeDelay={500}
          render={<Button variant="secondary" />}
        >
          500ms close delay
        </Tooltip>
        <Tooltip
          content="Instant open, stays 1s"
          delay={0}
          closeDelay={1000}
          render={<Button variant="secondary" />}
        >
          Instant + 1s close
        </Tooltip>
      </div>
    </TooltipProvider>
```


## Blocks

## DeleteResource

- Type: block
- Category: Other
- Import path: `@cloudflare/kumo`
- Description: DeleteResource component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `size` | enum | No | base | `sm`, `base` | `{"sm":"Small dialog for simple delete confirmations","base":"Default delete confirmation dialog size"}` |
| `open` | boolean | Yes | - | - | Whether the dialog is open |
| `resourceType` | string | Yes | - | - | The type of resource being deleted (e.g., "Zone", "Worker", "KV Namespace") |
| `resourceName` | string | Yes | - | - | The name of the specific resource being deleted |
| `isDeleting` | boolean | No | - | - | Whether the delete action is in progress |
| `caseSensitive` | boolean | No | - | - | Whether the confirmation input should be case-sensitive (default: true) |
| `deleteButtonText` | string | No | - | - | Custom delete button text (defaults to "Delete {resourceType}") |
| `className` | string | No | - | - | Additional className for the dialog |
| `errorMessage` | string | No | - | - | Error message to display if the delete action fails |

**Kumo Color Tokens**

`bg-kumo-fill`, `bg-kumo-tint`, `border-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Block Files**

- `delete-resource/delete-resource.tsx`

**Dependencies**

`Banner`, `Button`, `Dialog`, `DialogClose`, `DialogRoot`, `DialogTitle`, `Input`

**Examples**

Example 1:

```tsx
<>
      <Button variant="destructive" onClick={() => setOpen(true)}>
        Delete Zone
      </Button>
      <DeleteResource
        open={open}
        onOpenChange={setOpen}
        resourceType="Zone"
        resourceName="example.com"
        onDelete={handleDelete}
        isDeleting={isDeleting}
      />
    </>
```

Example 2:

```tsx
<>
      <Button variant="destructive" onClick={() => setOpen(true)}>
        Delete Worker
      </Button>
      <DeleteResource
        open={open}
        onOpenChange={setOpen}
        resourceType="Worker"
        resourceName="api-gateway-worker"
        onDelete={handleDelete}
        isDeleting={isDeleting}
      />
    </>
```

Example 3:

```tsx
<>
      <Button variant="destructive" onClick={() => setOpen(true)}>
        Delete Zone
      </Button>
      <DeleteResource
        open={open}
        onOpenChange={setOpen}
        resourceType="Zone"
        resourceName="example.com"
        onDelete={handleDelete}
        isDeleting={isDeleting}
        errorMessage={errorMsg}
      />
    </>
```


## PageHeader

- Type: block
- Category: Layout
- Import path: `@cloudflare/kumo`
- Description: PageHeader component

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `spacing` | enum | No | base | `compact`, `base`, `relaxed` | `{"compact":"Compact spacing between header elements","base":"Default spacing between header elements","relaxed":"Relaxed spacing for more prominent headers"}` |
| `breadcrumbs` | ReactNode | No | - | - | - |
| `title` | string | No | - | - | - |
| `description` | string | No | - | - | - |
| `tabs` | TabsItem[] | No | - | - | - |
| `defaultTab` | string | No | - | - | - |
| `className` | string | No | - | - | - |
| `children` | ReactNode | No | - | - | - |

**Kumo Color Tokens**

`border-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Block Files**

- `page-header/page-header.tsx`

**Dependencies**

`Tabs`

**Examples**

Example 1:

```tsx
<PageHeader
      className="w-full"
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link icon={<HouseIcon size={16} />} href="#">
            Workers & Pages
          </Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>cloudflare-dev-platform</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      tabs={[
        { label: "Overview", value: "overview" },
        { label: "Metrics", value: "metrics" },
        { label: "Deployments", value: "deployments" },
        { label: "Bindings", value: "bindings" },
        { label: "Observability", value: "observability" },
        { label: "Settings", value: "settings" },
      ]}
      defaultTab="overview"
      onValueChange={(v) => console.log(v)}
    >
      <Button icon={<CodeIcon />} className="h-8">
        Edit code
      </Button>
      <Button icon={<GlobeIcon />} variant="primary" className="h-8">
        Visit
      </Button>
    </PageHeader>
```

Example 2:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>Dashboard</Breadcrumbs.Current>
        </Breadcrumbs>
      }
    />
```

Example 3:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link icon={<HouseIcon size={16} />} href="#">
            Home
          </Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current icon={<GearIcon size={16} />}>
            Settings
          </Breadcrumbs.Current>
        </Breadcrumbs>
      }
    />
```

Example 4:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>Settings</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      tabs={[
        { label: "General", value: "general" },
        { label: "Security", value: "security" },
        { label: "Notifications", value: "notifications" },
      ]}
      defaultTab="general"
    />
```

Example 5:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Link href="#">Projects</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>My Project</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      tabs={[
        { label: "Overview", value: "overview" },
        { label: "Settings", value: "settings" },
      ]}
      defaultTab="overview"
    >
      <Button variant="primary" size="base">
        Deploy
      </Button>
    </PageHeader>
```

Example 6:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Link href="#">Products</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>Page title</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      title="Page title"
    />
```

Example 7:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Link href="#">Products</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>Page title</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      title="Page title"
      description="Action-led, value-oriented description of what this page does. Optional second sentence with use cases or prerequisites."
    />
```

Example 8:

```tsx
<PageHeader
      breadcrumbs={
        <Breadcrumbs>
          <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Link href="#">Products</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>Page title</Breadcrumbs.Current>
        </Breadcrumbs>
      }
      title="Page title"
      description="Action-led, value-oriented description of what this page does."
      tabs={[
        { label: "Overview", value: "overview" },
        { label: "Analytics", value: "analytics" },
        { label: "Settings", value: "settings" },
      ]}
      defaultTab="overview"
    >
      <Button variant="outline" size="sm">
        Export
      </Button>
      <Button variant="primary" size="sm" icon={<PlusIcon size={16} />}>
        New Item
      </Button>
    </PageHeader>
```


## ResourceListPage

- Type: block
- Category: Layout
- Import path: `@cloudflare/kumo`
- Description: ResourceListPage - A layout component for resource list pages  Layouts are page-level components that provide consistent structure for common page patterns like resource lists, dashboards, and settings.

**Props**

| Prop | Type | Required | Default | Values | Description |
| --- | --- | --- | --- | --- | --- |
| `title` | string | No | - | - | - |
| `description` | string | No | - | - | - |
| `icon` | ReactNode | No | - | - | - |
| `usage` | ReactNode | No | - | - | - |
| `additionalContent` | ReactNode | No | - | - | - |
| `children` | ReactNode | No | - | - | - |
| `className` | string | No | - | - | - |

**Kumo Color Tokens**

`bg-kumo-overlay`, `text-kumo-subtle`

**Block Files**

- `resource-list/resource-list.tsx`
- `resource-list/resource-list.test.tsx`

**Examples**

Example 1:

```tsx
<ResourceListPage
      title="Databases"
      description="Manage your database instances and configurations"
      icon={<DatabaseIcon size={32} className="text-kumo-subtle" />}
    >
      <Surface className="p-6">
        <p>Main content area - your resource list would go here</p>
      </Surface>
    </ResourceListPage>
```

Example 2:

```tsx
<ResourceListPage
      title="API Keys"
      description="Create and manage API keys for your applications"
      usage={
        <Surface className="p-4">
          <h3 className="mb-2 font-semibold">Quick Start</h3>
          <p className="mb-3 text-sm text-kumo-subtle">
            Generate an API key to authenticate your requests
          </p>
          <Code
            lang="bash"
            code='curl -H "Authorization: Bearer <API_KEY>" https://api.example.com'
          />
        </Surface>
      }
    >
      <Surface className="p-6">
        <p>API keys list would appear here</p>
      </Surface>
    </ResourceListPage>
```

Example 3:

```tsx
<ResourceListPage
      title="KV Namespaces"
      description="Store key-value data globally with low-latency access"
      icon={<DatabaseIcon size={32} className="text-kumo-subtle" />}
      usage={
        <Surface className="p-4">
          <h3 className="mb-2 font-semibold">Usage Example</h3>
          <Code
            lang="ts"
            code={`// Read from KV
const value = await KV.get('key');

// Write to KV
await KV.put('key', 'value');`}
          />
        </Surface>
      }
      additionalContent={
        <Surface className="p-4">
          <h3 className="mb-2 font-semibold">Learn More</h3>
          <p className="text-sm text-kumo-subtle">
            Check out our documentation to learn more about KV storage.
          </p>
        </Surface>
      }
    >
      <div className="space-y-4">
        <Surface className="p-6">
          <h4 className="mb-2 font-semibold">production-kv</h4>
          <p className="text-sm text-kumo-subtle">Created 2 days ago</p>
        </Surface>
        <Surface className="p-6">
          <h4 className="mb-2 font-semibold">staging-kv</h4>
          <p className="text-sm text-kumo-subtle">Created 1 week ago</p>
        </Surface>
      </div>
    </ResourceListPage>
```


## Search Metadata

### By Type

- component: `Autocomplete`, `Badge`, `Banner`, `Breadcrumbs`, `Button`, `Checkbox`, `ClipboardText`, `CloudflareLogo`, `Code`, `Collapsible`, `Combobox`, `CommandPalette`, `DatePicker`, `DateRangePicker`, `Dialog`, `DropdownMenu`, `Empty`, `Field`, `Grid`, `Input`, `InputGroup`, `Label`, `LayerCard`, `Link`, `Loader`, `MenuBar`, `Meter`, `Pagination`, `Popover`, `Radio`, `Select`, `SensitiveInput`, `Sidebar`, `Surface`, `Switch`, `Table`, `TableOfContents`, `Tabs`, `Text`, `Toasty`, `Tooltip`
- block: `DeleteResource`, `PageHeader`, `ResourceListPage`

### By Category Raw

```json
{
  "Other": [
    "Autocomplete",
    "CloudflareLogo",
    "DatePicker",
    "Label",
    "Link",
    "SensitiveInput",
    "Sidebar",
    "Table",
    "TableOfContents",
    "DeleteResource"
  ],
  "Display": [
    "Badge",
    "Breadcrumbs",
    "Code",
    "Collapsible",
    "Empty",
    "LayerCard",
    "Meter",
    "Text"
  ],
  "Feedback": [
    "Banner",
    "Loader",
    "Toasty"
  ],
  "Action": [
    "Button",
    "ClipboardText"
  ],
  "Input": [
    "Checkbox",
    "Combobox",
    "DateRangePicker",
    "Field",
    "Input",
    "InputGroup",
    "Radio",
    "Select",
    "Switch"
  ],
  "Navigation": [
    "CommandPalette",
    "MenuBar",
    "Pagination",
    "Tabs"
  ],
  "Overlay": [
    "Dialog",
    "DropdownMenu",
    "Popover",
    "Tooltip"
  ],
  "Layout": [
    "Grid",
    "Surface",
    "PageHeader",
    "ResourceListPage"
  ]
}
```
