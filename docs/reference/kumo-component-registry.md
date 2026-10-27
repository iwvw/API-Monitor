# Kumo Component Registry

> Auto-generated component metadata for AI/agent consumption.

---

### Autocomplete

Autocomplete — free-form text input with an optional filtered suggestion list.

**Type:** component

**Import:** `import { Autocomplete } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `items`: unknown[] (required)
  Array of items to display in the dropdown
- `value`: string | number | string[]
  The controlled input value
- `open`: boolean
  Whether the popup is open (controlled)
- `children`: ReactNode
  Autocomplete content (input group, popup content)
- `className`: string
  Additional CSS classes
- `label`: ReactNode
  Label content (enables Field wrapper)
- `required`: boolean
  Whether the field is required
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label
- `description`: ReactNode
  Helper text displayed below the field
- `error`: string | object
  Error message or validation error object

**Colors (kumo tokens used):**

`bg-kumo-control`, `bg-kumo-line`, `bg-kumo-tint`, `border-kumo-line`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-strong`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Autocomplete.InputGroup

InputGroup sub-component

Props:
- `className`: string
- `size`: KumoAutocompleteSize
- `placeholder`: string

#### Autocomplete.Content

Content sub-component

Props:
- `children`: ReactNode
- `className`: string
- `align`: AutocompleteBase.Positioner.Props["align"]
- `alignOffset`: AutocompleteBase.Positioner.Props["alignOffset"]
- `side`: AutocompleteBase.Positioner.Props["side"]
- `sideOffset`: AutocompleteBase.Positioner.Props["sideOffset"]

#### Autocomplete.Item

Item sub-component

#### Autocomplete.GroupLabel

GroupLabel sub-component

#### Autocomplete.Group

Group sub-component

#### Autocomplete.Separator

Separator sub-component

#### Autocomplete.List

List sub-component

#### Autocomplete.Empty

Empty sub-component (wraps AutocompleteBase)

#### Autocomplete.Collection

Collection sub-component (wraps AutocompleteBase)


**Examples:**

```tsx
<Autocomplete items={fruits}>
      <Autocomplete.InputGroup placeholder="Search fruits…" />
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

```tsx
<div className="w-80">
      <Autocomplete
        items={languages}
        label="Language"
        description="Start typing to filter languages"
        filter={filter}
      >
        <Autocomplete.InputGroup placeholder="Search a language…" />
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

```tsx
<div className="w-80">
      <Autocomplete
        items={countries}
        label="Country"
        error={{ message: "Please enter a valid country", match: true }}
        filter={filter}
      >
        <Autocomplete.InputGroup placeholder="Search countries…" />
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

```tsx
<Autocomplete items={servers}>
      <Autocomplete.InputGroup placeholder="Select region…" />
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

```tsx
<div className="flex w-80 flex-col gap-3">
      <Autocomplete
        items={fruits}
        value={value}
        onValueChange={(v) => setValue(v)}
      >
        <Autocomplete.InputGroup placeholder="Type a fruit…" />
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
          Value: <span className="font-medium text-kumo-default">{value}</span>
        </p>
      )}
    </div>
```


---

### Badge

Small status label for categorizing or highlighting content.

**Type:** component

**Import:** `import { Badge } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `variant`: enum [default: primary]
  - `"primary"`: Primary badge
  - `"secondary"`: Secondary badge
  - `"error"`: Error badge
  - `"warning"`: Warning badge
  - `"success"`: Success badge
  - `"destructive"`: Deprecated. Use red instead.
  - `"info"`: Info badge
  - `"beta"`: Indicates beta or experimental features
  - `"outline"`: Bordered badge with base background
  - `"red"`: Red badge
  - `"green"`: Green badge
  - `"neutral"`: Neutral badge
  - `"orange"`: Orange badge
  - `"purple"`: Purple badge
  - `"teal"`: Teal badge
  - `"teal-subtle"`: Subtle teal badge
  - `"blue"`: Blue badge
- `className`: string
  Additional CSS classes merged via `cn()`.
- `children`: ReactNode
  Content rendered inside the badge.
- `appearance`: enum [default: filled]
  - `"filled"`: Filled badge with background color (default)
  - `"dot"`: Outlined badge with a colored circle dot indicating status
- `icon`: ReactNode
  Icon from `@phosphor-icons/react` or a React element. Rendered before children.

**Colors (kumo tokens used):**

`bg-kumo-badge-blue`, `bg-kumo-badge-green`, `bg-kumo-badge-inverted`, `bg-kumo-badge-neutral`, `bg-kumo-badge-orange`, `bg-kumo-badge-purple`, `bg-kumo-badge-red`, `bg-kumo-badge-teal`, `bg-kumo-badge-teal-subtle`, `bg-kumo-base`, `bg-kumo-danger-tint`, `bg-kumo-fill`, `bg-kumo-info-tint`, `bg-kumo-success`, `bg-kumo-success-tint`, `bg-kumo-warning-tint`, `border-kumo-brand`, `border-kumo-fill`, `ring-kumo-hairline`, `text-kumo-badge-inverted`, `text-kumo-badge-neutral-subtle`, `text-kumo-badge-teal-subtle`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-info`, `text-kumo-link`, `text-kumo-success`, `text-kumo-warning`

**Examples:**

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

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Badge variant="neutral">Neutral</Badge>
      <Badge variant="red">Red</Badge>
      <Badge variant="green">Green</Badge>
      <Badge variant="orange">Orange</Badge>
      <Badge variant="teal">Teal</Badge>
      <Badge variant="blue">Blue</Badge>
      <Badge variant="purple">Purple</Badge>
    </div>
```

```tsx
<p className="flex items-center gap-2">
      Workers
      <Badge variant="secondary">New</Badge>
    </p>
```

```tsx
<Badge icon={CheckCircleIcon} variant="success">
      Verified
    </Badge>
```

```tsx
<Link href="/changelog" variant="plain">
      <Badge variant="outline">View changelog</Badge>
    </Link>
```

```tsx
<div className="flex flex-wrap items-center gap-2">
      <Badge variant="success" appearance="dot">
        Healthy
      </Badge>
      <Badge variant="warning" appearance="dot">
        Warning
      </Badge>
      <Badge variant="error" appearance="dot">
        Error
      </Badge>
      <Badge variant="neutral" appearance="dot">
        Neutral
      </Badge>
    </div>
```


---

### Banner

Full-width message bar with an optional trailing CTA slot.

**Type:** component

**Import:** `import { Banner } from "@cloudflare/kumo";`

**Category:** Feedback

**Props:**

- `className`: string
  Additional CSS classes merged via `cn()`.
- `id`: string
- `lang`: string
- `icon`: ReactNode
  Icon element rendered before the banner content (e.g. from `@phosphor-icons/react`).
- `title`: string
  Primary heading text for the banner. Use for i18n string injection.
- `description`: ReactNode
  Secondary description text displayed below the title. Use for i18n string injection.
- `action`: ReactNode
  Action slot for a CTA button or link. Compact banners render a Kumo `Link` inline with the description; CTAs render at the trailing end. Use `Banner.Action` for accent-aware CTAs that self-style to the banner variant; other nodes are rendered as-is. Multiple actions can be passed in a Fragment. Only used in structured mode (with `title` or `description`).
- `text`: string
- `children`: ReactNode
- `variant`: enum [default: default]
  - `"default"`: Informational banner for general messages
  - `"alert"`: Warning banner for cautionary messages
  - `"error"`: Error banner for critical issues
  - `"secondary"`: Neutral banner for secondary messages
- `size`: enum [default: base]
  - `"base"`: Default banner size
  - `"sm"`: Compact banner for dialogs and tight spaces

**Colors (kumo tokens used):**

`bg-kumo-contrast`, `bg-kumo-danger-tint`, `bg-kumo-info-tint`, `bg-kumo-warning-tint`, `fill-kumo-danger`, `fill-kumo-info`, `fill-kumo-interact`, `fill-kumo-warning`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-info`, `text-kumo-warning`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Banner.Action

Action sub-component


**Examples:**

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

```tsx
<Banner
      icon={<Info weight="fill" />}
      title="Update available"
      description="A new version is ready to install."
    />
```

```tsx
<Banner
      icon={<Warning weight="fill" />}
      variant="alert"
      title="Session expiring"
      description="Your session will expire in 5 minutes."
    />
```

```tsx
<Banner
      icon={<Info weight="fill" />}
      title="Your changes have been saved."
    />
```

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

```tsx
<div className="w-full space-y-3">
      <Banner
        icon={<Info weight="fill" />}
        title="Update available"
        description="A new version is ready to install."
        action={
          <>
            <Banner.Action>Update</Banner.Action>
            <Banner.Action variant="ghost" icon={<X />} aria-label="Dismiss" />
          </>
        }
      />
      <Banner
        variant="error"
        icon={<WarningCircle weight="fill" />}
        title="Save failed"
        description="We couldn't save your changes. Please try again."
        action={
          <>
            <Banner.Action>Retry</Banner.Action>
            <Banner.Action variant="ghost" icon={<X />} aria-label="Dismiss" />
          </>
        }
      />
      <Banner
        variant="alert"
        icon={<WarningCircle weight="fill" />}
        title="Save failed"
        description="We couldn't save your changes. Please try again."
        action={
          <>
            <Banner.Action>Retry</Banner.Action>
            <Banner.Action variant="ghost" icon={<X />} aria-label="Dismiss" />
          </>
        }
      />
      <Banner
        variant="secondary"
        icon={<WarningCircle weight="fill" />}
        title="Save failed"
        description="We couldn't save your changes. Please try again."
        action={
          <>
            <Banner.Action>Retry</Banner.Action>
            <Banner.Action variant="ghost" icon={<X />} aria-label="Dismiss" />
          </>
        }
      />
    </div>
```

```tsx
<div className="w-full space-y-3">
      <Banner
        icon={<Warning weight="fill" />}
        variant="error"
        title="Your account is 90 days past due."
        description="Pay now to avoid interruption."
        action={
          <>
            <Banner.Action>Pay now</Banner.Action>
            <Banner.Action variant="secondary">Go to billing</Banner.Action>
          </>
        }
      />
    </div>
```

```tsx
<Banner
      size="sm"
      description="A DNS record for puppies.cloudflare.dev already exists in this zone."
      action={<Link href="#">Manage DNS for puppies.cloudflare.dev</Link>}
    />
```

```tsx
<Banner
      size="sm"
      description="A DNS record for puppies.cloudflare.dev already exists in this zone."
      action={
        <>
          <Banner.Action>Manage DNS</Banner.Action>
          <Banner.Action variant="ghost">
            <X />
          </Banner.Action>
        </>
      }
    />
```

```tsx
<Banner
      size="sm"
      description="A DNS record for puppies.cloudflare.dev already exists in this zone."
    />
```

```tsx
<Banner icon={<Info />}>This is a simple banner using children.</Banner>
```


---

### Breadcrumbs

Breadcrumbs component

**Type:** component

**Import:** `import { Breadcrumbs } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `size`: enum [default: base]
  - `"sm"`: Compact breadcrumbs for dense UIs
  - `"base"`: Default breadcrumbs size
- `children`: ReactNode
- `className`: string
  Additional CSS classes merged via `cn()`.

**Colors (kumo tokens used):**

`text-kumo-inactive`, `text-kumo-subtle`, `text-kumo-success`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Breadcrumbs.Link

Link sub-component

Props:
- `href`: string (required)
- `icon`: React.ReactNode

#### Breadcrumbs.Current

Current sub-component

Props:
- `loading`: boolean
- `icon`: React.ReactNode

#### Breadcrumbs.Separator

Separator sub-component

#### Breadcrumbs.Clipboard

Clipboard sub-component

Props:
- `text`: string (required)


**Examples:**

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Link href="#">Docs</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current>Breadcrumbs</Breadcrumbs.Current>
    </Breadcrumbs>
```

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

```tsx
<Breadcrumbs>
      <Breadcrumbs.Current icon={<House size={16} />}>
        Worker Analytics
      </Breadcrumbs.Current>
    </Breadcrumbs>
```

```tsx
<Breadcrumbs>
      <Breadcrumbs.Link href="#">Home</Breadcrumbs.Link>
      <Breadcrumbs.Separator />
      <Breadcrumbs.Current>Breadcrumbs</Breadcrumbs.Current>
      <Breadcrumbs.Clipboard text="#" />
    </Breadcrumbs>
```


---

### BubbleMap

BubbleMap component

**Type:** component

**Import:** `import { BubbleMap } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `echarts`: typeof echarts (required)
  The ECharts core instance imported by the consumer (passed in for tree-shaking). Requires `MapChart`, `ScatterChart`, `TooltipComponent`, and a renderer registered via `echarts.use([...])`.
- `geoJson`: MapGeoJson (required)
  GeoJSON `FeatureCollection` for the land base.
- `mapName`: string
  Optional stable ECharts map registry name. Set this when the same GeoJSON is parsed into new object instances across mounts and should reuse one global ECharts registration.
- `data`: T[] (required)
  Raw data rows. Coordinates/value/name are read via the accessors below.
- `lng`: MapAccessor<T, number> (required)
  Longitude accessor (key of `T` or `(row) => number`).
- `lat`: MapAccessor<T, number> (required)
  Latitude accessor (key of `T` or `(row) => number`).
- `value`: MapAccessor<T, number> (required)
  Value accessor — drives bubble size.
- `name`: MapAccessor<T, string>
  Optional name accessor — used by the default tooltip.
- `minRadius`: number
  Smallest bubble radius in px. Default: `6`.
- `maxRadius`: number
  Largest bubble radius in px. Default: `26`.
- `bubbleSize`: (value: number) => number
  Explicit bubble radius `(value) => px`. Overrides the default `minRadius`/`maxRadius` scaling.
- `bubbleColor`: MapStyle<T, string>
  Bubble fill colour — constant or `(row) => color`. Defaults to the chart blue.
- `bubbleBorderColor`: MapStyle<T, string>
  Bubble border colour — constant or `(row) => color`. Default: `transparent`.
- `bubbleBorderWidth`: MapStyle<T, number>
  Bubble border width — constant or `(row) => px`. Default: `0`.
- `center`: [number, number]
  Map center as `[longitude, latitude]`. Defaults to auto-fit.
- `zoom`: number
  Zoom level — multiplies the auto-fit scale. Default: `1.25`.
- `roam`: boolean
  Enable drag-to-pan and scroll-to-zoom. Default: `false`.
- `projection`: MapProjection | null
  Geographic projection. Defaults to a latitude-clamped Mercator (flat 2D web-map look). Pass another d3-geo projection to override, or `null` for ECharts' raw equirectangular plotting. Use a stable reference (module-level or memoised): a new object each render rebuilds the view and resets a roamed/zoomed map.
- `showTooltip`: boolean
  Show the tooltip. Default: `true`.
- `valueFormat`: (value: number) => string
  Format the value for the default tooltip. Default: `toLocaleString()`.
- `tooltipFormatter`: (row: T) => string
  Override the tooltip content for a row. Returns an HTML string rendered by ECharts' own tooltip. USE WITH CAUTION: the return value is injected as HTML. Escape any user-provided strings to avoid XSS.
- `aspectRatio`: number | string
  Container aspect ratio as `width / height` (e.g. `1.7` or `"16 / 9"`). The height derives from the rendered width so the map fills the frame with no letterboxing. Defaults to the projected aspect of the displayed window, so the land fits edge-to-edge. Pass `height` to fix a pixel height instead.
- `height`: number
  Fixed chart height in pixels. Overrides `aspectRatio` when set. Leave unset to size by aspect ratio (the default) so the map fills the container.
- `className`: string
- `isDarkMode`: boolean

**Examples:**

```tsx
<BubbleMap<CfLocation>
      echarts={echarts}
      geoJson={geoJson}
      data={cloudflareLocations}
      lng="lon"
      lat="lat"
      name="city"
      value={() => 1}
      bubbleColor="#F6821F"
      minRadius={8}
      maxRadius={8}
      tooltipFormatter={(row) =>
        `<span style="font-size:12px"><strong>${row.city}</strong><span style="color:var(--text-color-kumo-subtle);margin-left:8px">${row.iata}</span></span>`
      }
      isDarkMode={isDarkMode}
    />
```

```tsx
<BubbleMap<Colo>
      echarts={echarts}
      geoJson={geoJson}
      data={colos}
      lng="lon"
      lat="lat"
      name="city"
      value="requests"
      valueFormat={fmt}
      minRadius={8}
      isDarkMode={isDarkMode}
    />
```

```tsx
<BubbleMap<Colo>
      echarts={echarts}
      geoJson={geoJson}
      data={colos2}
      lng="lon"
      lat="lat"
      name="city"
      value="requests"
      valueFormat={fmt}
      isDarkMode={isDarkMode}
    />
```


---

### Button

Primary action trigger. Supports multiple variants, sizes, shapes, icons, and loading state.

**Type:** component

**Import:** `import { Button } from "@cloudflare/kumo";`

**Category:** Action

**Props:**

- `shape`: enum [default: base]
  - `"base"`: Default rectangular button shape
  - `"square"`: Square button for icon-only actions
  - `"circle"`: Circular button for icon-only actions
- `size`: enum [default: base]
  - `"xs"`: Extra small button for compact UIs
  - `"sm"`: Small button for secondary actions
  - `"base"`: Default button size
  - `"lg"`: Large button for primary CTAs
- `variant`: enum [default: secondary]
  - `"primary"`: High-emphasis button for primary actions
  - `"secondary"`: Default button style for most actions
  - `"ghost"`: Minimal button with no background
  - `"destructive"`: Danger button for destructive actions like delete
  - `"secondary-destructive"`: Secondary button with destructive text for less prominent dangerous actions
  - `"outline"`: Bordered button with transparent background

  **State Classes:**
  - `"primary"`:
    - `focus`: `focus:ring-(--kumo-button-emphasis-ring) focus-visible:ring-(--kumo-button-emphasis-ring)`
    - `active`: `active:ring-(--kumo-button-emphasis-ring)`
    - `disabled`: `disabled:opacity-50`
  - `"secondary"`:
    - `not-disabled`: `not-disabled:hover:bg-kumo-tint`
    - `disabled`: `disabled:bg-kumo-base/50 disabled:!text-kumo-default/70`
    - `data-state`: `data-[state=open]:bg-kumo-base`
  - `"ghost"`:
    - `hover`: `hover:bg-kumo-tint`
  - `"destructive"`:
    - `focus`: `focus:ring-(--kumo-button-emphasis-ring) focus-visible:ring-(--kumo-button-emphasis-ring)`
    - `active`: `active:ring-(--kumo-button-emphasis-ring)`
    - `disabled`: `disabled:opacity-50`
  - `"secondary-destructive"`:
    - `not-disabled`: `not-disabled:hover:!text-kumo-danger not-disabled:hover:ring-kumo-danger/30`
    - `disabled`: `disabled:bg-kumo-base/50 disabled:!text-kumo-danger/70`
    - `data-state`: `data-[state=open]:bg-kumo-base`
  - `"outline"`:
    - `not-disabled`: `not-disabled:hover:text-kumo-strong not-disabled:hover:ring-kumo-focus/25`
- `children`: ReactNode
- `className`: string
- `icon`: ReactNode
  Icon from `@phosphor-icons/react` or a React element. Rendered before children.
- `loading`: boolean
  Shows a loading spinner and disables interaction.
- `title`: string
- `id`: string
- `lang`: string
- `disabled`: boolean
- `name`: string
- `type`: enum
- `value`: string | string[] | number

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-tint`, `ring-kumo-brand`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-strong`, `text-kumo-subtle`

**Examples:**

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

```tsx
<Button variant="primary">Primary</Button>
```

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

```tsx
<Button variant="secondary" icon={PlusIcon}>
      Create Worker
    </Button>
```

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
      <Button
        variant="secondary"
        title="You need edit access to create a Worker"
        disabled
      >
        Create Worker
      </Button>
    </div>
```


---

### ButtonGroup

Joins a small set of tightly-coupled buttons into a single control — most commonly a **split button**: a primary action next to a dropdown trigger for related, secondary actions.

**Type:** component

**Import:** `import { ButtonGroup } from "@cloudflare/kumo";`

**Category:** Action

**Props:**

- `className`: string
  Additional CSS classes merged via `cn()`. Use kumo semantic tokens only.
- `id`: string
- `lang`: string
- `title`: string
- `children`: ReactNode
  The tightly-coupled controls to join. Typically two `Button`s: a primary action and a dropdown trigger (a "split button").

**Examples:**

```tsx
<ButtonGroup aria-label="Deploy">
      <Button variant="primary">Deploy</Button>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="primary"
              shape="square"
              aria-label="More deploy options"
            >
              <CaretDownIcon />
            </Button>
          }
        />
        <DropdownMenu.Content>
          <DropdownMenu.Item>Deploy to staging</DropdownMenu.Item>
          <DropdownMenu.Item>Deploy and tail logs</DropdownMenu.Item>
          <DropdownMenu.Item>Schedule deploy…</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
    </ButtonGroup>
```

```tsx
<div className="flex flex-wrap items-center gap-4">
      {sizes.map((size) => (
        <ButtonGroup key={size} aria-label="Deploy">
          <Button size={size} variant="primary">
            Deploy
          </Button>
          <DropdownMenu>
            <DropdownMenu.Trigger
              render={
                <Button
                  size={size}
                  variant="primary"
                  shape="square"
                  aria-label="More deploy options"
                >
                  <CaretDownIcon />
                </Button>
              }
            />
            <DropdownMenu.Content>
              <DropdownMenu.Item>Deploy to staging</DropdownMenu.Item>
              <DropdownMenu.Item>Schedule deploy…</DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu>
        </ButtonGroup>
      ))}
    </div>
```


---

### Chart

Chart — a low-level wrapper around [Apache ECharts](https://echarts.apache.org).

**Type:** component

**Import:** `import { Chart } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `echarts`: typeof echarts (required)
  The ECharts core instance imported by the consumer. Passed in rather than imported directly so the consumer controls which ECharts modules are bundled (tree-shaking).
- `options`: KumoChartOption (required)
  ECharts option object — passed through to `chart.setOption()`
- `optionUpdateBehavior`: SetOptionOpts
  Additional options passed as the second argument to `chart.setOption()`. Defaults to `{ notMerge: false, lazyUpdate: true }`.
- `className`: string
  Additional CSS classes applied to the chart container `<div>`
- `isDarkMode`: boolean
  When `true`, initialises ECharts with its built-in dark theme. Changing this value after mount destroys and re-creates the chart instance.
- `height`: number
  Height of the chart container in pixels. Defaults to `350`.
- `aspectRatio`: number | string
  Container aspect ratio as `width / height` (e.g. `1.7` or `"16 / 9"`). When set, the height derives from the rendered width via CSS `aspect-ratio` and the `height` prop is ignored — useful for maps so the canvas matches the map's shape and fills the frame with no letterboxing.

**Examples:**

```tsx
<Chart
      echarts={echarts}
      options={options}
      height={400}
      isDarkMode={isDarkMode}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Count"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Requests"
      yAxisMinInterval={1}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      markers={markers}
      xAxisName="Time (UTC)"
      yAxisName="Count"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      thresholds={[
        {
          value: 55,
          label: "Memory limit",
          color: ChartPalette.semantic("Attention", isDarkMode),
        },
      ]}
      xAxisName="Time (UTC)"
      yAxisName="Memory (MB)"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Requests"
      xAxisTickFormat={(timestamp) =>
        new Intl.DateTimeFormat(undefined, {
          timeZone: "UTC",
          hour: "2-digit",
          minute: "2-digit",
        }).format(timestamp)
      }
      yAxisTickFormat={(value) => {
        if (value >= 1000) return `${value / 1000}k`;
        return value.toString();
      }}
      tooltipValueFormat={(value) => `${(value / 1000).toFixed(1)}k requests`}
      tooltipTimestampFormat={(timestamp) =>
        new Intl.DateTimeFormat(undefined, {
          timeZone: "UTC",
          dateStyle: "medium",
          timeStyle: "medium",
        }).format(timestamp)
      }
    />
```

```tsx
<TimeseriesChart
      yAxisTickCount={2}
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      height={160}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Mbps"
      incomplete={{ after: incompleteTimestamp }}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="%"
      onTimeRangeChange={(from, to) => {
        alert(
          `Selected range:\nFrom: ${new Date(from).toLocaleString()}\nTo: ${new Date(to).toLocaleString()}`,
        );
      }}
    />
```

```tsx
<div className="space-y-4">
      <h3 className="text-sm font-medium">Active State</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem
          name="Requests"
          color={ChartPalette.semantic("Neutral", isDarkMode)}
          value="1,234"
          unit="req/s"
        />
        <ChartLegend.LargeItem
          name="Storage"
          color={ChartPalette.semantic("Attention", isDarkMode)}
          value="56"
          unit="GB"
        />
        <ChartLegend.LargeItem
          name="Warnings"
          color={ChartPalette.semantic("Warning", isDarkMode)}
          value="128"
        />
      </div>

      <h3 className="mt-12 text-sm font-medium">Inactive State</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem
          name="Requests"
          color={ChartPalette.semantic("Neutral", isDarkMode)}
          value="1,234"
          unit="req/s"
          inactive
        />
        <ChartLegend.LargeItem
          name="Storage"
          color={ChartPalette.semantic("Attention", isDarkMode)}
          value="56"
          unit="GB"
          inactive
        />
        <ChartLegend.LargeItem
          name="Warnings"
          color={ChartPalette.semantic("Warning", isDarkMode)}
          value="128"
          inactive
        />
      </div>

      <h3 className="mt-12 text-sm font-medium">Loading state</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem loading />
      </div>
    </div>
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      type="bar"
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Count"
      tooltipValueFormat={(r) => r.toFixed(2)}
    />
```

```tsx
<div className="flex w-full flex-1 flex-col">
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={[]}
        loading
      />
    </div>
```

```tsx
<div className="flex w-full flex-1 flex-col">
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        type="bar"
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={[]}
        loading
      />
    </div>
```

```tsx
<div className="flex w-full flex-1 flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="flex flex-wrap gap-4">
          {data.map((series) => (
            <ChartLegend.SmallItem
              loading={loading}
              key={series.name}
              name={series.name}
              color={series.color}
              value={Math.round(series.data.at(-1)?.[1] ?? 0).toLocaleString()}
            />
          ))}
        </div>
        <Switch
          label="Loading"
          checked={loading}
          onCheckedChange={setLoading}
        />
      </div>
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={loading ? [] : data}
        loading={loading}
      />
    </div>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex gap-4 divide-x divide-kumo-hairline px-2">
          <ChartLegend.LargeItem
            name="P99"
            color={ChartPalette.semantic("Attention", isDarkMode)}
            value="124"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P95"
            color={ChartPalette.semantic("Warning", isDarkMode)}
            value="76"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P75"
            color={ChartPalette.semantic("Neutral", isDarkMode)}
            value="32"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P50"
            color={ChartPalette.semantic("Neutral", isDarkMode)}
            value="10"
            unit="ms"
          />
        </div>
        <TimeseriesChart
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
          tooltipFooter="Percentiles use a five-minute rolling window."
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex divide-x divide-kumo-line px-2">
          {series.map((s) => (
            <ChartLegend.LargeItem
              key={s.name}
              name={s.name}
              color={s.color}
              value={s.value}
              unit={s.unit}
              inactive={hoveredSeries !== null && hoveredSeries !== s.name}
              onPointerEnter={() => {
                setHoveredSeries(s.name);
                chartRef.current?.dispatchAction({
                  type: "highlight",
                  seriesName: s.name,
                });
              }}
              onPointerLeave={() => {
                setHoveredSeries(null);
                chartRef.current?.dispatchAction({
                  type: "downplay",
                  seriesName: s.name,
                });
              }}
              className="not-first:pl-4"
            />
          ))}
        </div>
        <TimeseriesChart
          ref={chartRef}
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex divide-x divide-kumo-line px-2">
          {series.map((s) => (
            <ChartLegend.LargeItem
              key={s.name}
              name={s.name}
              color={s.color}
              value={s.value}
              unit={s.unit}
              inactive={hiddenSeries[s.name] ?? false}
              onClick={() => handleClick(s.name)}
              className="not-first:pl-4"
            />
          ))}
        </div>
        <TimeseriesChart
          ref={chartRef}
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
          enableLegendSelection
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<div className="flex w-full flex-col gap-4">
      <Select
        label="Tooltip follow cursor"
        value={selected}
        onValueChange={(v) => {
          if (v) setSelected(v);
        }}
        renderValue={(v) => v.label}
      >
        {FOLLOW_CURSOR_OPTIONS.map((opt) => (
          <Select.Option key={opt.value} value={opt}>
            {opt.label}
          </Select.Option>
        ))}
      </Select>
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        data={data}
        xAxisName="Time (UTC)"
        yAxisName="Latency (ms)"
        tooltipFollowCursor={selected.value}
      />
    </div>
```

```tsx
<div
      ref={boundaryRef}
      className="w-full overflow-auto rounded-lg border border-kumo-line"
      style={{ height: 300 }}
    >
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        data={data}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        height={280}
        tooltipBoundary={boundary ?? undefined}
      />
    </div>
```


---

### Checkbox

Checkbox component

**Type:** component

**Import:** `import { Checkbox } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `variant`: enum [default: default]
  - `"default"`: Default checkbox appearance
  - `"error"`: Error state for validation failures

  **State Classes:**
  - `"default"`:
    - `focus`: `[&:focus-within>span]:ring-kumo-focus`
    - `hover`: `[&:hover>span]:ring-kumo-hairline`
- `label`: ReactNode
  Label content for the checkbox (enables built-in Field wrapper) - can be a string or any React node
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon
- `controlFirst`: boolean
  When true (default), checkbox appears before label. When false, label appears before checkbox.
- `checked`: boolean
  Whether the checkbox is checked (controlled)
- `indeterminate`: boolean
  Whether the checkbox is in indeterminate state
- `disabled`: boolean
  Whether the checkbox is disabled
- `name`: string
  Name for form submission
- `required`: boolean
  Whether the field is required
- `className`: string
  Additional class name

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-elevated`, `bg-kumo-tint`, `border-kumo-danger`, `border-kumo-hairline`, `border-kumo-interact`, `border-kumo-line`, `ring-kumo-brand`, `ring-kumo-contrast`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Styling:**

- **Dimensions:** `h-4 w-4`
- **Border Radius:** `rounded-sm`
- **Base Tokens:** `bg-kumo-base`, `ring-kumo-line`
- **States:**
  - `checked`: `bg-kumo-contrast`, `text-kumo-inverse`
  - `indeterminate`: `bg-kumo-contrast`, `text-kumo-inverse`
  - `error`: `ring-kumo-danger`
  - `hover`: `ring-kumo-hairline`
  - `focus`: `ring-kumo-hairline`
  - `disabled`: `opacity-50`, `cursor-not-allowed`
- **Icons:**
  - `ph-check` (checked) size 12
  - `ph-minus` (indeterminate) size 12

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Checkbox.Item

Item sub-component

#### Checkbox.Group

Group sub-component

Props:
- `legend`: string - Legend text for the group. For more control over legend styling, omit this prop and use `<Checkbox.Legend>` as a child instead.
- `children`: ReactNode (required) - Child Checkbox.Item components (and optionally a Checkbox.Legend)
- `error`: string - Error message for the group (only appears in groups, not single checkboxes)
- `description`: ReactNode - Helper text for the group
- `value`: string[] - Values of checkboxes that should be checked (controlled)
- `allValues`: string[] - All possible checkbox values (required for parent checkbox pattern)
- `disabled`: boolean - Whether all checkboxes in the group are disabled
- `orientation`: "vertical" | "horizontal" - Layout direction of the checkbox items
- `appearance`: KumoCheckboxAppearance - Visual appearance applied to all Checkbox.Item children. - `"default"` — Standard inline checkbox items - `"card"` — Choice card with border, padding, and highlighted selection state Individual items can override this with their own `appearance` prop.
- `controlFirst`: boolean - When true, checkbox appears before label. When false, label appears before checkbox. Defaults to true for default appearance and false for card appearance.
- `className`: string - Additional CSS classes

#### Checkbox.Legend

Legend sub-component

Props:
- `children`: ReactNode (required) - Legend content
- `className`: string - Additional CSS classes (e.g. "sr-only" to visually hide the legend)


**Examples:**

```tsx
<Checkbox
      label="Accept terms and conditions"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

```tsx
<Checkbox
      label="Select all"
      indeterminate={indeterminate}
      onCheckedChange={setIndeterminate}
    />
```

```tsx
<Checkbox
      label="Remember me"
      controlFirst={false}
      checked={checked}
      onCheckedChange={setChecked}
    />
```

```tsx
<Checkbox label="Disabled option" disabled />
```

```tsx
<Checkbox label="Invalid option" variant="error" />
```

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

```tsx
<Checkbox.Group
      legend="Where should this profile scan?"
      appearance="card"
      value={products}
      onValueChange={setProducts}
    >
      <Checkbox.Item
        value="gateway"
        label="Web traffic"
        description="Uploads and downloads through Gateway HTTP policies."
      />
      <Checkbox.Item
        value="ai"
        label="AI prompts"
        description="Prompts sent to AI apps, and the responses they return."
      />
      <Checkbox.Item
        value="casb"
        label="SaaS and cloud storage"
        description="Files stored in connected apps and cloud accounts."
      />
      <Checkbox.Item
        value="email"
        label="Outbound email"
        description="Email leaving your organization."
      />
    </Checkbox.Group>
```

```tsx
<Checkbox.Group
      legend="Where should this profile scan?"
      appearance="card"
      orientation="horizontal"
      value={products}
      onValueChange={setProducts}
    >
      <Checkbox.Item
        value="gateway"
        label="Web traffic"
        description="Uploads and downloads through Gateway HTTP policies."
      />
      <Checkbox.Item
        value="ai"
        label="AI prompts"
        description="Prompts sent to AI apps, and the responses they return."
      />
      <Checkbox.Item
        value="casb"
        label="SaaS and cloud storage"
        description="Files stored in connected apps and cloud accounts."
      />
      <Checkbox.Item
        value="email"
        label="Outbound email"
        description="Email leaving your organization."
      />
    </Checkbox.Group>
```


---

### ChoroplethMap

ChoroplethMap component

**Type:** component

**Import:** `import { ChoroplethMap } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `echarts`: typeof echarts (required)
  The ECharts core instance imported by the consumer (passed in for tree-shaking). Requires `MapChart`, `VisualMapComponent`, `TooltipComponent`, and a renderer registered via `echarts.use([...])`.
- `geoJson`: MapGeoJson (required)
  GeoJSON `FeatureCollection` whose regions are shaded by value.
- `mapName`: string
  Optional stable ECharts map registry name. Set this when the same GeoJSON is parsed into new object instances across mounts and should reuse one global ECharts registration.
- `data`: T[] (required)
  Raw data rows. The region key and value are read via the accessors below.
- `name`: MapAccessor<T, string> (required)
  Region-key accessor (key of `T` or `(row) => string`). Each row is joined to a GeoJSON feature whose `nameProperty` equals this value.
- `value`: MapAccessor<T, number> (required)
  Value accessor — drives the region's fill colour.
- `nameProperty`: string
  GeoJSON feature property to join on. Default: `"name"`. Real-world data is often more reliably matched on an ISO-code property (e.g. `"iso_a2"`).
- `colorRange`: string[]
  Sequential colour ramp (low → high). Defaults to the Kumo choropleth blues, tuned to stay distinct from the no-data fill. Distributed across the continuous gradient.
- `noDataColor`: string
  Fill for regions with no matching data row. Defaults to the neutral land grey.
- `showLegend`: boolean
  Show the visualMap colour legend. Default: `false`.
- `showTooltip`: boolean
  Show the tooltip. Default: `true`.
- `valueFormat`: (value: number) => string
  Format the value for the default tooltip. Default: `toLocaleString()`.
- `tooltipFormatter`: (row: T) => string
  Override the tooltip content for a row. Returns an HTML string rendered by ECharts' own tooltip. USE WITH CAUTION: the return value is injected as HTML. Escape any user-provided strings to avoid XSS.
- `center`: [number, number]
  Map center as `[longitude, latitude]`. Defaults to auto-fit.
- `zoom`: number
  Zoom level — multiplies the auto-fit scale. Default: `1.25`.
- `roam`: boolean
  Enable drag-to-pan and scroll-to-zoom. Default: `false`.
- `projection`: MapProjection | null
  Geographic projection. Defaults to a latitude-clamped Mercator (flat 2D web-map look). Pass another d3-geo projection to override, or `null` for ECharts' raw equirectangular plotting. Use a stable reference (module-level or memoised): a new object each render rebuilds the view and resets a roamed/zoomed map.
- `aspectRatio`: number | string
  Container aspect ratio as `width / height` (e.g. `1.7` or `"16 / 9"`). The height derives from the rendered width so the map fills the frame with no letterboxing. Defaults to the projected aspect of the displayed window, so the regions fit edge-to-edge. Pass `height` to fix a pixel height instead.
- `height`: number
  Fixed chart height in pixels. Overrides `aspectRatio` when set. Leave unset to size by aspect ratio (the default) so the map fills the container.
- `className`: string
- `isDarkMode`: boolean

**Examples:**

```tsx
<ChoroplethMap<CountryTraffic>
      echarts={echarts}
      geoJson={geoJson}
      data={countries}
      name="country"
      value="requests"
      valueFormat={fmt}
      isDarkMode={isDarkMode}
    />
```


---

### ClipboardText

Read-only text field with a one-click copy-to-clipboard button.

**Type:** component

**Import:** `import { ClipboardText } from "@cloudflare/kumo";`

**Category:** Action

**Props:**

- `size`: enum [default: lg]
  - `"sm"`: Small clipboard text for compact UIs
  - `"base"`: Default clipboard text size
  - `"lg"`: Large clipboard text for prominent display
- `text`: string (required)
  The text to display and copy to clipboard.
- `textToCopy`: string
  If provided, this text will be copied to clipboard instead of the `text` prop.
- `className`: string
  Additional CSS classes merged via `cn()`.
- `tooltip`: object
  Tooltip config. Shows tooltip on hover, anchored toast on click.
- `labels`: object
  Accessible labels for i18n.

**Colors (kumo tokens used):**

`bg-kumo-base`, `border-kumo-line`, `outline-kumo-line`, `ring-kumo-brand`, `ring-kumo-focus`, `text-kumo-default`

**Styling:**

- **Base Tokens:** `bg-kumo-base`, `text-kumo-default`, `ring-kumo-line`, `border-kumo-fill`
- **States:**
  - `input`: `bg-kumo-control`, `text-kumo-default`, `ring-kumo-line`
  - `text`: `bg-kumo-base`, `font-mono`
  - `button`: `border-kumo-fill`
- **Icons:**
  - `ph-clipboard` (default) size 16
  - `ph-check` (copied) size 16
- **Input Styles:**
  - Base: `bg-kumo-control text-kumo-default ring ring-kumo-line`
  - Sizes:
    - `xs`: `h-5 gap-1 rounded-sm px-1.5 text-xs`
    - `sm`: `h-6.5 gap-1 rounded-md px-2 text-xs`
    - `base`: `h-9 gap-1.5 rounded-lg px-3 text-base`
    - `lg`: `h-10 gap-2 rounded-lg px-4 text-base`
- **Size Variants:**
  - `sm`:
    - Height: 26px
    - Classes: `text-xs`
    - Button Size: `sm`
    - Dimensions:
      - paddingX: 8
      - gap: 1
      - borderRadius: 6
      - fontSize: 12
  - `base`:
    - Height: 36px
    - Classes: `text-sm`
    - Button Size: `base`
    - Dimensions:
      - paddingX: 12
      - gap: 6
      - borderRadius: 8
      - fontSize: 14
  - `lg`:
    - Height: 40px
    - Classes: `text-sm`
    - Button Size: `lg`
    - Dimensions:
      - paddingX: 16
      - gap: 8
      - borderRadius: 8
      - fontSize: 14

**Examples:**

```tsx
<ClipboardText text="0c239dd2" />
```

```tsx
<ClipboardText
      text="sk_live_***********"
      textToCopy="sk_live_51H8_abc123"
    />
```

```tsx
<ClipboardText
      text="npx kumo add button"
      tooltip={{ text: "Copy", copiedText: "Copied!", side: "top" }}
    />
```


---

### CloudflareLogo

Cloudflare logo component.

**Type:** component

**Import:** `import { CloudflareLogo } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `children`: ReactNode
- `className`: string
- `height`: number | string
- `id`: string
- `lang`: string
- `media`: string
- `method`: string
- `name`: string
- `target`: string
- `type`: string
- `width`: number | string
- `accentHeight`: number | string
- `accumulate`: enum
- `additive`: enum
- `alignmentBaseline`: enum
- `allowReorder`: enum
- `alphabetic`: number | string
- `amplitude`: number | string
- `arabicForm`: enum
- `ascent`: number | string
- `attributeName`: string
- `attributeType`: string
- `autoReverse`: Booleanish
- `azimuth`: number | string
- `baseFrequency`: number | string
- `baselineShift`: number | string
- `baseProfile`: number | string
- `bbox`: number | string
- `begin`: number | string
- `bias`: number | string
- `by`: number | string
- `calcMode`: number | string
- `capHeight`: number | string
- `clip`: number | string
- `clipPath`: string
- `clipPathUnits`: number | string
- `clipRule`: number | string
- `colorInterpolation`: number | string
- `colorInterpolationFilters`: enum
- `colorProfile`: number | string
- `colorRendering`: number | string
- `contentScriptType`: number | string
- `contentStyleType`: number | string
- `cursor`: number | string
- `cx`: number | string
- `cy`: number | string
- `d`: string
- `decelerate`: number | string
- `descent`: number | string
- `diffuseConstant`: number | string
- `direction`: number | string
- `display`: number | string
- `divisor`: number | string
- `dominantBaseline`: enum
- `dur`: number | string
- `dx`: number | string
- `dy`: number | string
- `edgeMode`: number | string
- `elevation`: number | string
- `enableBackground`: number | string
- `end`: number | string
- `exponent`: number | string
- `externalResourcesRequired`: Booleanish
- `fill`: string
- `fillOpacity`: number | string
- `fillRule`: enum
- `filter`: string
- `filterRes`: number | string
- `filterUnits`: number | string
- `floodColor`: number | string
- `floodOpacity`: number | string
- `focusable`: Booleanish | string
- `fontFamily`: string
- `fontSize`: number | string
- `fontSizeAdjust`: number | string
- `fontStretch`: number | string
- `fontStyle`: number | string
- `fontVariant`: number | string
- `fontWeight`: number | string
- `format`: number | string
- `fr`: number | string
- `from`: number | string
- `fx`: number | string
- `fy`: number | string
- `g1`: number | string
- `g2`: number | string
- `glyphName`: number | string
- `glyphOrientationHorizontal`: number | string
- `glyphOrientationVertical`: number | string
- `glyphRef`: number | string
- `gradientTransform`: string
- `gradientUnits`: string
- `hanging`: number | string
- `horizAdvX`: number | string
- `horizOriginX`: number | string
- `href`: string
- `ideographic`: number | string
- `imageRendering`: number | string
- `in2`: number | string
- `in`: string
- `intercept`: number | string
- `k1`: number | string
- `k2`: number | string
- `k3`: number | string
- `k4`: number | string
- `k`: number | string
- `kernelMatrix`: number | string
- `kernelUnitLength`: number | string
- `kerning`: number | string
- `keyPoints`: number | string
- `keySplines`: number | string
- `keyTimes`: number | string
- `lengthAdjust`: number | string
- `letterSpacing`: number | string
- `lightingColor`: number | string
- `limitingConeAngle`: number | string
- `local`: number | string
- `markerEnd`: string
- `markerHeight`: number | string
- `markerMid`: string
- `markerStart`: string
- `markerUnits`: number | string
- `markerWidth`: number | string
- `mask`: string
- `maskContentUnits`: number | string
- `maskUnits`: number | string
- `mathematical`: number | string
- `mode`: number | string
- `numOctaves`: number | string
- `offset`: number | string
- `opacity`: number | string
- `operator`: number | string
- `order`: number | string
- `orient`: number | string
- `orientation`: number | string
- `origin`: number | string
- `overflow`: number | string
- `overlinePosition`: number | string
- `overlineThickness`: number | string
- `paintOrder`: number | string
- `panose1`: number | string
- `path`: string
- `pathLength`: number | string
- `patternContentUnits`: string
- `patternTransform`: number | string
- `patternUnits`: string
- `pointerEvents`: number | string
- `points`: string
- `pointsAtX`: number | string
- `pointsAtY`: number | string
- `pointsAtZ`: number | string
- `preserveAlpha`: Booleanish
- `preserveAspectRatio`: string
- `primitiveUnits`: number | string
- `r`: number | string
- `radius`: number | string
- `refX`: number | string
- `refY`: number | string
- `renderingIntent`: number | string
- `repeatCount`: number | string
- `repeatDur`: number | string
- `requiredExtensions`: number | string
- `requiredFeatures`: number | string
- `restart`: number | string
- `result`: string
- `rotate`: number | string
- `rx`: number | string
- `ry`: number | string
- `scale`: number | string
- `seed`: number | string
- `shapeRendering`: number | string
- `slope`: number | string
- `spacing`: number | string
- `specularConstant`: number | string
- `specularExponent`: number | string
- `speed`: number | string
- `spreadMethod`: string
- `startOffset`: number | string
- `stdDeviation`: number | string
- `stemh`: number | string
- `stemv`: number | string
- `stitchTiles`: number | string
- `stopColor`: string
- `stopOpacity`: number | string
- `strikethroughPosition`: number | string
- `strikethroughThickness`: number | string
- `string`: number | string
- `stroke`: string
- `strokeDasharray`: string | number
- `strokeDashoffset`: string | number
- `strokeLinecap`: enum
- `strokeLinejoin`: enum
- `strokeMiterlimit`: number | string
- `strokeOpacity`: number | string
- `strokeWidth`: number | string
- `surfaceScale`: number | string
- `systemLanguage`: number | string
- `tableValues`: number | string
- `targetX`: number | string
- `targetY`: number | string
- `textAnchor`: enum
- `textDecoration`: number | string
- `textLength`: number | string
- `textRendering`: number | string
- `to`: number | string
- `transform`: string
- `u1`: number | string
- `u2`: number | string
- `underlinePosition`: number | string
- `underlineThickness`: number | string
- `unicode`: number | string
- `unicodeBidi`: number | string
- `unicodeRange`: number | string
- `unitsPerEm`: number | string
- `vAlphabetic`: number | string
- `values`: string
- `vectorEffect`: number | string
- `version`: string
- `vertAdvY`: number | string
- `vertOriginX`: number | string
- `vertOriginY`: number | string
- `vHanging`: number | string
- `vIdeographic`: number | string
- `viewBox`: string
- `viewTarget`: number | string
- `visibility`: number | string
- `vMathematical`: number | string
- `widths`: number | string
- `wordSpacing`: number | string
- `writingMode`: number | string
- `x1`: number | string
- `x2`: number | string
- `x`: number | string
- `xChannelSelector`: string
- `xHeight`: number | string
- `xlinkActuate`: string
- `xlinkArcrole`: string
- `xlinkHref`: string
- `xlinkRole`: string
- `xlinkShow`: string
- `xlinkTitle`: string
- `xlinkType`: string
- `xmlBase`: string
- `xmlLang`: string
- `xmlns`: string
- `xmlnsXlink`: string
- `xmlSpace`: string
- `y1`: number | string
- `y2`: number | string
- `y`: number | string
- `yChannelSelector`: string
- `z`: number | string
- `zoomAndPan`: string
- `variant`: enum [default: full]
  - `"glyph"`: Cloud glyph only (logomark)
  - `"full"`: Full logo with cloud glyph and wordmark stacked

**Colors (kumo tokens used):**

`bg-kumo-base`, `ring-kumo-hairline`, `text-kumo-default`

**Examples:**

```tsx
<CloudflareLogo className="w-72" />
```

```tsx
<CloudflareLogo variant="glyph" className="w-24" />
```

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

```tsx
<div className="flex flex-wrap items-end gap-6">
      <CloudflareLogo className="w-20" />
      <CloudflareLogo className="w-28" />
      <CloudflareLogo className="w-44" />
    </div>
```

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

```tsx
<PoweredByCloudflare />
```

```tsx
<div className="flex flex-wrap items-center gap-4">
      <PoweredByCloudflare />
      <PoweredByCloudflare color="black" />
      <div className="rounded-lg bg-black p-3">
        <PoweredByCloudflare color="white" />
      </div>
    </div>
```

```tsx
<footer className="flex w-full items-center justify-between rounded-lg border border-kumo-hairline bg-kumo-elevated px-6 py-4">
      <span className="text-sm text-kumo-subtle">
        &copy; 2026 Your Company. All rights reserved.
      </span>
      <PoweredByCloudflare />
    </footer>
```


---

### Code

Code component

**Type:** component

**Import:** `import { Code } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `lang`: enum [default: ts]
  - `"ts"`: TypeScript code
  - `"tsx"`: TypeScript JSX code
  - `"jsonc"`: JSON with comments
  - `"bash"`: Shell/Bash commands
  - `"css"`: CSS styles
- `code`: string (required)
  The code string to display.
- `values`: Record<string, { value: string; highlight?: boolean }>
  Template values for `{{key}}` interpolation. Values with `highlight: true` are visually emphasized.
- `className`: string
  Additional CSS classes merged via `cn()`.

**Colors (kumo tokens used):**

`bg-kumo-base`, `border-kumo-fill`, `text-kumo-subtle`

**Styling:**

- **Dimensions:** `[object Object]`
- **Base Tokens:** `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Code.Block

Block sub-component

Props:
- `code`: string (required) - The code string to display.
- `lang`: CodeLang - Language hint for the code content.


---

### Collapsible

Collapsible — a composable disclosure component for showing/hiding content.

**Type:** component

**Import:** `import { Collapsible } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `className`: string
  Additional CSS classes
- `onOpenChange`: (open: boolean) => void
  Callback when collapsed state changes

**Colors (kumo tokens used):**

`border-kumo-fill`, `text-kumo-default`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Collapsible.Root

Root sub-component

Props:
- `className`: string - Additional CSS classes

#### Collapsible.Trigger

Trigger sub-component

Props:
- `className`: string - Additional CSS classes

#### Collapsible.Panel

Panel sub-component

Props:
- `className`: string - Additional CSS classes

#### Collapsible.DefaultTrigger

DefaultTrigger sub-component

Props:
- `children`: ReactNode (required) - Label text displayed in the trigger
- `className`: string - Additional CSS classes

#### Collapsible.DefaultPanel

DefaultPanel sub-component

Props:
- `children`: ReactNode (required) - Panel content
- `className`: string - Additional CSS classes


**Examples:**

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

```tsx
<div className="w-full space-y-2">
      <Collapsible.Root open={open1} onOpenChange={setOpen1}>
        <Collapsible.DefaultTrigger>What is Kumo?</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Kumo is Cloudflare's new design system.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
      <Collapsible.Root open={open2} onOpenChange={setOpen2}>
        <Collapsible.DefaultTrigger>
          How do I use it?
        </Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Install the components and import them into your project.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
      <Collapsible.Root open={open3} onOpenChange={setOpen3}>
        <Collapsible.DefaultTrigger>
          Is it open source?
        </Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <Text>Check the repository for license information.</Text>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

```tsx
<div className="w-full">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.Trigger render={<Button variant="secondary" size="sm" />}>
          {isOpen ? "Hide details" : "Show details"}
        </Collapsible.Trigger>
        <Collapsible.Panel className="mt-3 rounded-lg bg-kumo-tint p-4">
          <Text>
            This panel uses custom styling instead of the default border-left
            accent.
          </Text>
        </Collapsible.Panel>
      </Collapsible.Root>
    </div>
```

```tsx
<div className="w-full space-y-4">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.DefaultTrigger>Edit details</Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel keepMounted>
          <Text>
            Type something below, then collapse and re-open — your input is
            preserved because the panel stays mounted.
          </Text>
          <Input label="Name" placeholder="Type here…" />
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

```tsx
<div className="w-full">
      <Collapsible.Root open={isOpen} onOpenChange={setIsOpen}>
        <Collapsible.DefaultTrigger>
          Contact settings
        </Collapsible.DefaultTrigger>
        <Collapsible.DefaultPanel>
          <form
            className="flex max-w-sm flex-col gap-4"
            onSubmit={(event) => event.preventDefault()}
          >
            <Input label="Email" placeholder="user@example.com" type="email" />
            <Input label="Team name" placeholder="Design engineering" />
            <Button type="submit">Save settings</Button>
          </form>
        </Collapsible.DefaultPanel>
      </Collapsible.Root>
    </div>
```

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


---

### Combobox

Combobox — autocomplete input with filterable dropdown list.

**Type:** component

**Import:** `import { Combobox } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `size`: enum [default: base]
  Size of the combobox trigger. Matches Input component sizes.
- `"xs"` — Extra small for compact UIs (h-5 / 20px)
- `"sm"` — Small for secondary fields (h-6.5 / 26px)
- `"base"` — Default size (h-9 / 36px)
- `"lg"` — Large for prominent fields (h-10 / 40px)
- `inputSide`: enum [default: right]
  - `"right"`: Input positioned inline to the right of chips
  - `"top"`: Input positioned above chips
- `items`: T[] (required)
  Array of items to display in the dropdown
- `value`: T | T[]
  Currently selected value(s)
- `children`: ReactNode
  Combobox content (trigger, content, items)
- `className`: string
  Additional CSS classes
- `label`: ReactNode
  Label content for the combobox (enables Field wrapper) - can be a string or any React node
- `required`: boolean
  Whether the combobox is required
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon
- `description`: ReactNode
  Helper text displayed below the combobox
- `error`: string | object
  Error message or validation error object
- `onValueChange`: (value: T | T[]) => void
  Callback when selection changes
- `multiple`: boolean
  Allow multiple selections
- `isItemEqualToValue`: (item: T, value: T) => boolean
  Custom equality function for comparing items

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-fill-hover`, `bg-kumo-overlay`, `bg-kumo-tint`, `border-kumo-hairline`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-placeholder`, `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Combobox.Content

Content sub-component

Props:
- `className`: string
- `align`: ComboboxBase.Positioner.Props["align"]
- `alignOffset`: ComboboxBase.Positioner.Props["alignOffset"]
- `side`: ComboboxBase.Positioner.Props["side"]
- `sideOffset`: ComboboxBase.Positioner.Props["sideOffset"]
- `anchor`: ComboboxBase.Positioner.Props["anchor"]
- `positionMethod`: ComboboxBase.Positioner.Props["positionMethod"]
- `collisionAvoidance`: ComboboxBase.Positioner.Props["collisionAvoidance"]
- `collisionBoundary`: ComboboxBase.Positioner.Props["collisionBoundary"]
- `collisionPadding`: ComboboxBase.Positioner.Props["collisionPadding"]
- `sticky`: ComboboxBase.Positioner.Props["sticky"]
- `disableAnchorTracking`: ComboboxBase.Positioner.Props["disableAnchorTracking"]
- `container`: PortalContainer

#### Combobox.TriggerValue

TriggerValue sub-component

#### Combobox.TriggerInput

TriggerInput sub-component

#### Combobox.TriggerMultipleWithInput

TriggerMultipleWithInput sub-component

#### Combobox.Chip

Chip sub-component

#### Combobox.Item

Item sub-component

#### Combobox.Input

Input sub-component

#### Combobox.Empty

Empty sub-component

#### Combobox.GroupLabel

GroupLabel sub-component

#### Combobox.Group

Group sub-component

#### Combobox.List

List sub-component

#### Combobox.Collection

Renders filtered list items. Use when you need more control over item rendering.

Props:
- `children`: (item: T, index: number) => ReactNode (required) - Function that receives each filtered item and returns a node

Usage:
```tsx
<Combobox.Collection>
  {(item, index) => (
    <Combobox.Item key={index} value={item}>
      {item.label}
    </Combobox.Item>
  )}
</Combobox.Collection>
```

#### Combobox.Trigger

Trigger sub-component (wraps ComboboxBase)

#### Combobox.Value

Value sub-component (wraps ComboboxBase)

#### Combobox.Icon

Icon sub-component (wraps ComboboxBase)


**Examples:**

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

```tsx
<Combobox
      value={value}
      onValueChange={(nextValue) => setValue(nextValue as DatabaseItem | null)}
      items={databaseItems}
    >
      <Combobox.TriggerValue
        className="w-[240px]"
        placeholder="Select a database"
      />
      <Combobox.Content>
        <Combobox.Input placeholder="Search databases" />
        <Combobox.Empty>No databases found.</Combobox.Empty>
        <Combobox.List>
          {(database: DatabaseItem) => (
            <Combobox.Item key={database.value} value={database}>
              {database.label}
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
```

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

```tsx
<div className="flex flex-wrap items-start gap-4">
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
                      {" — "}
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

```tsx
<Combobox
      value={value}
      onValueChange={(v) => setValue(v as Language)}
      items={languages}
    >
      <Combobox.Trigger render={<Button variant="ghost" size="sm" />}>
        <Combobox.Value>
          <span className="truncate">
            {value.emoji} {value.label}
          </span>
        </Combobox.Value>
        <CaretUpDownIcon size={14} className="shrink-0 text-kumo-subtle" />
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


---

### CommandPalette

CommandPalette — accessible command palette / spotlight search overlay.

**Type:** component

**Import:** `import { CommandPalette } from "@cloudflare/kumo";`

**Category:** Navigation

**Props:**

- `open`: boolean (required)
  Whether the dialog is open
- `children`: ReactNode
  Child content - typically one or more Panel components
- `container`: PortalContainer
  Container element for the portal. Use this to render the command palette inside a Shadow DOM or custom container. Overrides `KumoPortalProvider` context.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-elevated`, `bg-kumo-overlay`, `bg-kumo-warning`, `ring-kumo-brand`, `ring-kumo-hairline`, `text-kumo-default`, `text-kumo-subtle`

**Examples:**

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
              ↑↓
            </kbd>
            <span>Navigate</span>
          </span>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ↵
            </kbd>
            <span>Select</span>
          </span>
        </CommandPalette.Footer>
      </CommandPalette.Root>
    </div>
```

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
              ↑↓
            </kbd>
            <span>Navigate</span>
          </span>
          <span className="flex items-center gap-2">
            <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-[10px]">
              ⌘↵
            </kbd>
            <span>Open in new tab</span>
          </span>
        </CommandPalette.Footer>
      </CommandPalette.Root>
    </div>
```


---

### DatePicker

DatePicker — a date selection calendar.

**Type:** component

**Import:** `import { DatePicker } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `className`: string
  Additional CSS classes
- `children`: ReactNode
  Child elements

**Colors (kumo tokens used):**

`bg-kumo-base`

**Examples:**

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

```tsx
<DatePicker
      mode="multiple"
      selected={dates}
      onChange={setDates}
      max={maxDays}
      disabled={unavailableDates}
      fixedWeeks
      footer={
        <p className="w-full pt-2 text-xs text-kumo-subtle">
          {selectedCount}/{maxDays} days selected. Grayed dates are unavailable.
        </p>
      }
    />
```


---

### DateRangePicker

DateRangePicker — dual-calendar date range selector.

**Type:** component

**Import:** `import { DateRangePicker } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `size`: enum [default: base]
  - `"sm"`: Compact calendar for tight spaces
  - `"base"`: Default calendar size
  - `"lg"`: Large calendar for prominent date selection
- `variant`: enum [default: default]
  - `"default"`: Default calendar appearance
  - `"subtle"`: Subtle calendar with minimal background
- `timezone`: string
  Display timezone string shown in the footer.
- `className`: string
  Additional CSS classes merged via `cn()`.
- `onStartDateChange`: (date: Date | null) => void
  Callback when start date changes
- `onEndDateChange`: (date: Date | null) => void
  Callback when end date changes

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-fill`, `bg-kumo-interact`, `bg-kumo-overlay`, `ring-kumo-focus`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Styling:**

- **Size Variants:**
  - `sm`:
    - Classes: `p-3 gap-2`
    - Dimensions:
      - calendarWidth: 168
      - cellHeight: 22
      - cellWidth: 24
      - textSize: 12
      - iconSize: 14
      - padding: 12
      - gap: 8
  - `base`:
    - Classes: `p-4 gap-2.5`
    - Dimensions:
      - calendarWidth: 196
      - cellHeight: 26
      - cellWidth: 28
      - textSize: 14
      - iconSize: 16
      - padding: 16
      - gap: 10
  - `lg`:
    - Classes: `p-5 gap-3`
    - Dimensions:
      - calendarWidth: 252
      - cellHeight: 32
      - cellWidth: 36
      - textSize: 16
      - iconSize: 18
      - padding: 20
      - gap: 12

---

### Dialog

Dialog component

**Type:** component

**Import:** `import { Dialog } from "@cloudflare/kumo";`

**Category:** Overlay

**Props:**

- `className`: string
  Additional CSS classes merged via `cn()`.
- `children`: ReactNode
  Dialog content (typically Title, Description, Close, and action buttons).
- `container`: PortalContainer
  Container element for the portal. Use this to render the dialog inside a Shadow DOM or custom container. Overrides `KumoPortalProvider` context.
- `size`: enum [default: base]
  - `"base"`: Default dialog width (384px)
  - `"sm"`: Small dialog for simple confirmations (288px)
  - `"lg"`: Large dialog for complex content (512px)
  - `"xl"`: Extra large dialog for detailed views (768px)

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-recessed`, `ring-kumo-line`, `text-kumo-default`

**Styling:**

- **Dimensions:** `[object Object]`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Dialog.Root

Root sub-component

#### Dialog.Trigger

Trigger sub-component

#### Dialog.Title

Title sub-component

#### Dialog.Description

Description sub-component

#### Dialog.Close

Close sub-component


**Examples:**

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

```tsx
<Dialog.Root>
      <Dialog.Trigger
        render={(p) => <Button {...p}>Open capped dialog</Button>}
      />
      <Dialog className="max-w-lg p-8">
        <div className="mb-4 flex items-start justify-between gap-4">
          <Dialog.Title className="text-2xl font-semibold">
            Max width override
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
          This dialog uses <code>className="max-w-lg"</code> and should stay
          capped around 512px on desktop.
        </Dialog.Description>
        <div className="mt-4 truncate rounded-md border border-kumo-line bg-kumo-recessed p-3 font-mono text-sm">
          abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789
        </div>
      </Dialog>
    </Dialog.Root>
```

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
          placeholder="Select region..."
          renderValue={(v) => regions.find((r) => r.value === v)?.label}
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

```tsx
<div className="flex flex-wrap gap-2">
      {sizes.map(({ size, label, width }) => (
        <Dialog.Root key={size}>
          <Dialog.Trigger
            render={(p) => (
              <Button variant="secondary" {...p}>
                {label} ({width})
              </Button>
            )}
          />
          <Dialog size={size} className="p-8">
            <div className="mb-4 flex items-start justify-between gap-4">
              <Dialog.Title className="text-2xl font-semibold">
                {label} Dialog
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
              This <code>size="{size}"</code> dialog should stay at {width} wide
              regardless of the content below.
            </Dialog.Description>
            <div className="mt-4 overflow-auto rounded-md border border-kumo-line">
              <table className="w-max text-sm">
                <thead className="bg-kumo-elevated text-left">
                  <tr>
                    <th className="px-3 py-2">Resource</th>
                    <th className="px-3 py-2">Region</th>
                    <th className="px-3 py-2">Status</th>
                    <th className="px-3 py-2">Latency</th>
                    <th className="px-3 py-2">Requests</th>
                    <th className="px-3 py-2">Last Deployed</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-hairline">
                  <tr>
                    <td className="px-3 py-2">api-gateway-prod</td>
                    <td className="px-3 py-2">us-east-1</td>
                    <td className="px-3 py-2 text-kumo-success">Healthy</td>
                    <td className="px-3 py-2">12ms</td>
                    <td className="px-3 py-2">1,234,567</td>
                    <td className="px-3 py-2">2026-06-23</td>
                  </tr>
                  <tr>
                    <td className="px-3 py-2">worker-analytics</td>
                    <td className="px-3 py-2">eu-west-1</td>
                    <td className="px-3 py-2 text-kumo-warning">Degraded</td>
                    <td className="px-3 py-2">89ms</td>
                    <td className="px-3 py-2">456,789</td>
                    <td className="px-3 py-2">2026-06-22</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div className="mt-6 flex justify-end gap-2">
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
      ))}
    </div>
```


---

### DropdownMenu

DropdownMenu — accessible dropdown menu anchored to a trigger.

**Type:** component

**Import:** `import { DropdownMenu } from "@cloudflare/kumo";`

**Category:** Overlay

**Props:**

- `variant`: enum [default: default]
  - `"default"`: Default dropdown item appearance
  - `"danger"`: Destructive action item

**Colors (kumo tokens used):**

`bg-kumo-control`, `bg-kumo-danger`, `bg-kumo-hairline`, `bg-kumo-tint`, `ring-kumo-brand`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### DropdownMenu.Trigger

Trigger sub-component

#### DropdownMenu.Portal

Portal sub-component

#### DropdownMenu.Positioner

Positioner sub-component

#### DropdownMenu.Popup

Popup sub-component

#### DropdownMenu.Sub

Sub sub-component (wraps DropdownMenuPrimitive)

#### DropdownMenu.SubTrigger

SubTrigger sub-component

Props:
- `icon`: Icon - Icon displayed before the label.
- `inset`: boolean - Adds left padding to align with items that have icons.

#### DropdownMenu.SubContent

SubContent sub-component

#### DropdownMenu.Content

Content sub-component

Props:
- `sideOffset`: number [default: 8] - Distance in pixels from the trigger.
- `container`: PortalContainer - Container element for the portal. Use this to render inside a Shadow DOM or custom container.

#### DropdownMenu.Item

Item sub-component

Props:
- `icon`: Icon | ReactNode - Icon displayed before the label.
- `variant`: "default" | "danger" [default: "default"] - Visual style of the item.
- `selected`: boolean - Shows a check mark indicator when true.
- `inset`: boolean - Adds left padding to align with items that have icons.
- `onClick`: (event: React.MouseEvent) => void - Callback when the item is clicked.
- `closeOnClick`: boolean [default: true] - Whether the menu closes after clicking this item.
- `disabled`: boolean - When true, the item cannot be interacted with.

#### DropdownMenu.LinkItem

LinkItem sub-component

Props:
- `href`: string - URL to navigate to when clicked.
- `icon`: Icon | ReactNode - Icon displayed before the label.
- `variant`: "default" | "danger" [default: "default"] - Visual style of the item.
- `inset`: boolean - Adds left padding to align with items that have icons.
- `target`: string - Link target attribute (e.g. "_blank" for new tab).
- `render`: ReactElement | ((props, state) => ReactElement) - Custom element to render as the link. Use to integrate with framework routers (e.g. Next.js Link).

#### DropdownMenu.CheckboxItem

CheckboxItem sub-component

Props:
- `checked`: boolean - Whether the item is checked.
- `defaultChecked`: boolean [default: false] - Whether the item is initially checked (uncontrolled).
- `onCheckedChange`: (checked: boolean, event: ChangeEventDetails) => void - Callback when the checked state changes.
- `closeOnClick`: boolean [default: false] - Whether the menu closes after clicking this item.
- `disabled`: boolean - When true, the item cannot be interacted with.

#### DropdownMenu.RadioGroup

RadioGroup sub-component (wraps DropdownMenuPrimitive)

Props:
- `value`: any - The controlled value of the currently selected radio item.
- `defaultValue`: any - The initially selected value (uncontrolled).
- `onValueChange`: (value: any, event: ChangeEventDetails) => void - Callback when the selected value changes.
- `disabled`: boolean - When true, all radio items in the group are disabled.

#### DropdownMenu.RadioItem

RadioItem sub-component

Props:
- `value`: any (required) - The value of this radio item.
- `icon`: Icon | ReactNode - Icon displayed before the label.
- `inset`: boolean - Adds left padding to align with items that have icons.
- `closeOnClick`: boolean [default: false] - Whether the menu closes after clicking this item.
- `disabled`: boolean - When true, the item cannot be interacted with.

#### DropdownMenu.RadioItemIndicator

RadioItemIndicator sub-component

#### DropdownMenu.Label

Label sub-component

Props:
- `inset`: boolean - Adds left padding to align with items that have icons.

#### DropdownMenu.Separator

Separator sub-component

#### DropdownMenu.Shortcut

Shortcut sub-component

#### DropdownMenu.Group

Group sub-component (wraps DropdownMenuPrimitive)


**Examples:**

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

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button icon={UserIcon}>Account</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={UserIcon}>Profile</DropdownMenu.Item>
        <DropdownMenu.Item icon={CreditCardIcon}>Billing</DropdownMenu.Item>
        <DropdownMenu.Item icon={MoonIcon}>Dark mode</DropdownMenu.Item>

        {/* Language submenu with RadioGroup */}
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger icon={GlobeIcon}>
            Language
          </DropdownMenu.SubTrigger>
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
          <DropdownMenu.SubTrigger icon={<ClockIcon weight="duotone" />}>
            Set Timezone
          </DropdownMenu.SubTrigger>
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

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button>Edit</Button>} />
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={PencilSimpleIcon}>Rename</DropdownMenu.Item>
        <DropdownMenu.Item icon={CopyIcon}>Duplicate</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item>Move to folder</DropdownMenu.Item>
        <DropdownMenu.Item>Add to favorites</DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={TrashIcon} variant="danger">
          Delete
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
```

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
          <DropdownMenu.Item
            icon={<ArrowsClockwiseIcon className="animate-spin" />}
            onClick={() => setLastAction("Refreshed")}
          >
            Refresh
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

```tsx
<DropdownMenu>
      <DropdownMenu.Trigger render={<Button>Open long list</Button>} />
      <DropdownMenu.Content>
        {items.map((item) => (
          <DropdownMenu.Item key={item}>{item}</DropdownMenu.Item>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
```

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


---

### Empty

Placeholder shown when a list, table, or page has no content to display.

**Type:** component

**Import:** `import { Empty } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `size`: enum [default: base]
  - `"sm"`: Compact empty state for smaller containers
  - `"base"`: Default empty state size
  - `"lg"`: Large empty state for prominent placement
- `icon`: ReactNode
  Decorative icon displayed above the title (e.g. from `@phosphor-icons/react`).
- `title`: string (required)
  Primary heading text for the empty state.
- `description`: string
  Secondary description text displayed below the title.
- `commandLine`: string
  Shell command displayed in a copyable code block.
- `contents`: ReactNode
  Additional content (buttons, links) rendered below the description.
- `className`: string
  Additional CSS classes merged via `cn()`.

**Colors (kumo tokens used):**

`bg-kumo-control`, `bg-kumo-overlay`, `border-kumo-fill`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-subtle`, `text-kumo-success`

**Examples:**

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

```tsx
<Empty
      icon={<FolderOpen size={48} className="text-kumo-inactive" />}
      title="No projects found"
      description="Get started by creating your first project using the command below."
      commandLine="npm create kumo-project"
    />
```

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

```tsx
<Empty title="Nothing here" />
```

```tsx
<Empty
      title="No results found"
      description="Try adjusting your search or filter to find what you're looking for."
    />
```


---

### Field

Form field wrapper that provides a label, optional description, and error display around any form control. Built on Base UI Field primitives.

**Type:** component

**Import:** `import { Field } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `controlFirst`: boolean
  When `true`, places the control before the label (for checkbox/switch layouts).
- `children`: ReactNode
  The form control element(s) to wrap (Input, Select, Checkbox, etc.).
- `label`: ReactNode
  The label content — can be a string or any React node.
- `required`: boolean
  When explicitly `false`, shows gray "(optional)" text after the label. When `true` or `undefined`, no indicator is shown.
- `labelTooltip`: ReactNode
  Tooltip content displayed next to the label via an info icon.
- `error`: object
  Validation error with a message and a browser `ValidityState` match key.
- `description`: ReactNode
  Helper text displayed below the control (hidden when `error` is present).
- `hideLabel`: boolean
  When `true`, Field renders layout, description, and error but skips the `<label>` element. Use when the child component provides its own accessible label (e.g. Select uses Base UI's `Select.Label` to avoid hover/focus coupling from native `<label>`).

**Colors (kumo tokens used):**

`text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`

---

### GlobeMap

GlobeMap — an SVG orthographic globe with hatched land and geographic markers. Rendering is SVG-only and does not use WebGL.

**Type:** component

**Import:** `import { GlobeMap } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `landColor`: string
  Stroke color for the hatched land. Defaults to the neutral Kumo map area color.
- `landHatchSpacing`: number
  Spacing between land hatch lines in view-box pixels. Default: `10`.
- `oceanColor`: string
  Fill behind the land and graticule. Default: the Kumo base surface.
- `markers`: GlobeMapMarker[]
  Geographic points drawn above the land. Points fade at the horizon and back-facing points are hidden.
- `markerColor`: string
  Default marker fill. Defaults to the Kumo chart blue.
- `markerRadius`: number
  Default marker radius in view-box pixels. Default: `7`.
- `defaultRotation`: number[]
  Initial globe rotation as `[longitude, latitude, roll]`.
- `autoRotate`: boolean
  Continuously rotate the globe horizontally. Default: `false`.
- `autoRotateSpeed`: number
  Horizontal auto-rotation speed in degrees per second. Default: `4`.
- `showGraticule`: boolean
  Draw latitude and longitude guides. Default: `false`.
- `showTooltip`: boolean
  Show the Kumo-styled marker tooltip. Default: `true`.
- `height`: number
  Fixed component height. Otherwise the globe uses a square aspect ratio.
- `className`: string
- `isDarkMode`: boolean

**Colors (kumo tokens used):**

`bg-kumo-base`, `border-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Examples:**

```tsx
<div className="mx-auto max-w-xl">
      <GlobeMap
        markers={cloudflareAvailabilityLocations}
        landColor="var(--text-color-kumo-inactive)"
        landHatchSpacing={8}
        oceanColor="transparent"
        showGraticule
        markerColor="var(--color-kumo-brand)"
        markerRadius={8}
        autoRotate
        aria-label="Cloudflare availability locations"
        isDarkMode={isDarkMode}
      />
    </div>
```


---

### Grid

Responsive CSS grid layout container with preset column configurations.

**Type:** component

**Import:** `import { Grid } from "@cloudflare/kumo";`

**Category:** Layout

**Props:**

- `children`: ReactNode
  Grid items to render.
- `className`: string
  Additional CSS classes merged via `cn()`.
- `id`: string
- `lang`: string
- `title`: string
- `mobileDivider`: boolean
  Show dividers between grid items on mobile (only works with `"4up"` variant).
- `gap`: enum [default: base]
  - `"none"`: No gap between grid items
  - `"sm"`: Small gap between grid items
  - `"base"`: Default responsive gap between grid items
  - `"lg"`: Large gap between grid items
- `variant`: enum
  - `"2up"`: Grid items stack on small screens, display side-by-side on medium screens and up
  - `"side-by-side"`: Grid items always displayed side-by-side
  - `"2-1"`: Two-thirds / one-third split (66%/33%) on medium screens and up
  - `"1-2"`: One-third / two-thirds split (33%/66%) on medium screens and up
  - `"1-3up"`: Grid items stack on small screens, expand to 3 across on large screens
  - `"3up"`: Grid items stack on small screens, 2 across on medium, 3 across on large
  - `"4up"`: Grid items stack on small screens, progressively increase columns at larger breakpoints
  - `"6up"`: Grid items start at 2 across, expand to 6 across on XL
  - `"1-2-4up"`: Grid items stack on small screens, 2 across on medium, 4 across on large

**Colors (kumo tokens used):**

`border-kumo-hairline`

**Examples:**

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


---

### InlineCopyText

Compact, borderless copy control for IDs and other short values displayed inline or inside dense table cells.

**Type:** component

**Import:** `import { InlineCopyText } from "@cloudflare/kumo";`

**Category:** Action

**Props:**

- `labels`: InlineCopyTextLabels
  Accessible labels for localization.
- `children`: ReactNode
  Content to display. String children are copied unless `value` is provided.
- `value`: string
  The value to copy. Required when `children` is not a string.
- `size`: "xs" | "sm" | "base" | "lg"
  Text size. Supported values depend on the text variant.
- `bold`: boolean
  Whether to use medium font weight. Only applies to body text variants.
- `as`: enum
  The HTML element to render. Accepts headings (`"h1"`–`"h6"`), block text (`"p"`, `"pre"`), inline text (`"span"`, `"code"`, `"em"`, `"strong"`, `"small"`, `"abbr"`, `"time"`), form-related (`"label"`, `"legend"`), list/definition (`"dt"`, `"dd"`, `"li"`), and `"figcaption"`.

- **Optional** for `"heading"` (defaults to `"span"`). Pass the heading   element that reflects this text's place in the document outline.
- **Required** for deprecated heading variants (`"heading1"`,   `"heading2"`, `"heading3"`).
- **Optional** for body variants (defaults to `"p"`) and monospace   variants (defaults to `"span"`).
- `truncate`: boolean
  Whether to truncate overflowing text with an ellipsis. Adds `truncate min-w-0` classes.
- `disabled`: boolean
- `name`: string
- `type`: enum
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `variant`: "body" | "secondary" | "success" | "error" | "mono" | "mono-secondary" [default: "mono-secondary"]
  Text style variant. Supports every Text variant except heading variants.

**Colors (kumo tokens used):**

`ring-kumo-brand`, `text-kumo-default`

**Styling:**


**Examples:**

```tsx
<InlineCopyText
      labels={{ copyAction: "Copy database ID", copied: "Database ID copied" }}
    >
      f86b3f10-32e9-4db7-ae95-84a1b2c3d4e5
    </InlineCopyText>
```

```tsx
<div className="group flex w-full max-w-xl items-center justify-between gap-4 rounded-lg bg-kumo-base px-4 py-3 ring ring-kumo-line">
      <span className="min-w-0 truncate text-sm text-kumo-default">
        Production database
      </span>
      <InlineCopyText
        labels={{
          copyAction: "Copy database ID",
          copied: "Database ID copied",
        }}
      >
        f86b3f10-32e9-4db7-ae95-84a1b2c3d4e5
      </InlineCopyText>
    </div>
```

```tsx
<InlineCopyText
      value="f86b3f10-32e9-4db7-ae95-84a1b2c3d4e5"
      variant="body"
      labels={{ copyAction: "Copy database ID", copied: "Database ID copied" }}
    >
      <span>
        Database ID: <strong>f86b3f10…</strong>
      </span>
    </InlineCopyText>
```


---

### Input

Input component

**Type:** component

**Import:** `import { Input } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `label`: ReactNode
  Label content for the input (enables Field wrapper) - can be a string or any React node
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon
- `description`: ReactNode
  Helper text displayed below the input
- `error`: string | object
  Error message or validation error object
- `passwordManagerIgnore`: boolean
  Suppress browser extension password manager overlays on non-credential inputs.
- `size`: enum [default: base]
  - `"xs"`: Extra small input for compact UIs
  - `"sm"`: Small input for secondary fields
  - `"base"`: Default input size
  - `"lg"`: Large input for prominent fields
- `variant`: enum [default: default]
  - `"default"`: Default input appearance
  - `"error"`: Error state for validation failures

  **State Classes:**
  - `"default"`:
    - `focus`: `focus:ring-kumo-focus/50 focus:ring-[1.5px]`
  - `"error"`:
    - `focus`: `focus:ring-kumo-danger/50 focus:ring-[1.5px]`

**Colors (kumo tokens used):**

`bg-kumo-control`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-inactive`

**Styling:**

- **Dimensions:** `[object Object]`

**Examples:**

```tsx
<Input
      label="Email"
      placeholder="you@example.com"
      description="We'll never share your email"
    />
```

```tsx
<Input
      label="Email"
      placeholder="you@example.com"
      value="invalid-email"
      error="Please enter a valid email address"
    />
```

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

```tsx
<div className="flex flex-col gap-4">
      <Input size="xs" label="Extra Small" placeholder="Extra small input" />
      <Input size="sm" label="Small" placeholder="Small input" />
      <Input label="Base" placeholder="Base input (default)" />
      <Input size="lg" label="Large" placeholder="Large input" />
    </div>
```

```tsx
<Input label="Disabled field" placeholder="Cannot edit" disabled />
```

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

```tsx
<div className="flex flex-col gap-4">
      <Input
        label="API Key (default)"
        type="password"
        placeholder="sk_live_..."
      />
      <Input
        label="API Key (passwordManagerIgnore)"
        type="password"
        placeholder="sk_live_..."
        passwordManagerIgnore
      />
    </div>
```

```tsx
<div className="flex flex-col gap-4">
      <Input type="email" label="Email" placeholder="you@example.com" />
      <Input type="password" label="Password" placeholder="••••••••" />
      <Input type="number" label="Age" placeholder="18" />
      <Input type="tel" label="Phone" placeholder="+1 (555) 000-0000" />
    </div>
```

```tsx
<Input
      label="Phone Number"
      required={false}
      placeholder="+1 (555) 000-0000"
    />
```

```tsx
<Input
      label="API Key"
      labelTooltip="Find this in your dashboard under Settings > API Keys"
      placeholder="sk_live_..."
    />
```

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

```tsx
<Input
      label="With onChange"
      placeholder="Type something..."
      description={value ? `Value: ${value}` : "Uses e.target.value"}
      value={value}
      onChange={(e) => setValue(e.target.value)}
    />
```

```tsx
<Input
      label="With onValueChange"
      placeholder="Type something..."
      description={value ? `Value: ${value}` : "Receives the value directly"}
      value={value}
      onValueChange={(v) => setValue(v)}
    />
```


---

### InputGroup

InputGroup component

**Type:** component

**Import:** `import { InputGroup } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `label`: ReactNode
  The label content — can be a string or any React node.
- `description`: ReactNode
  Helper text displayed below the control (hidden when `error` is present).
- `error`: object
  Validation error with a message and a browser `ValidityState` match key.
- `required`: boolean
  When explicitly `false`, shows gray "(optional)" text after the label. When `true` or `undefined`, no indicator is shown.
- `labelTooltip`: ReactNode
  Tooltip content displayed next to the label via an info icon.
- `children`: ReactNode
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `size`: enum [default: base]
  - `"xs"`: Extra small size.
  - `"sm"`: Small size.
  - `"base"`: Default size.
  - `"lg"`: Large size.
- `disabled`: boolean

**Colors (kumo tokens used):**

`border-kumo-focus`, `border-kumo-line`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### InputGroup.Input

Input sub-component

#### InputGroup.Button

Button sub-component

Props:
- `tooltip`: ReactNode - When provided, wraps the button in a Tooltip. Automatically sets aria-label from a string value.
- `tooltipSide`: "top" | "right" | "bottom" | "left" [default: "bottom"] - Preferred side for the tooltip popup.
- `variant`: "primary" | "secondary" | "ghost" | "destructive" | "secondary-destructive" | "outline" [default: "ghost"] - Button visual style. Defaults to ghost.
- `size`: "xs" | "sm" | "base" | "lg" [default: "sm"] - Button size.

#### InputGroup.Addon

Addon sub-component

Props:
- `align`: "start" | "end" [default: "start"] - Position relative to the input.
- `className`: string - Additional CSS classes.

#### InputGroup.Suffix

Suffix sub-component

Props:
- `className`: string - Additional CSS classes.

#### InputGroup.Label

Label sub-component

#### InputGroup.Description

Description sub-component


**Examples:**

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

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Addon>
        <LinkIcon />
      </InputGroup.Addon>
      <InputGroup.Input placeholder="Paste a link..." aria-label="Link" />
    </InputGroup>
```

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

```tsx
<div className="flex flex-col gap-4">
      <InputGroup className="w-full max-w-3xs">
        <InputGroup.Input
          type={show ? "text" : "password"}
          defaultValue="password"
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

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Addon>
        <MagnifyingGlassIcon />
      </InputGroup.Addon>
      <InputGroup.Input placeholder="Search..." aria-label="Search" />
      <InputGroup.Addon align="end">
        <kbd className="border-none! bg-none!">⌘K</kbd>
      </InputGroup.Addon>
    </InputGroup>
```

```tsx
<InputGroup className="w-full max-w-3xs">
      <InputGroup.Input defaultValue="kumo" aria-label="kumo" />
      <InputGroup.Addon align="end">
        <Loader />
      </InputGroup.Addon>
    </InputGroup>
```

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


---

### Label

Label component for form fields.

**Type:** component

**Import:** `import { Label } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `children`: ReactNode
  The label content — can be a string or any React node.
- `showOptional`: boolean
  When `true`, shows gray "(optional)" text after the label.
- `optionalLabel`: ReactNode
  Content shown when `showOptional` is `true`.
- `tooltip`: ReactNode
  Tooltip content displayed next to the label via an info icon.
- `tooltipAriaLabel`: string
  Accessible label for the tooltip button.
- `className`: string
  Additional CSS classes merged via `cn()`.
- `htmlFor`: string
  The id of the form element this label is associated with
- `asContent`: boolean
  When true, only renders the inline content (indicators, tooltip) without the outer label element with font styling. Useful when composed inside another label element that already provides the text styling.

**Colors (kumo tokens used):**

`text-kumo-default`, `text-kumo-subtle`

**Examples:**

```tsx
<div className="flex flex-col gap-4">
      <Label>Default Label</Label>
      <Label showOptional>Optional Label</Label>
      <Label tooltip="More information about this field">
        Label with Tooltip
      </Label>
      <KumoLocaleProvider
        translations={{
          label: {
            optional: "(opcional)",
            tooltip: "Mais informações",
          },
        }}
      >
        <Label showOptional>Rótulo traduzido</Label>
        <Label tooltip="Mais informações sobre este campo">
          Rótulo com dica
        </Label>
      </KumoLocaleProvider>
    </div>
```

```tsx
<Input label="Phone Number" required={false} placeholder="+1 555-0000" />
```

```tsx
<Input
      label="API Key"
      labelTooltip="Find this in your dashboard settings under API > Keys"
      placeholder="sk_live_..."
    />
```

```tsx
<Checkbox
      label={
        <span>
          I agree to the <strong>Terms of Service</strong>
        </span>
      }
    />
```

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

```tsx
<div className="flex flex-col gap-3">
      <Label>Default</Label>
      <Label showOptional>Optional</Label>
      <Label tooltip="Important field">With Tooltip</Label>
    </div>
```


---

### LayerCard

LayerCard component

**Type:** component

**Import:** `import { LayerCard } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `render`: ReactNode
  Allows you to replace the component's HTML element with a different tag, or compose it with another component.

Accepts a `ReactElement` or a function that returns the element to render.
- `children`: ReactNode
- `className`: string
- `id`: string
- `lang`: string
- `title`: string

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-elevated`, `ring-kumo-fill`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-subtle`

**Styling:**


**Sub-Components:**

This is a compound component. Use these sub-components:

#### LayerCard.Primary

Primary sub-component

#### LayerCard.Secondary

Secondary sub-component


**Examples:**

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


---

### LayerDialog

LayerDialog component

**Type:** component

**Import:** `import { LayerDialog } from "@cloudflare/kumo";`

**Category:** Overlay

**Props:**

- `dismissDisabled`: boolean
  Prevent every user-initiated dismissal while dialog work is pending.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-elevated`, `bg-kumo-fill`, `bg-kumo-recessed`, `border-kumo-hairline`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### LayerDialog.Root

Root sub-component

#### LayerDialog.Alert

Alert sub-component

#### LayerDialog.Trigger

Trigger sub-component

#### LayerDialog.Content

Content sub-component

Props:
- `children`: ReactNode (required)
- `container`: PortalContainer - Container element for the portal. Overrides `KumoPortalProvider` context.
- `size`: KumoLayerDialogSize - Desktop-only width. Mobile dialogs always remain full-width.
- `verticalAlign`: KumoLayerDialogVerticalAlign - Desktop-only positioning. Mobile dialogs always remain bottom sheets.
- `closeLabel`: string - Accessible name of the automatic X button. Overrides the `close` translation from KumoLocaleProvider.

#### LayerDialog.Title

Title sub-component

Props:
- `children`: ReactNode (required)

#### LayerDialog.Description

Description sub-component

Props:
- `children`: ReactNode (required)

#### LayerDialog.Body

Body sub-component

Props:
- `children`: ReactNode (required)

#### LayerDialog.Action

Action sub-component

#### LayerDialog.Actions

Actions sub-component

Props:
- `children`: ReactNode (required) - Exactly one action. Pass `menu` to LayerDialog.Action for related actions such as "Save as draft".
- `dismissLabel`: string - Text of the automatic dismiss button. Say "Cancel" only when the workflow has a real cancel outcome. Translate it for non-English products.


**Examples:**

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Open keyboard shortcuts</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Keyboard shortcuts</LayerDialog.Title>
        <LayerDialog.Description>
          Browse available shortcuts without changing a setting.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <LongContent />
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Open settings</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Configure custom hostname</LayerDialog.Title>
        <LayerDialog.Description>
          Route requests for this hostname to your Worker.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-5">
            <Input
              label="Hostname"
              onChange={(event) => setHostname(event.target.value)}
              value={hostname}
            />
            <Input
              label="Display name"
              onChange={(event) => setName(event.target.value)}
              value={name}
            />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions>
          <LayerDialog.Action
            disabled={!hostname || !name}
            onClick={() => undefined}
          >
            Save hostname
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Create deployment</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Create deployment</LayerDialog.Title>
        <LayerDialog.Description>
          The browser owns the form’s native validation and submission state.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <form
            id={formId}
            className="flex flex-col gap-5"
            onSubmit={(event) => event.preventDefault()}
          >
            <Input
              label="Service name"
              name="serviceName"
              placeholder="production-api"
              required
            />
            <Input
              label="Compatibility date"
              name="compatibilityDate"
              required
              type="date"
            />
          </form>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="Cancel">
          <LayerDialog.Action form={formId} type="submit">
            Create deployment
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Choose a Worker</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Choose a Worker</LayerDialog.Title>
        <LayerDialog.Description>
          Select the Worker that handles requests for this route.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-3">
            <Input
              aria-label="Search Workers"
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search Workers"
              type="search"
              value={query}
            />
            {matches.length > 0 ? (
              <ul className="flex flex-col divide-y divide-kumo-hairline rounded-lg border border-kumo-hairline">
                {matches.map((worker) => (
                  <li key={worker} className="px-3 py-2 text-kumo-default">
                    {worker}
                  </li>
                ))}
              </ul>
            ) : (
              <Text variant="secondary">No Workers match “{query}”.</Text>
            )}
          </div>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<div className="flex flex-wrap justify-center gap-3">
      <LayerDialog.Root>
        <LayerDialog.Trigger
          render={(props) => <Button {...props}>Edit deployment</Button>}
        />
        <LayerDialog.Content>
          <LayerDialog.Title>Save deployment</LayerDialog.Title>
          <LayerDialog.Description>
            Deploy these changes now, or keep them as a draft.
          </LayerDialog.Description>
          <LayerDialog.Body>
            <Text variant="secondary">
              The alternate save outcome is related to the primary action, so it
              lives in its split-button menu rather than as an unrelated footer
              button.
            </Text>
          </LayerDialog.Body>
          <LayerDialog.Actions>
            <LayerDialog.Action
              menu={[
                <DropdownMenu.Item key="draft">
                  Save as draft
                </DropdownMenu.Item>,
              ]}
              menuLabel="Save options"
            >
              Save and deploy
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>

      <LayerDialog.Alert>
        <LayerDialog.Trigger
          render={(props) => (
            <Button {...props} variant="secondary">
              Delete deployment
            </Button>
          )}
        />
        <LayerDialog.Content>
          <LayerDialog.Title>Delete deployment</LayerDialog.Title>
          <LayerDialog.Description>
            This permanently removes the deployment and cannot be undone.
          </LayerDialog.Description>
          <LayerDialog.Body>
            <Text variant="secondary">
              The destructive split button keeps every part of the action,
              including the menu trigger, visually consistent.
            </Text>
          </LayerDialog.Body>
          <LayerDialog.Actions>
            <LayerDialog.Action
              menu={[
                <DropdownMenu.Item key="delete-with-tokens">
                  Delete and revoke tokens
                </DropdownMenu.Item>,
              ]}
              menuLabel="Delete options"
              variant="destructive"
            >
              Delete deployment
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Alert>
    </div>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Edit profile</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Edit profile</LayerDialog.Title>
        <LayerDialog.Description>
          Update the profile information shown to your teammates. Changes are
          not saved until you confirm.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-5">
            <Input
              label="Display name"
              onChange={(event) => setName(event.target.value)}
              value={name}
            />
            <Input
              label="Email address"
              onChange={(event) => setEmail(event.target.value)}
              type="email"
              value={email}
            />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="Cancel">
          <LayerDialog.Action
            disabled={!email || !name}
            onClick={() => undefined}
          >
            Save changes
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Alert>
      <LayerDialog.Trigger
        render={(props) => (
          <Button variant="secondary-destructive" {...props}>
            Delete Worker
          </Button>
        )}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Delete Worker</LayerDialog.Title>
        <LayerDialog.Description>
          Deleting{" "}
          <strong className="font-medium text-kumo-default">
            {workerName}
          </strong>{" "}
          is permanent.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-5">
            <Text variant="secondary">
              This deletes the Worker, deployments, and configuration. If this
              Worker consumes Queues, those connections are removed first.
              Queues, D1 databases, and messages stay in your account.
            </Text>
            <Input
              label={
                <>
                  Type{" "}
                  <strong className="font-medium text-kumo-default">
                    {workerName}
                  </strong>{" "}
                  to confirm
                </>
              }
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder={workerName}
              value={confirmation}
            />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions>
          <LayerDialog.Action
            disabled={confirmation !== workerName}
            onClick={() => undefined}
            variant="destructive"
          >
            Delete Worker
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Alert>
```

```tsx
<LayerDialog.Root dismissDisabled={pending}>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Save a setting</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Save a setting</LayerDialog.Title>
        <LayerDialog.Description>
          While saving, Close, Escape, backdrop, and mobile swipe dismissals are
          blocked together.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <Text variant="secondary">
            Programmatic closes still work, so a successful save can dismiss the
            dialog through `actionsRef` or a controlled `open` prop.
          </Text>
        </LayerDialog.Body>
        <LayerDialog.Actions>
          <LayerDialog.Action
            loading={pending}
            onClick={() => {
              setPending(true);
              window.setTimeout(() => setPending(false), 1500);
            }}
          >
            Save changes
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) setCleanupCount((count) => count + 1);
        setOpen(nextOpen);
      }}
    >
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Open draft</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Draft settings</LayerDialog.Title>
        <LayerDialog.Body>
          <Text variant="secondary">
            Cleanup has run {cleanupCount} time{cleanupCount === 1 ? "" : "s"}.
          </Text>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Edit deployment</Button>}
      />
      <LayerDialog.Content>
        <LayerDialog.Title>Edit deployment</LayerDialog.Title>
        <LayerDialog.Description>
          Review the deployment settings before saving.
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-4">
            <Text variant="secondary">
              Opening a second dialog from this body should keep the first
              dialog beneath it and restore focus when it closes.
            </Text>
            <LayerDialog.Alert>
              <LayerDialog.Trigger
                render={(props) => (
                  <Button variant="secondary-destructive" {...props}>
                    Discard changes
                  </Button>
                )}
              />
              <LayerDialog.Content size="sm">
                <LayerDialog.Title>Discard unsaved changes?</LayerDialog.Title>
                <LayerDialog.Description>
                  Your deployment edits will be permanently lost.
                </LayerDialog.Description>
                <LayerDialog.Body>
                  <Text variant="secondary">
                    This nested alert is independently portaled and should
                    dismiss back to the edit dialog.
                  </Text>
                </LayerDialog.Body>
                <LayerDialog.Actions>
                  <LayerDialog.Action variant="destructive">
                    Discard changes
                  </LayerDialog.Action>
                </LayerDialog.Actions>
              </LayerDialog.Content>
            </LayerDialog.Alert>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions>
          <LayerDialog.Action>Save changes</LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<LayerDialog.Root>
      <LayerDialog.Trigger
        render={(props) => <Button {...props}>Open top-aligned dialog</Button>}
      />
      <LayerDialog.Content verticalAlign="top">
        <LayerDialog.Title>Top-aligned dialog</LayerDialog.Title>
        <LayerDialog.Body>
          <Text variant="secondary">Mobile dialogs remain bottom sheets.</Text>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
```

```tsx
<>
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => openAt("center")}>Centered, tall content</Button>
        <Button onClick={() => openAt("top")}>Top-aligned, tall content</Button>
      </div>
      <LayerDialog.Root open={open} onOpenChange={setOpen}>
        <LayerDialog.Content verticalAlign={verticalAlign}>
          <LayerDialog.Title>Audit log</LayerDialog.Title>
          <LayerDialog.Description>
            The dialog grows with its content until it reaches the viewport cap,
            then only the body scrolls.
          </LayerDialog.Description>
          <LayerDialog.Body>
            <ol className="flex flex-col gap-2">
              {Array.from({ length: 40 }, (_, index) => (
                <li
                  key={index}
                  className="rounded-lg border border-kumo-line px-3 py-2 text-kumo-subtle"
                >
                  Entry {index + 1}
                </li>
              ))}
            </ol>
          </LayerDialog.Body>
          <LayerDialog.Actions>
            <LayerDialog.Action>Export log</LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </>
```

```tsx
<>
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => openAtSize("sm")}>Small</Button>
        <Button onClick={() => openAtSize("base")}>Default</Button>
        <Button onClick={() => openAtSize("lg")}>Large</Button>
        <Button onClick={() => openAtSize("xl")}>Extra large</Button>
      </div>
      <LayerDialog.Root open={open} onOpenChange={setOpen}>
        <LayerDialog.Content size={size}>
          <LayerDialog.Title>Review deployment configuration</LayerDialog.Title>
          <LayerDialog.Description>
            Confirm the service details and routing configuration before this
            deployment is created.
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="grid gap-5 sm:grid-cols-2">
              <Input label="Service name" defaultValue="production-api" />
              <Input label="Environment" defaultValue="Production" />
              <Input label="Hostname" defaultValue="api.example.com" />
              <Input label="Compatibility date" defaultValue="2026-09-09" />
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions>
            <LayerDialog.Action>Create deployment</LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </>
```


---

### Link

Link component

**Type:** component

**Import:** `import { Link } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `variant`: enum [default: inline]
  - `"inline"`: Inline text link that flows with content
  - `"current"`: Link that inherits color from parent text
  - `"plain"`: Link without underline decoration
- `to`: string
- `children`: ReactNode
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `download`: unknown
- `href`: string
- `hrefLang`: string
- `media`: string
- `ping`: string
- `target`: React.HTMLAttributeAnchorTarget
- `type`: string
- `referrerPolicy`: enum
- `render`: ReactNode
  Allows you to replace the component's HTML element with a different tag, or compose it with another component.

Accepts a `ReactElement` or a function that returns the element to render.

**Colors (kumo tokens used):**

`text-kumo-link`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Link.ExternalIcon

ExternalIcon sub-component


**Examples:**

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

```tsx
<p className="mx-auto max-w-md text-base leading-relaxed text-kumo-default">
      This is a paragraph with an <Link href="#">inline link</Link> that flows
      naturally with the surrounding text. Links maintain proper underline
      offset for readability.
    </p>
```

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

```tsx
<p className="text-base text-kumo-danger">
      This error message contains a{" "}
      <Link href="#" variant="current">
        link
      </Link>{" "}
      that inherits the red color from its parent.
    </p>
```

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


---

### Loader

Animated circular spinner for indicating loading states.

**Type:** component

**Import:** `import { Loader } from "@cloudflare/kumo";`

**Category:** Feedback

**Props:**

- `className`: string
  Additional CSS classes merged via `cn()`.
- `size`: enum [default: base]
  - `"sm"`: Small loader for inline use
  - `"base"`: Default loader size
  - `"lg"`: Large loader for prominent loading states

**Examples:**

```tsx
<div className="flex items-center gap-4">
      <Loader size="sm" />
      <Loader size="base" />
      <Loader size="lg" />
    </div>
```

```tsx
<Loader size={24} />
```

```tsx
<Loader className="text-kumo-subtle" />
```


---

### MenuBar

MenuBar — horizontal icon-button toolbar with keyboard arrow-key navigation.

**Type:** component

**Import:** `import { MenuBar } from "@cloudflare/kumo";`

**Category:** Navigation

**Props:**

- `className`: string
  Additional CSS classes merged via `cn()`.
- `isActive`: number | boolean | string
  The currently active option value — matched against option index or `id`.
- `options`: MenuOptionProps[] (required)
  Array of menu option configurations.
- `optionIds`: boolean
  When true, each option's `id` field is used for matching instead of its array index.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-recessed`, `border-kumo-recessed`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-line`

**Styling:**


---

### Meter

Progress bar showing a measured value within a known range (e.g. quota usage).

**Type:** component

**Import:** `import { Meter } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `customValue`: string
  Custom formatted value text (e.g. "750 / 1,000") displayed instead of percentage.
- `label`: string (required)
  Label text displayed above the meter track.
- `showValue`: boolean
  Whether to display the percentage value next to the label.
- `trackClassName`: string
  Additional CSS classes for the track (background bar).
- `indicatorClassName`: string
  Additional CSS classes for the indicator (filled bar).
- `value`: number
  Current value of the meter
- `max`: number
  Maximum value of the meter (default: 100)
- `min`: number
  Minimum value of the meter (default: 0)

**Colors (kumo tokens used):**

`bg-kumo-fill`, `text-kumo-default`, `text-kumo-subtle`

**Examples:**

```tsx
<Meter label="Storage used" value={65} />
```

```tsx
<Meter label="API requests" value={75} customValue="750 / 1,000" />
```

```tsx
<Meter label="Progress" value={40} showValue={false} />
```

```tsx
<Meter
      label="Upload progress"
      value={80}
      indicatorClassName="from-kumo-success via-kumo-success to-kumo-success"
    />
```


---

### Pagination

Pagination component

**Type:** component

**Import:** `import { Pagination } from "@cloudflare/kumo";`

**Category:** Navigation

**Props:**

- `setPage`: (page: number) => void (required)
  Callback when page changes
- `page`: number
  Current page number (1-indexed).
- `perPage`: number
  Number of items displayed per page.
- `totalCount`: number
  Total number of items across all pages.
- `hasNextPage`: boolean
  Whether another page exists when the total count is unknown. Ignored when `totalCount` is provided. Unknown totals use sequential controls only.
- `className`: string
  Additional CSS classes for the container
- `labels`: PaginationLabels
  Labels for internationalization of aria-labels. All labels have English defaults.

For visible text like "Showing X of Y", use render props on sub-components:
- `Pagination.Info` children for the info text
- `Pagination.PageSize` label prop for the "Per page:" text
- `children`: ReactNode
  Compound component children for custom layouts. Use Pagination.Info, Pagination.PageSize, Pagination.Controls, and Pagination.Separator.
- `controls`: enum [default: full]
  - `"full"`: Full pagination controls with first, previous, page input, next, and last buttons
  - `"simple"`: Simple pagination controls with only previous and next buttons
- `text`: object

**Colors (kumo tokens used):**

`border-kumo-hairline`, `ring-kumo-hairline`, `text-kumo-subtle`

**Styling:**


**Sub-Components:**

This is a compound component. Use these sub-components:

#### Pagination.Info

Info sub-component

Props:
- `children`: (props: {
    page: number;
    perPage?: number;
    totalCount?: number;
    pageShowingRange: string;
  }) => ReactNode - Custom render function for the info text
- `className`: string - Additional CSS classes

#### Pagination.PageSize

PageSize sub-component

Props:
- `value`: number (required) - Current page size value
- `options`: number[] - Available page size options
- `label`: ReactNode - Label text shown before the selector.
- `className`: string - Additional CSS classes

#### Pagination.Controls

Controls sub-component

Props:
- `pageSelector`: "input" | "dropdown" - How the page number selector is rendered in "full" controls mode. - `"input"` (default): A text input where users type a page number. - `"dropdown"`: A dropdown select with all page numbers as options. **Note:** `"dropdown"` renders an option for every page, so it is best suited for small page counts. For large datasets (hundreds of pages or more) prefer `"input"` to avoid rendering performance overhead.
- `className`: string - Additional CSS classes

#### Pagination.Separator

Separator sub-component

Props:
- `className`: string - Additional CSS classes


**Examples:**

```tsx
<Pagination page={page} setPage={setPage} perPage={10} totalCount={100} />
```

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={10}
      totalCount={100}
      controls="simple"
    />
```

```tsx
<Pagination
      text={() => `Page ${page}`}
      page={page}
      setPage={setPage}
      hasNextPage={hasNextPage}
    />
```

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

```tsx
<Pagination
      page={page}
      setPage={setPage}
      perPage={10}
      totalCount={100}
      labels={{
        firstPage: "Première page",
        previousPage: "Page précédente",
        nextPage: "Page suivante",
        lastPage: "Dernière page",
        pageNumber: "Numéro de page",
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


---

### Popover

Popover component for displaying accessible popup content anchored to a trigger.

**Type:** component

**Import:** `import { Popover } from "@cloudflare/kumo";`

**Category:** Overlay

**Props:**

- `side`: enum [default: bottom]
  - `"top"`: Popover appears above the trigger
  - `"bottom"`: Popover appears below the trigger
  - `"left"`: Popover appears to the left of the trigger
  - `"right"`: Popover appears to the right of the trigger

**Colors (kumo tokens used):**

`bg-kumo-base`, `fill-kumo-arrow-edge`, `fill-kumo-arrow-stroke`, `fill-kumo-base`, `outline-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Popover.Root

Root sub-component

#### Popover.Trigger

Trigger sub-component

#### Popover.Portal

Portal sub-component

#### Popover.Backdrop

Backdrop sub-component

#### Popover.Positioner

Positioner sub-component

#### Popover.Popup

Popup sub-component

#### Popover.Arrow

Arrow sub-component

#### Popover.Viewport

Viewport sub-component

#### Popover.Content

Content sub-component

#### Popover.Title

Title sub-component

#### Popover.Description

Description sub-component

#### Popover.Close

Close sub-component


**Examples:**

```tsx
<Popover.Root>
      <Popover.Trigger
        render={
          <Button shape="square" icon={BellIcon} aria-label="Notifications" />
        }
      />
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup>
            <Popover.Arrow />
            <Popover.Title>Notifications</Popover.Title>
            <Popover.Description>
              You are all caught up. Good job!
            </Popover.Description>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<Popover.Root>
      <Popover.Trigger render={<Button />}>Open Popover</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup>
            <Popover.Arrow />
            <Popover.Title>Popover Title</Popover.Title>
            <Popover.Description>
              This is a basic popover with a title and description.
            </Popover.Description>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<Popover>
      <Popover.Trigger render={<Button variant="secondary" />}>
        Open legacy Content
      </Popover.Trigger>
      <Popover.Content className="w-72">
        <Popover.Title>Compatibility mode</Popover.Title>
        <Popover.Description>
          This legacy wrapper fades in without scaling or transforming.
        </Popover.Description>
      </Popover.Content>
    </Popover>
```

```tsx
<Popover.Root>
      <Popover.Trigger render={<Button variant="secondary" />}>
        Open scrollable Popover
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup className="w-72">
            <Popover.Arrow />
            <Popover.Title>Recent notifications</Popover.Title>
            <Popover.Description>
              The list scrolls while the heading and arrow stay in place.
            </Popover.Description>
            <div
              aria-label="Recent notifications"
              className="mt-3 max-h-40 overflow-y-auto rounded-md border border-kumo-hairline outline-none focus-visible:ring-2 focus-visible:ring-kumo-brand"
              tabIndex={0}
            >
              {Array.from({ length: 10 }, (_, index) => (
                <div
                  key={index}
                  className="border-b border-kumo-hairline px-3 py-2 last:border-b-0"
                >
                  Notification {index + 1}
                </div>
              ))}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<Popover.Root>
      <Popover.Trigger render={<Button />}>Open tall Popover</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup className="h-80 max-h-[var(--available-height)] w-72">
            <Popover.Arrow />
            <Popover.Title>Available-height popup</Popover.Title>
            <Popover.Description>
              The popup is 320px tall unless the viewport provides less space.
            </Popover.Description>
            <div
              aria-label="Popover rows"
              className="mt-3 min-h-0 flex-1 overflow-y-auto rounded-md border border-kumo-hairline outline-none focus-visible:ring-2 focus-visible:ring-kumo-brand"
              tabIndex={0}
            >
              {Array.from({ length: 12 }, (_, index) => (
                <div
                  key={index}
                  className="border-b border-kumo-hairline px-3 py-2 last:border-b-0"
                >
                  Row {index + 1}
                </div>
              ))}
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<Popover.Root>
      <Popover.Trigger render={<Button />}>Open Settings</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup>
            <Popover.Arrow />
            <Popover.Title>Settings</Popover.Title>
            <Popover.Description>
              Configure your preferences below.
            </Popover.Description>
            <div className="mt-3">
              <Popover.Close render={<Button variant="secondary" size="sm" />}>
                Close
              </Popover.Close>
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<div className="flex flex-wrap gap-4">
      <Popover.Root>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Bottom
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Positioner side="bottom">
            <Popover.Popup>
              <Popover.Arrow />
              <Popover.Title>Bottom</Popover.Title>
              <Popover.Description>
                Popover on bottom (default).
              </Popover.Description>
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>

      <Popover.Root>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Top
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Positioner side="top">
            <Popover.Popup>
              <Popover.Arrow />
              <Popover.Title>Top</Popover.Title>
              <Popover.Description>Popover on top.</Popover.Description>
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>

      <Popover.Root>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Left
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Positioner side="left">
            <Popover.Popup>
              <Popover.Arrow />
              <Popover.Title>Left</Popover.Title>
              <Popover.Description>Popover on left.</Popover.Description>
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>

      <Popover.Root>
        <Popover.Trigger render={<Button variant="secondary" />}>
          Right
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Positioner side="right">
            <Popover.Popup>
              <Popover.Arrow />
              <Popover.Title>Right</Popover.Title>
              <Popover.Description>Popover on right.</Popover.Description>
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </div>
```

```tsx
<Popover.Root>
      <Popover.Trigger render={<Button />}>User Profile</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup className="w-64">
            <Popover.Arrow />
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
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

```tsx
<Popover.Root>
      <Popover.Trigger
        openOnHover
        delay={200}
        render={<Button variant="secondary" />}
      >
        Hover Me
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup>
            <Popover.Arrow />
            <Popover.Title>Hover Triggered</Popover.Title>
            <Popover.Description>
              This popover opens on hover with a 200ms delay. It can still
              contain interactive content like buttons and links.
            </Popover.Description>
            <div className="mt-3">
              <Popover.Close render={<Button variant="secondary" size="sm" />}>
                Got it
              </Popover.Close>
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
```

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
      <Popover.Root
        open={!!selectedRow}
        onOpenChange={(open) => !open && setSelectedRow(null)}
      >
        <Popover.Portal>
          <Popover.Positioner
            side="left"
            anchor={
              anchorRect
                ? { getBoundingClientRect: () => anchorRect }
                : undefined
            }
          >
            <Popover.Popup>
              <Popover.Arrow />
              <Popover.Title>
                Edit {rows.find((r) => r.id === selectedRow)?.name}
              </Popover.Title>
              <Popover.Description>
                The popover anchors to the selected row, not the icon button.
              </Popover.Description>
              <div className="mt-3">
                <Popover.Close
                  render={<Button size="sm" variant="secondary" />}
                >
                  Close
                </Popover.Close>
              </div>
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </div>
```


---

### Radio

Radio — radio button group for single-select choices.

**Type:** component

**Import:** `import { Radio } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `variant`: enum [default: default]
  - `"default"`: Default radio appearance
  - `"error"`: Error state for validation failures
- `appearance`: KumoRadioAppearance
  Visual appearance applied to all Radio.Item children. - `"default"` — Standard inline radio items - `"card"` — Choice card with border, padding, and highlighted selection state - `"segmented"` — Compact single-line group for short options Individual items can override default and card appearances. Segmented is group-only and cannot be overridden by individual items.
- `legend`: string
  Legend text for the group (required for accessibility). For more control over legend styling, omit this prop and use `<Radio.Legend>` as a child instead.
- `children`: ReactNode (required)
  Child Radio.Item components (and optionally a Radio.Legend)
- `orientation`: "vertical" | "horizontal"
  Layout direction of the radio items
- `error`: string
  Error message for the group
- `description`: ReactNode
  Helper text for the group
- `value`: T
  The controlled value of the selected radio item.
- `disabled`: boolean
  Whether all radios in the group are disabled
- `controlPosition`: RadioControlPosition
  Position of radio control relative to label: "start" puts radio before label, "end" puts label before radio. Defaults to "start" for default appearance and "end" for card appearance.
- `name`: string
  Form submission name for the radio group
- `className`: string
  Additional CSS classes
- `defaultValue`: T
  The uncontrolled initial value of the selected radio item.
- `onValueChange`: (value: T, eventDetails: RadioGroupChangeEventDetails) => void
  Callback fired when the selected value changes. The second argument carries native event details about the interaction.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-contrast`, `bg-kumo-control`, `bg-kumo-elevated`, `bg-kumo-tint`, `border-kumo-danger`, `border-kumo-hairline`, `border-kumo-interact`, `border-kumo-line`, `outline-kumo-brand`, `ring-kumo-brand`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Radio.Item

Item sub-component

#### Radio.Group

Group sub-component

Props:
- `legend`: string - Legend text for the group (required for accessibility). For more control over legend styling, omit this prop and use `<Radio.Legend>` as a child instead.
- `children`: ReactNode (required) - Child Radio.Item components (and optionally a Radio.Legend)
- `orientation`: "vertical" | "horizontal" - Layout direction of the radio items
- `appearance`: KumoRadioAppearance - Visual appearance applied to all Radio.Item children. - `"default"` — Standard inline radio items - `"card"` — Choice card with border, padding, and highlighted selection state - `"segmented"` — Compact single-line group for short options Individual items can override default and card appearances. Segmented is group-only and cannot be overridden by individual items.
- `error`: string - Error message for the group
- `description`: ReactNode - Helper text for the group
- `value`: T - The controlled value of the selected radio item.
- `disabled`: boolean - Whether all radios in the group are disabled
- `controlPosition`: RadioControlPosition - Position of radio control relative to label: "start" puts radio before label, "end" puts label before radio. Defaults to "start" for default appearance and "end" for card appearance.
- `name`: string - Form submission name for the radio group
- `className`: string - Additional CSS classes
- `defaultValue`: T - The uncontrolled initial value of the selected radio item.
- `onValueChange`: (value: T, eventDetails: RadioGroupChangeEventDetails) => void - Callback fired when the selected value changes. The second argument carries native event details about the interaction.

#### Radio.Legend

Legend sub-component

Props:
- `children`: ReactNode (required) - Legend content
- `className`: string - Additional CSS classes (e.g. "sr-only" to visually hide the legend)


**Examples:**

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

```tsx
<Radio.Group
      appearance="segmented"
      legend="Duration preset"
      value={duration}
      onValueChange={setDuration}
    >
      <Radio.Item label="1h" value="1" />
      <Radio.Item label="12h" value="12" />
      <Radio.Item label="24h" value="24" />
    </Radio.Group>
```

```tsx
<div className="flex flex-wrap gap-8">
      <Radio.Group
        legend="Default appearance"
        orientation="horizontal"
        defaultValue="1"
      >
        <Radio.Item label="1h" value="1" />
        <Radio.Item label="12h" value="12" />
        <Radio.Item label="24h" value="24" />
      </Radio.Group>
      <Radio.Group
        appearance="segmented"
        legend="Segmented appearance"
        defaultValue="1"
      >
        <Radio.Item label="1h" value="1" />
        <Radio.Item label="12h" value="12" />
        <Radio.Item label="24h" value="24" />
      </Radio.Group>
    </div>
```

```tsx
<div className="flex flex-wrap gap-8">
      <Radio.Group
        appearance="segmented"
        legend="Disabled group"
        defaultValue="12"
        disabled
      >
        <Radio.Item label="1h" value="1" />
        <Radio.Item label="12h" value="12" />
        <Radio.Item label="24h" value="24" />
      </Radio.Group>
      <Radio.Group
        appearance="segmented"
        legend="Disabled item"
        defaultValue="1"
      >
        <Radio.Item label="1h" value="1" />
        <Radio.Item label="12h" value="12" disabled />
        <Radio.Item label="24h" value="24" />
      </Radio.Group>
    </div>
```

```tsx
<Radio.Group appearance="segmented" defaultValue="automatic">
      <Radio.Legend className="text-sm font-normal text-kumo-subtle">
        Configuration mode
      </Radio.Legend>
      <Radio.Item label="Automatic" value="automatic" />
      <Radio.Item label="Manual" value="manual" />
    </Radio.Group>
```

```tsx
<div className="w-48 max-w-full">
      <Radio.Group
        appearance="segmented"
        legend="Processing preference"
        defaultValue="automatic"
      >
        <Radio.Item label="Automatic processing" value="automatic" />
        <Radio.Item label="Manual configuration" value="manual" />
      </Radio.Group>
    </div>
```

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

```tsx
<Radio.Group legend="Preferences" controlPosition="end" defaultValue="a">
      <Radio.Item label="Label before radio" value="a" />
      <Radio.Item label="Another option" value="b" />
    </Radio.Group>
```

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

```tsx
<Radio.Group defaultValue="all" value={value} onValueChange={setValue}>
      <Radio.Legend className="sr-only">Paths</Radio.Legend>
      <Radio.Item label="Allow all paths" value="all" />
      <Radio.Item label="Restrict to specific paths" value="specific" />
    </Radio.Group>
```

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

```tsx
<div className="grid grid-cols-2 gap-6">
      <Radio.Group<number>
        legend="Items per page"
        value={pageSize}
        onValueChange={setPageSize}
      >
        <Radio.Item<number> label="10" value={10} />
        <Radio.Item<number> label="25" value={25} />
        <Radio.Item<number> label="50" value={50} />
      </Radio.Group>
      <Radio.Group<ThemeType>
        legend="Theme"
        value={theme}
        onValueChange={setTheme}
      >
        <Radio.Item<ThemeType> label="Light" value={ThemeType.light} />
        <Radio.Item<ThemeType> label="Dark" value={ThemeType.dark} />
        <Radio.Item<ThemeType> label="System" value={ThemeType.system} />
      </Radio.Group>
    </div>
```

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


---

### SankeyChart

SankeyChart component

**Type:** component

**Import:** `import { SankeyChart } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `echarts`: typeof echarts (required)
  The ECharts core instance imported by the consumer. Passed in rather than imported directly so the consumer controls which ECharts modules are bundled (tree-shaking).
- `nodes`: SankeyNodeData[] (required)
  Array of nodes in the Sankey diagram
- `links`: SankeyLinkData[] (required)
  Array of links connecting nodes by index
- `height`: number
  Height of the chart in pixels
- `showNodeValues`: boolean
  Show node values above labels (default: true if any node has a value)
- `nodeLabelLayout`: "stacked" | "inline"
  Layout for node labels when showNodeValues is true. - 'stacked': value on top, name below (default) - 'inline': "value name" on a single line (better for small nodes)
- `formatValue`: (value: number) => string
  Format function for node values (default: toLocaleString)
- `tooltipFormatter`: (params: SankeyTooltipParams) => string
  Custom tooltip formatter. Return HTML string or empty string to hide tooltip.
- `nodeWidth`: number
- `nodePadding`: number
- `showTooltip`: boolean
- `defaultNodeColor`: string
- `left`: number | string
  Left padding of the Sankey layout within the chart container. Accepts a number (px) or percentage string. ECharts default: '5%'.
- `right`: number | string
  Right padding of the Sankey layout within the chart container. Accepts a number (px) or percentage string. ECharts default: '5%'.
- `linkColor`: "gradient" | "gray"
  Link fill style: 'gradient' blends source to target colors, 'gray' uses flat gray
- `linkOpacity`: number
- `className`: string
- `isDarkMode`: boolean

**Examples:**

```tsx
<SankeyChart
      echarts={echarts}
      nodes={basicNodes}
      links={basicLinks}
      height={350}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={previewNodes}
      links={previewLinks}
      height={200}
      showTooltip={false}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={multiLevelNodes}
      links={multiLevelLinks}
      height={350}
      nodeWidth={20}
      nodePadding={15}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={nodes}
      links={links}
      height={250}
      linkOpacity={0.6}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={nodes}
      links={links}
      height={300}
      tooltipFormatter={customTooltip}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={basicNodes}
      links={basicLinks}
      height={350}
      left={0}
      right={0}
      isDarkMode={isDarkMode}
    />
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={basicNodes}
      links={basicLinks}
      height={350}
      onNodeClick={handleNodeClick}
      onLinkClick={handleLinkClick}
      isDarkMode={isDarkMode}
    />
```

```tsx
<div className="flex w-full flex-col gap-2">
      <div className="flex items-center gap-2 text-sm text-kumo-subtle">
        <span>
          {hasSelection
            ? `Showing: ${[selectedSource, selectedTarget].filter(Boolean).join(" → ")}`
            : "Click a node to filter"}
        </span>
        {hasSelection && (
          <button
            onClick={resetFilters}
            className="text-kumo-brand hover:underline"
          >
            Reset
          </button>
        )}
      </div>
      <SankeyChart
        echarts={echarts}
        nodes={filteredNodes}
        links={filteredLinks}
        height={300}
        onNodeClick={handleNodeClick}
        isDarkMode={isDarkMode}
      />
    </div>
```

```tsx
<SankeyChart
      echarts={echarts}
      nodes={nodes}
      links={links}
      height={300}
      nodeLabelLayout="inline"
      isDarkMode={isDarkMode}
    />
```


---

### Select

Select component

**Type:** component

**Import:** `import { Select } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `align`: enum
  How to align the popup relative to the specified side.
- `alignItemWithTrigger`: boolean
  Whether the positioner overlaps the trigger so the selected item's text is aligned with the trigger's value text. This only applies to mouse input and is automatically disabled if there is not enough space.
- `alignOffset`: number | OffsetFunction
  Additional offset along the alignment axis in pixels. Also accepts a function that returns the offset to read the dimensions of the anchor and positioner elements, along with its side and alignment.

The function takes a `data` object parameter with the following properties:
- `data.anchor`: the dimensions of the anchor element with properties `width` and `height`.
- `data.positioner`: the dimensions of the positioner element with properties `width` and `height`.
- `data.side`: which side of the anchor element the positioner is aligned against.
- `data.align`: how the positioner is aligned relative to the specified side.
- `anchor`: ReactNode
  An element to position the popup against. By default, the popup will be positioned against the trigger.
- `arrowPadding`: number
  Minimum distance to maintain between the arrow and the edges of the popup.

Use it to prevent the arrow element from hanging out of the rounded corners of a popup.
- `collisionAvoidance`: CollisionAvoidance
  Determines how to handle collisions when positioning the popup.

`side` controls overflow on the preferred placement axis (`top`/`bottom` or `left`/`right`):
- `'flip'`: keep the requested side when it fits; otherwise try the opposite side   (`top` and `bottom`, or `left` and `right`).
- `'shift'`: never change side; keep the requested side and move the popup within   the clipping boundary so it stays visible.
- `'none'`: do not correct side-axis overflow.

`align` controls overflow on the alignment axis (`start`/`center`/`end`):
- `'flip'`: keep side, but swap `start` and `end` when the requested alignment overflows.
- `'shift'`: keep side and requested alignment, then nudge the popup along the   alignment axis to fit.
- `'none'`: do not correct alignment-axis overflow.

`fallbackAxisSide` controls fallback behavior on the perpendicular axis when the preferred axis cannot fit:
- `'start'`: allow perpendicular fallback and try the logical start side first   (`top` before `bottom`, or `left` before `right` in LTR).
- `'end'`: allow perpendicular fallback and try the logical end side first   (`bottom` before `top`, or `right` before `left` in LTR).
- `'none'`: do not fallback to the perpendicular axis.

When `side` is `'shift'`, explicitly setting `align` only supports `'shift'` or `'none'`. If `align` is omitted, it defaults to `'flip'`.
- `collisionBoundary`: Boundary
  An element or a rectangle that delimits the area that the popup is confined to.
- `collisionPadding`: Padding
  Additional space to maintain from the edge of the collision boundary.
- `disableAnchorTracking`: boolean
  Whether to disable the popup from tracking any layout shift of its positioning anchor.
- `positionMethod`: enum
  Determines which CSS `position` property to use.
- `side`: enum
  Which side of the anchor element to align the popup against. May automatically change to avoid collisions.
- `sideOffset`: number | OffsetFunction
  Distance between the anchor and the popup in pixels. Also accepts a function that returns the distance to read the dimensions of the anchor and positioner elements, along with its side and alignment.

The function takes a `data` object parameter with the following properties:
- `data.anchor`: the dimensions of the anchor element with properties `width` and `height`.
- `data.positioner`: the dimensions of the positioner element with properties `width` and `height`.
- `data.side`: which side of the anchor element the positioner is aligned against.
- `data.align`: how the positioner is aligned relative to the specified side.
- `sticky`: boolean
  Whether to maintain the popup in the viewport after the anchor element was scrolled out of view.
- `className`: string
  Additional CSS classes merged via `cn()`.
- `render`: ReactNode
  Replaces the trigger element while preserving Select behavior.
- `size`: enum [default: base]
  Size of the select trigger. Matches Input component sizes.
- `label`: ReactNode
  Label content for the select. When provided, enables the Field wrapper with a visible label above the select. For accessibility without a visible label, use `aria-label` instead.
- `hideLabel`: boolean
- `placeholder`: string
  Placeholder text shown when no value is selected.
- `loading`: boolean
  When `true`, shows a skeleton loader in place of the selected value.
- `disabled`: boolean
  Whether the select is disabled.
- `required`: boolean
  Whether the select is required. When `false`, shows "(optional)" text.
- `labelTooltip`: ReactNode
  Tooltip content displayed next to the label via an info icon.
- `value`: T
  Currently selected value (controlled mode).
- `children`: ReactNode
  `Select.Option` elements to render in the dropdown.
- `description`: ReactNode
  Helper text displayed below the select.
- `error`: string | object
  Error message string or validation error object with `match` key.
- `onValueChange`: (value: T) => void
  Callback when selection changes
- `defaultValue`: T
  Initial value for uncontrolled mode
- `renderValue`: (value: T) => ReactNode
  A function that returns a ReactNode to format the selected value in the trigger. Required when using object values. Use `placeholder` for the empty state.
- `items`: Record<string, string> | Array<{ label: ReactNode; value: T }>
  Data structure of items rendered in the popup. Accepts a plain object map (`{ key: "Label" }`) or an array of `{ label, value }` for object/complex values.
- `isItemEqualToValue`: (item: T, value: T) => boolean
  Custom equality function for comparing items. Required when value is an object, since object identity (`===`) won't match across renders.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-control`, `bg-kumo-hairline`, `bg-kumo-tint`, `border-kumo-line`, `ring-kumo-brand`, `ring-kumo-danger`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-placeholder`, `text-kumo-subtle`

**Styling:**


**Sub-Components:**

This is a compound component. Use these sub-components:

#### Select.Option

Option sub-component

#### Select.Group

Group sub-component

#### Select.GroupLabel

GroupLabel sub-component

#### Select.Separator

Separator sub-component


**Examples:**

```tsx
<Select
      label="Favorite Fruit"
      className="w-[200px]"
      value={value}
      onValueChange={(v) => setValue(v ?? "apple")}
      items={{ apple: "Apple", banana: "Banana", cherry: "Cherry" }}
    />
```

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

```tsx
<Select aria-label="Loading select" className="w-[200px]" loading />
```

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

```tsx
<div className="grid gap-4 sm:grid-cols-2">
      <Select
        label="side=bottom (default)"
        className="w-[200px]"
        defaultValue="earth"
        items={planets}
      />
      <Select
        label="side=top"
        side="top"
        className="w-[200px]"
        defaultValue="earth"
        items={planets}
      />
      <Select
        label="align=end"
        align="end"
        className="w-[200px]"
        defaultValue="earth"
        items={planets}
      />
      <Select
        label="sideOffset=12"
        sideOffset={12}
        className="w-[200px]"
        defaultValue="earth"
        items={planets}
      />
    </div>
```

```tsx
<div className="flex flex-wrap items-start gap-8">
      <Select
        label="Anchored (default)"
        description="Opens below the trigger"
        className="w-[200px]"
        defaultValue="mars"
        items={planets}
      />
      <Select
        label="Aligned to selection"
        description="Selected option lands on the trigger"
        alignItemWithTrigger
        className="w-[200px]"
        defaultValue="mars"
        items={planets}
      />
    </div>
```


---

### SensitiveInput

Password/secret input that masks its value by default and reveals on click. Includes a built-in copy-to-clipboard button on hover.

**Type:** component

**Import:** `import { SensitiveInput } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `alt`: string
- `autoComplete`: React.HTMLInputAutoCompleteAttribute
- `checked`: boolean
- `disabled`: boolean
- `height`: number | string
- `list`: string
- `name`: string
- `placeholder`: string
- `readOnly`: boolean
- `required`: boolean
- `width`: number | string
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `children`: ReactNode
- `value`: string
  Controlled value
- `size`: enum [default: base]
  Size of the input.
- `"xs"` — Extra small for compact UIs
- `"sm"` — Small for secondary fields
- `"base"` — Default input size
- `"lg"` — Large for prominent fields
- `variant`: enum [default: default]
  Style variant of the input.
- `"default"` — Default input appearance
- `"error"` — Error state for validation failures
- `label`: ReactNode
  Label content for the input (enables Field wrapper and sets masked state label) - can be a string or any React node
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon
- `description`: ReactNode
  Helper text displayed below the input
- `error`: string | object
  Error message or validation error object

**Colors (kumo tokens used):**

`bg-kumo-brand`, `bg-kumo-control`, `outline-kumo-focus`, `ring-kumo-brand`, `ring-kumo-focus`, `text-kumo-default`, `text-kumo-subtle`

**Examples:**

```tsx
<div className="w-80">
      <SensitiveInput label="API Key" defaultValue="sk_live_abc123xyz789" />
    </div>
```

```tsx
<div className="flex flex-col gap-4">
      {sizes.map((size) => (
        <div key={size} className="flex items-center gap-2">
          <span className="w-12 text-sm text-kumo-subtle">{size}</span>
          <SensitiveInput
            label={`${size} size`}
            size={size}
            defaultValue="secret-api-key-123"
          />
        </div>
      ))}
    </div>
```

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
          onClick={() => setValue("new-secret-" + Date.now())}
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
        defaultValue="view-only-secret-key"
        readOnly
      />
      <SensitiveInput
        label="With Description"
        defaultValue="my-secret-value"
        description="Keep this value secure and don't share it"
      />
    </div>
```


---

### Sidebar

Sidebar — responsive navigation panel with expand/collapse support.

**Type:** component

**Import:** `import { Sidebar } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `defaultOpen`: boolean
  Initial open state when uncontrolled.
- `open`: boolean
  Controlled open state.
- `variant`: enum [default: sidebar]
  - `"sidebar"`: Standard sidebar with border separator
  - `"floating"`: Floating sidebar with shadow and rounded corners
  - `"inset"`: Inset sidebar within the content area
- `side`: enum [default: left]
  - `"left"`: Left-aligned sidebar
  - `"right"`: Right-aligned sidebar
- `collapsible`: enum [default: icon]
  - `"icon"`: Collapses to show icons only
  - `"offcanvas"`: Slides off screen when collapsed
  - `"none"`: Cannot be collapsed
- `resizable`: boolean
  Enable drag-to-resize on the sidebar edge.
- `defaultWidth`: number
  Initial width in pixels when resizable.
- `minWidth`: number
  Minimum width in pixels when resizing.
- `maxWidth`: number
  Maximum width in pixels when resizing.
- `contained`: boolean
  When true, the collapsed sidebar uses absolute positioning instead of fixed, keeping it scoped inside a bounded parent. Useful for demos and embedded sidebars.
- `peekable`: boolean
  When true, hovering or focusing the collapsed sidebar temporarily expands it. The `state` will be `"peeking"` during the peek. Moving away collapses it back.
- `animationDuration`: number
  Duration of sidebar expand/collapse animation in milliseconds.
- `mobileBreakpoint`: number
  Viewport width (in px) below which the sidebar renders as a mobile dialog sheet instead of the desktop aside rail.
- `children`: ReactNode
  Content — typically `<Sidebar>` + main content.
- `className`: string
  Additional CSS classes for the wrapper div.

**Colors (kumo tokens used):**

`bg-kumo-brand`, `bg-kumo-hairline`, `bg-kumo-line`, `bg-kumo-recessed`, `border-kumo-line`, `ring-kumo-brand`, `text-kumo-default`, `text-kumo-strong`, `text-kumo-subtle`

**Styling:**


**Sub-Components:**

This is a compound component. Use these sub-components:

#### Sidebar.Provider

Provider sub-component

Props:
- `defaultOpen`: boolean - Initial open state when uncontrolled.
- `open`: boolean - Controlled open state.
- `variant`: SidebarVariant - Sidebar layout variant.
- `side`: SidebarSide - Which side the sidebar is on.
- `collapsible`: "icon" | "offcanvas" | "none"
- `resizable`: boolean - Enable drag-to-resize on the sidebar edge.
- `defaultWidth`: number - Initial width in pixels when resizable.
- `minWidth`: number - Minimum width in pixels when resizing.
- `maxWidth`: number - Maximum width in pixels when resizing.
- `contained`: boolean - When true, the collapsed sidebar uses absolute positioning instead of fixed, keeping it scoped inside a bounded parent. Useful for demos and embedded sidebars.
- `peekable`: boolean - When true, hovering or focusing the collapsed sidebar temporarily expands it. The `state` will be `"peeking"` during the peek. Moving away collapses it back.
- `animationDuration`: number - Duration of sidebar expand/collapse animation in milliseconds.
- `mobileBreakpoint`: number - Viewport width (in px) below which the sidebar renders as a mobile dialog sheet instead of the desktop aside rail.
- `children`: ReactNode (required) - Content — typically `<Sidebar>` + main content.
- `className`: string - Additional CSS classes for the wrapper div.

#### Sidebar.Header

Header sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.Content

Content sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.Footer

Footer sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.Loading

Loading sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.Group

Group sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.GroupLabel

GroupLabel sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.Menu

Menu sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.MenuItem

MenuItem sub-component

Props:
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.

#### Sidebar.MenuButton

MenuButton sub-component

Props:
- `icon`: React.ComponentType<{ className?: string }> | React.ReactNode
- `active`: boolean
- `size`: SidebarMenuButtonSize - Button size. - `"base"` — Standard nav item - `"sm"` — Compact nav item
- `href`: string
- `target`: React.HTMLAttributeAnchorTarget - Link target — only meaningful when `href` is provided.
- `tooltip`: string
- `itemId`: string - Anchor id for `useSidebar().scrollToItem(id)`.
- `className`: string
- `children`: ReactNode

#### Sidebar.MenuBadge

MenuBadge sub-component

Props:
- `active`: boolean - Marks this sub-item as currently active/selected.
- `href`: string - Navigation URL. When set, renders as a link via LinkProvider.
- `target`: React.HTMLAttributeAnchorTarget - Link target — only meaningful when `href` is provided.

#### Sidebar.MenuSub

MenuSub sub-component

Props:
- `active`: boolean - Marks this sub-item as currently active/selected.
- `href`: string - Navigation URL. When set, renders as a link via LinkProvider.
- `target`: React.HTMLAttributeAnchorTarget - Link target — only meaningful when `href` is provided.

#### Sidebar.MenuSubItem

MenuSubItem sub-component

Props:
- `active`: boolean - Marks this sub-item as currently active/selected.
- `href`: string - Navigation URL. When set, renders as a link via LinkProvider.
- `target`: React.HTMLAttributeAnchorTarget - Link target — only meaningful when `href` is provided.

#### Sidebar.MenuSubButton

MenuSubButton sub-component

Props:
- `active`: boolean - Marks this sub-item as currently active/selected.
- `href`: string - Navigation URL. When set, renders as a link via LinkProvider.
- `target`: React.HTMLAttributeAnchorTarget - Link target — only meaningful when `href` is provided.

#### Sidebar.Separator

Separator sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.Trigger

Trigger sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.Close

Close sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.Rail

Rail sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.ResizeHandle

ResizeHandle sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.MenuChevron

MenuChevron sub-component

Props:
- `className`: string

#### Sidebar.Collapsible

Collapsible sub-component

Props:
- `defaultOpen`: boolean - Initial open state (uncontrolled).
- `open`: boolean - Controlled open state.
- `autoScrollOnOpen`: boolean - Scroll the expanded content into view after opening.

#### Sidebar.CollapsibleTrigger

CollapsibleTrigger sub-component

Props:
- `render`: React.ReactElement (required) - Element to render as the trigger. Gets aria-expanded, aria-controls, and onClick merged.

#### Sidebar.CollapsibleContent

CollapsibleContent sub-component

Props:
- `activeKey`: string (required) - Key of the currently active view. Must match a child `SlidingView` value.
- `direction`: "left" | "right" - Slide direction for the transition. - `"left"`: new view slides in from the right - `"right"`: new view slides in from the left

#### Sidebar.SlidingViews

SlidingViews sub-component

Props:
- `activeKey`: string (required) - Key of the currently active view. Must match a child `SlidingView` value.
- `direction`: "left" | "right" - Slide direction for the transition. - `"left"`: new view slides in from the right - `"right"`: new view slides in from the left

#### Sidebar.SlidingView

SlidingView sub-component

Props:
- `value`: string (required) - Unique key matching this view. Must correspond to `activeKey` on `SlidingViews`.


**Examples:**

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
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

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
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
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain>
          <ToggleButton />
          <p>Click the button or the sidebar trigger to toggle</p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          {loading ? (
            <Sidebar.Loading />
          ) : (
            <Sidebar.Content>
              <Sidebar.Group>
                <Sidebar.Menu>
                  <Sidebar.MenuButton icon={HouseIcon} active>
                    Home
                  </Sidebar.MenuButton>
                  <Sidebar.MenuButton icon={ChartBarIcon}>
                    Analytics
                  </Sidebar.MenuButton>
                  <Sidebar.MenuButton icon={CodeIcon}>
                    Compute
                  </Sidebar.MenuButton>
                  <Sidebar.MenuButton icon={DatabaseIcon}>
                    Storage
                  </Sidebar.MenuButton>
                </Sidebar.Menu>
              </Sidebar.Group>
            </Sidebar.Content>
          )}
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain>
          <button
            type="button"
            onClick={() => setLoading((l) => !l)}
            className="cursor-pointer rounded-lg border border-kumo-line bg-kumo-base px-3 py-1.5 text-base text-kumo-default transition-colors hover:bg-kumo-tint"
          >
            {loading ? "Show loaded nav" : "Show loading"}
          </button>
          <p>Toggle to compare the loading state with the loaded nav</p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider
        contained
        defaultOpen
        resizable
        defaultWidth={240}
        minWidth={180}
        maxWidth={400}
        className="h-full min-h-0!"
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
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
          <Sidebar.ResizeHandle />
        </Sidebar>
        <DemoMain>
          <p>Drag the sidebar edge to resize</p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider
        contained
        defaultOpen
        side="right"
        className="h-full min-h-0!"
      >
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

```tsx
<div className="relative h-[420px] w-full overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
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
                        <Sidebar.MenuSubButton>
                          Deployments
                        </Sidebar.MenuSubButton>
                        <Sidebar.MenuSubButton>
                          Observability
                        </Sidebar.MenuSubButton>
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
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain>
          <p>Open Workers near the bottom of the list</p>
        </DemoMain>
      </Sidebar.Provider>
    </div>
```

```tsx
<DemoContainer>
      <Sidebar.Provider
        contained
        defaultOpen
        className="h-full min-h-0!"
        onOpenChangeComplete={(open) => {
          setCompletion(`Sidebar ${open ? "opened" : "collapsed"}.`);
        }}
      >
        <Sidebar>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.GroupLabel>Overview</Sidebar.GroupLabel>
              <Sidebar.Menu>
                <Sidebar.MenuButton icon={HouseIcon} active>
                  Home
                </Sidebar.MenuButton>
                <Sidebar.MenuItem>
                  <Sidebar.Collapsible
                    onOpenChangeComplete={(open) => {
                      setCompletion(
                        `Compute section ${open ? "opened" : "collapsed"}.`,
                      );
                    }}
                  >
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
                        <Sidebar.MenuSubButton>Workers</Sidebar.MenuSubButton>
                        <Sidebar.MenuSubButton>Pages</Sidebar.MenuSubButton>
                      </Sidebar.MenuSub>
                    </Sidebar.CollapsibleContent>
                  </Sidebar.Collapsible>
                </Sidebar.MenuItem>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain>
          <p aria-live="polite">{completion}</p>
          <p>
            Toggle the sidebar or open Compute to see the completion callback.
          </p>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
        <Sidebar>
          <Sidebar.Header>
            <button
              type="button"
              onClick={() =>
                setSurface((s) => (s === "account" ? "zone" : "account"))
              }
              className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium text-kumo-default transition-colors hover:bg-kumo-tint"
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
            <p>Click the header button to slide between views</p>
          </div>
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider
        contained
        defaultOpen
        peekable
        className="h-full min-h-0!"
      >
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
                      className="mb-3 ring ring-kumo-line transition-[margin] duration-(--sidebar-animation-duration) group-data-[state=collapsed]/sidebar:mb-0 group-data-[state=collapsed]/sidebar:ring-transparent"
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

          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain />
      </Sidebar.Provider>
    </DemoContainer>
```

```tsx
<DemoContainer>
      <Sidebar.Provider contained defaultOpen className="h-full min-h-0!">
        <Sidebar>
          <Sidebar.Header>
            <BrandLogo />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group>
              <Sidebar.Menu>
                {LONG_NAV_ITEMS.map((label, idx) => (
                  <Sidebar.MenuButton
                    key={label}
                    icon={idx === 0 ? HouseIcon : GlobeIcon}
                    active={idx === 0}
                    itemId={ANCHOR_ID_BY_LABEL[label]}
                  >
                    {label}
                  </Sidebar.MenuButton>
                ))}
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
        </Sidebar>
        <DemoMain>
          <ScrollToItemControls />
        </DemoMain>
      </Sidebar.Provider>
    </DemoContainer>
```

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
                <Sidebar.MenuButton icon={CodeIcon}>Compute</Sidebar.MenuButton>
                <Sidebar.MenuButton icon={DatabaseIcon}>
                  Storage
                </Sidebar.MenuButton>
              </Sidebar.Menu>
            </Sidebar.Group>
          </Sidebar.Content>
          <DemoSidebarFooter>
            <Sidebar.Trigger />
          </DemoSidebarFooter>
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

```tsx
<div className="flex flex-col gap-3">
      {/* Width control — stands in for resizing a real phone viewport. */}
      <label className="flex items-center gap-3 text-sm text-kumo-subtle">
        <span className="shrink-0">Viewport</span>
        <input
          type="range"
          min={280}
          max={720}
          value={width}
          onChange={(e) => setWidth(Number(e.target.value))}
          className="flex-1 cursor-pointer"
        />
        <span className="w-14 shrink-0 text-right tabular-nums">{width}px</span>
      </label>

      <div
        className="relative h-[540px] overflow-hidden rounded-lg border border-kumo-line bg-kumo-base"
        style={{ width }}
      >
        <Sidebar.Provider contained mobileBreakpoint={9999} className="h-full">
          <Sidebar fullScreenOnMobile>
            {/* Single row: avatar, then the trail takes the rest of the width.
                Mirrors the product header — account mark, then breadcrumbs. */}
            <Sidebar.Header className="gap-2 px-3.5">
              <CompactAccountSwitcher />
              <div className="min-w-0 flex-1">
                <Breadcrumbs size="sm">
                  {trail.map((label) => (
                    <Fragment key={label}>
                      <Breadcrumbs.Link href="#">{label}</Breadcrumbs.Link>
                      <Breadcrumbs.Separator />
                    </Fragment>
                  ))}
                  <Breadcrumbs.Current>{current}</Breadcrumbs.Current>
                </Breadcrumbs>
              </div>
              <Sidebar.Close />
            </Sidebar.Header>
            <Sidebar.Content>
              <div className="pt-1 pb-2">
                <QuickSearch />
              </div>
              <NavTree selected={selected} onSelect={setSelected} />
            </Sidebar.Content>
          </Sidebar>

          <div className="flex min-w-0 flex-1 flex-col">
            <header className="flex h-12 shrink-0 items-center gap-2 border-b border-kumo-line px-3">
              <Sidebar.Trigger />
              <div className="min-w-0 flex-1">
                <Breadcrumbs size="sm">
                  {trail.map((label) => (
                    <Fragment key={label}>
                      <Breadcrumbs.Link href="#">{label}</Breadcrumbs.Link>
                      <Breadcrumbs.Separator />
                    </Fragment>
                  ))}
                  <Breadcrumbs.Current>{current}</Breadcrumbs.Current>
                </Breadcrumbs>
              </div>
            </header>
            <DemoMain>
              <p className="text-kumo-default">{current}</p>
              <p className="text-sm text-kumo-subtle">
                Drill into the nav — the trail is derived from the tree, so it
                always matches where you are.
              </p>
            </DemoMain>
          </div>
        </Sidebar.Provider>
      </div>
    </div>
```


---

### Slider

Slider component

**Type:** component

**Import:** `import { Slider } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `size`: enum [default: base]
  - `"sm"`: Compact slider for dense layouts
  - `"base"`: Default slider size
- `label`: ReactNode
  Label displayed above the slider track.
- `getAriaLabel`: (index: number) => string
  Accessible name for each thumb. Use it when there is no visible `label`, or to tell the thumbs of a range slider apart.
- `value`: number | readonly number[]
  Controlled value. Pass an array to render one thumb per value (a range).
- `defaultValue`: number | readonly number[]
  Uncontrolled initial value. Pass an array to render one thumb per value (a range).
- `onValueChange`: (value: number | readonly number[], eventDetails: SliderRoot.ChangeEventDetails) => void
  Called when the value changes while dragging or from the keyboard
- `min`: number
  Minimum value of the slider (default: 0)
- `max`: number
  Maximum value of the slider (default: 100)
- `step`: number
  Amount the value changes per step (default: 1)
- `format`: Intl.NumberFormatOptions
  Formats the value badges, range labels, and the value announced to assistive technology
- `disabled`: boolean
  Whether the slider is disabled

**Colors (kumo tokens used):**

`bg-kumo-contrast`, `bg-kumo-fill`, `bg-kumo-overlay`, `bg-kumo-recessed`, `ring-kumo-focus`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-inverse`, `text-kumo-subtle`

**Examples:**

```tsx
<Slider label="Volume" defaultValue={40} className="w-72" />
```

```tsx
<Slider
      label="Price range"
      defaultValue={[25, 75]}
      getAriaLabel={(index) =>
        index === 0 ? "Minimum price" : "Maximum price"
      }
      className="w-72"
    />
```

```tsx
<Slider label="Match count" defaultValue={2} max={5} className="w-72" />
```

```tsx
<Slider
      label="Sampling rate"
      defaultValue={0.25}
      min={0}
      max={1}
      step={0.05}
      format={{ style: "percent" }}
      className="w-72"
    />
```

```tsx
<div className="flex w-72 flex-col gap-6">
      <Slider label="Small" size="sm" defaultValue={30} />
      <Slider label="Base" defaultValue={30} />
    </div>
```

```tsx
<div className="flex w-72 flex-col gap-3">
      <Slider
        label="Brightness"
        value={value}
        onValueChange={(next) => setValue(next)}
      />
      <p className="text-sm text-kumo-subtle">Brightness: {value}%</p>
    </div>
```


---

### Surface

Surface component

**Type:** component

**Import:** `import { Surface } from "@cloudflare/kumo";`

**Category:** Layout

**Props:**

- `as`: React.ElementType
- `render`: ReactNode
  Allows you to replace the component's HTML element with a different tag, or compose it with another component.

Accepts a `ReactElement` or a function that returns the element to render.
- `children`: ReactNode
- `className`: string
- `id`: string
- `lang`: string
- `title`: string

---

### Switch

Switch component

**Type:** component

**Import:** `import { Switch } from "@cloudflare/kumo";`

**Category:** Input

**Props:**

- `variant`: enum [default: default]
  - `"default"`: Default switch with squircle shape and brand blue color
  - `"neutral"`: Monochrome switch with squircle shape for subtle toggles
- `label`: ReactNode
  Label content for the switch (Field wrapper is built-in) - can be a string or any React node. Optional when used standalone for visual-only purposes.
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon
- `required`: boolean
  Whether the switch is required. When explicitly false, shows "(optional)" text after the label.
- `controlFirst`: boolean
  When true (default), switch appears before label. When false, label appears before switch.
- `size`: enum [default: base]
  - `"sm"`: Small switch for compact UIs
  - `"base"`: Default switch size
  - `"lg"`: Large switch for prominent toggles
- `checked`: boolean
- `disabled`: boolean
- `transitioning`: boolean
- `name`: string
- `type`: enum
- `value`: string | string[] | number
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `onClick`: (event: React.MouseEvent) => void (required)
  Callback when switch is clicked

**Colors (kumo tokens used):**

`bg-kumo-base`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-hairline`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Switch.Item

Item sub-component

#### Switch.Group

Group sub-component

Props:
- `legend`: string - Legend text for the group. For more control over legend styling, omit this prop and use `<Switch.Legend>` as a child instead.
- `children`: ReactNode (required) - Child Switch.Item components (and optionally a Switch.Legend)
- `error`: string - Error message for the group (only appears in groups, not single switches)
- `description`: ReactNode - Helper text for the group
- `disabled`: boolean - Whether all switches in the group are disabled
- `controlFirst`: boolean - When true (default), switch appears before label. When false, label appears before switch.
- `className`: string - Additional CSS classes

#### Switch.Legend

Legend sub-component

Props:
- `children`: ReactNode (required) - Legend content
- `className`: string - Additional CSS classes (e.g. "sr-only" to visually hide the legend)


**Examples:**

```tsx
<Switch label="Switch" checked={checked} onCheckedChange={setChecked} />
```

```tsx
<Switch label="Disabled" checked={false} disabled />
```

```tsx
<Switch
      label="Neutral switch"
      variant="neutral"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

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

```tsx
<Switch
      id="my-custom-switch"
      label="Custom ID"
      checked={checked}
      onCheckedChange={setChecked}
    />
```

```tsx
<Switch.Group legend="Notification settings">
      <Switch.Item label="Email notifications" />
      <Switch.Item label="SMS notifications" />
      <Switch.Item label="Push notifications" />
    </Switch.Group>
```

```tsx
<Switch.Group>
      <Switch.Legend className="sr-only">Notification settings</Switch.Legend>
      <Switch.Item label="Email notifications" />
      <Switch.Item label="SMS notifications" />
      <Switch.Item label="Push notifications" />
    </Switch.Group>
```

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


---

### Table

Table — semantic HTML table with styled rows, cells, and selection support.

**Type:** component

**Import:** `import { Table } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `layout`: enum [default: auto]
  - `"auto"`: Auto table layout - columns resize based on content
  - `"fixed"`: Fixed table layout - columns have equal width, controlled via colgroup
- `variant`: enum [default: default]
  - `"default"`: Default row variant
  - `"selected"`: Selected row variant
- `sticky`: enum
  - `"left"`: Pin column to the left edge of the scroll container
  - `"right"`: Pin column to the right edge of the scroll container
- `className`: string
  Additional CSS classes
- `children`: ReactNode
  Child elements

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-elevated`, `bg-kumo-hairline`, `bg-kumo-tint`, `border-kumo-fill`, `ring-kumo-brand`, `text-kumo-default`, `text-kumo-strong`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Table.Header

Header sub-component

#### Table.Head

Head sub-component

#### Table.Row

Row sub-component

#### Table.Body

Body sub-component

#### Table.Cell

Cell sub-component

#### Table.CheckCell

CheckCell sub-component

#### Table.CheckHead

CheckHead sub-component

#### Table.Footer

Footer sub-component

#### Table.ResizeHandle

ResizeHandle sub-component


**Examples:**

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
              <Table.Cell className="whitespace-nowrap">{row.from}</Table.Cell>
              <Table.Cell className="whitespace-nowrap">{row.date}</Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.tags ? (
                  <div className="inline-flex gap-1">
                    {row.tags.map((tag) => (
                      <Badge key={tag}>{tag}</Badge>
                    ))}
                  </div>
                ) : (
                  "—"
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
              <Table.Cell className="whitespace-nowrap">{row.from}</Table.Cell>
              <Table.Cell className="whitespace-nowrap">{row.date}</Table.Cell>
              <Table.Cell className="whitespace-nowrap">
                {row.tags ? (
                  <div className="inline-flex gap-1">
                    {row.tags.map((tag) => (
                      <Badge key={tag}>{tag}</Badge>
                    ))}
                  </div>
                ) : (
                  "—"
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


---

### TableOfContents

TableOfContents — presentational compound component for section navigation.

**Type:** component

**Import:** `import { TableOfContents } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `children`: ReactNode
- `className`: string
- `id`: string
- `lang`: string
- `title`: string

**Colors (kumo tokens used):**

`border-kumo-brand`, `border-kumo-hairline`, `border-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### TableOfContents.Title

Title sub-component

#### TableOfContents.List

List sub-component

#### TableOfContents.Item

Item sub-component

Props:
- `active`: boolean - Whether this item represents the currently active section.
- `render`: React.ReactElement - Custom element to render as the link. Use this to integrate with framework routers (e.g., Next.js `<Link>`, React Router `<NavLink>`). The element receives all anchor props including `href`, `className`, and `children`.

#### TableOfContents.Group

Group sub-component

Props:
- `label`: string (required) - Label displayed above the group's items.
- `href`: string - URL the group label links to. When provided, the label renders as a clickable link with item styling.
- `active`: boolean - Whether this group's label represents the currently active section. Only applies when `href` is provided.


**Examples:**

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

```tsx
<div className="flex w-full max-w-xl gap-6">
      <div className="min-w-40">
        <TableOfContents>
          <TableOfContents.Title>On this page</TableOfContents.Title>
          <TableOfContents.List>
            {scrollspySections.map((section) => (
              <TableOfContents.Item
                key={section.id}
                render={<button type="button" />}
                active={activeId === section.id}
                onClick={() => {
                  selectSection(section.id);
                  root
                    ?.querySelector(`#${section.id}`)
                    ?.scrollIntoView({ behavior: "smooth", block: "start" });
                }}
              >
                {section.title}
              </TableOfContents.Item>
            ))}
          </TableOfContents.List>
        </TableOfContents>
      </div>
      <div
        ref={setRoot}
        className="h-64 flex-1 overflow-y-auto rounded-lg border border-kumo-hairline p-4"
      >
        {scrollspySections.map((section) => (
          <section key={section.id}>
            <h4
              id={section.id}
              className="mb-2 scroll-mt-2 text-sm font-semibold"
            >
              {section.title}
            </h4>
            <p className="mb-6 text-sm text-kumo-subtle">
              {Array.from(
                { length: 6 },
                () =>
                  `Scrollable placeholder copy for the ${section.title} section. `,
              ).join("")}
            </p>
          </section>
        ))}
        <div className="h-40" />
      </div>
    </div>
```

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


---

### Tabs

Tab navigation component with segmented or underline style. Built on Base UI Tabs with animated active indicator.

**Type:** component

**Import:** `import { Tabs } from "@cloudflare/kumo";`

**Category:** Navigation

**Props:**

- `tabs`: TabsItem[]
  Array of tab items to render.
- `value`: string
  Controlled value. When set, component becomes controlled.
- `selectedValue`: string
  Default selected value for uncontrolled mode. Ignored when `value` is set.
- `activateOnFocus`: boolean
  When `true`, tabs are activated immediately upon receiving focus via arrow keys. When `false` (default), tabs receive focus but require Enter/Space to activate.
- `className`: string
  Additional CSS classes for the root element.
- `listClassName`: string
  Additional CSS classes for the tab list element.
- `indicatorClassName`: string
  Additional CSS classes for the indicator element.
- `labels`: TabsLabels
  Labels for internationalization of aria-labels. All labels have English defaults.
- `variant`: enum [default: segmented]
  Tab style.
- `"segmented"` — Pill-shaped indicator on a filled track
- `"underline"` — Underline indicator below tab text
- `size`: enum [default: base]
  Tab size.
- `"base"` — Default size (h-9, text-base)
- `"sm"` — Compact size (h-6.5, text-xs) — matches Input size="sm"
- `onValueChange`: (value: string) => void
  Callback when active tab changes

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-brand`, `bg-kumo-recessed`, `bg-kumo-tint`, `border-kumo-hairline`, `ring-kumo-brand`, `ring-kumo-focus`, `ring-kumo-hairline`, `ring-kumo-line`, `text-kumo-default`, `text-kumo-subtle`

**Styling:**


**Examples:**

```tsx
<div className="flex flex-col items-start gap-6">
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

```tsx
<div className="space-y-3">
      <div className="w-full max-w-[588px]">
        <Tabs tabs={tabs} selectedValue="settings" />
      </div>
      <div className="flex items-center gap-3 text-sm text-kumo-subtle">
        <button
          type="button"
          className="rounded-md border border-kumo-line bg-kumo-base px-2.5 py-1 text-kumo-default hover:bg-kumo-tint focus:ring-2 focus:ring-kumo-brand focus:outline-none"
          onClick={() => setShowExtraTabs((current) => !current)}
        >
          Toggle extra tabs
        </button>
        <span>{showExtraTabs ? "10 tabs" : "7 tabs"}</span>
      </div>
    </div>
```

```tsx
<div className="flex flex-col items-start gap-6">
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
          nativeButton: false,
          render: (props) => <a {...props} href="#tab2" />,
        },
        {
          value: "tab3",
          label: "Cloudflare",
          nativeButton: false,
          render: (props) => (
            <a {...props} href="https://cloudflare.com" target="_blank" />
          ),
        },
      ]}
      selectedValue="tab1"
    />
```


---

### TagInput

TagInput component

**Type:** component

**Import:** `import { TagInput } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `alt`: string
- `autoComplete`: React.HTMLInputAutoCompleteAttribute
- `checked`: boolean
- `height`: number | string
- `list`: string
- `name`: string
  Submits each accepted tag as a separate form value under this name.
- `placeholder`: string
- `readOnly`: boolean
  Prevents adding or removing tags while keeping the input focusable.
- `required`: boolean
  Requires at least one accepted tag for native form validation. Like a native input, a `readOnly` TagInput is skipped by validation.
- `type`: React.HTMLInputTypeAttribute
- `width`: number | string
- `className`: string
- `id`: string
- `lang`: string
- `title`: string
- `children`: ReactNode
- `value`: string[]
- `validateValue`: object
- `maxValues`: number
- `labels`: TagInputLabels
  Translated labels for generated input, action, and validation text.
- `label`: ReactNode
- `labelTooltip`: ReactNode
- `description`: ReactNode
- `error`: string | object
- `variant`: enum [default: default]
  - `"default"`: Default tag input appearance.
  - `"error"`: Error state for validation failures.
- `disabled`: boolean

**Colors (kumo tokens used):**

`bg-kumo-overlay`, `ring-kumo-hairline`, `text-kumo-default`

**Styling:**


**Examples:**

```tsx
<TagInput
      label="Recipients"
      description="Paste comma- or newline-separated email addresses."
      placeholder="name@example.com"
      autoComplete="off"
      value={recipients}
      onValueChange={setRecipients}
      validateValue={(value) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)}
    />
```

```tsx
<TagInput
      defaultValue={["frontend", "priority"]}
      label="Labels"
      description="Accepts any non-empty value."
      placeholder="Add a label"
    />
```

```tsx
<TagInput
      defaultValue={["alpha"]}
      label="Access groups"
      maxValues={3}
      description="A maximum of three groups."
      placeholder="Type a group name"
    />
```

```tsx
<form
      className="flex w-full max-w-md flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        setSubmitted(data.getAll("topics").map(String));
      }}
    >
      <TagInput
        name="topics"
        required
        label="Topics"
        description="Add at least one topic, then submit."
        placeholder="Add a topic"
      />
      <div className="flex items-center gap-3">
        <Button type="submit" variant="primary">
          Submit
        </Button>
        {submitted ? (
          <code className="text-sm text-kumo-subtle">
            topics: {JSON.stringify(submitted)}
          </code>
        ) : null}
      </div>
    </form>
```

```tsx
<TagInput
      readOnly
      defaultValue={["billing", "dns", "workers"]}
      label="Assigned teams"
      description="Managed by your organization's administrator."
    />
```

```tsx
<TagInput
      defaultValue={["uno"]}
      label="Etiquetas"
      labels={{
        input: "Agregar etiqueta",
        removeValue: (value) => `Eliminar ${value}`,
        invalidValue: (value) => `${value} no es valido.`,
        maxValuesReached: (maxValues) => `Maximo de ${maxValues} etiquetas.`,
      }}
      maxValues={3}
      placeholder="Agregar una etiqueta"
    />
```


---

### Text

Text component

**Type:** component

**Import:** `import { Text } from "@cloudflare/kumo";`

**Category:** Display

**Props:**

- `variant`: enum [default: body]
  - `"heading"`: Heading text (16px by default, 20px at large size)
  - `"heading1"`: Deprecated large heading for page titles; use heading instead
  - `"heading2"`: Deprecated medium heading for section titles; use heading instead
  - `"heading3"`: Deprecated small heading for subsections; use heading instead
  - `"body"`: Default body text
  - `"secondary"`: Muted text for secondary information
  - `"success"`: Success state text
  - `"error"`: Error state text
  - `"mono"`: Monospace text for code
  - `"mono-secondary"`: Muted monospace text
- `size`: enum [default: base]
  - `"xs"`: Extra small text
  - `"sm"`: Small text
  - `"base"`: Default text size
  - `"lg"`: Large text
- `bold`: boolean
  Whether to use bold font weight (only applies to body variants).
- `truncate`: boolean
  Whether to truncate overflowing text with an ellipsis. Adds `truncate min-w-0` classes.
- `as`: enum
  The HTML element to render. Accepts headings (`"h1"`–`"h6"`), block text (`"p"`, `"pre"`), inline text (`"span"`, `"code"`, `"em"`, `"strong"`, `"small"`, `"abbr"`, `"time"`), form-related (`"label"`, `"legend"`), list/definition (`"dt"`, `"dd"`, `"li"`), and `"figcaption"`.

- **Optional** for `"heading"` (defaults to `"span"`). Pass the heading   element that reflects this text's place in the document outline.
- **Required** for deprecated heading variants (`"heading1"`,   `"heading2"`, `"heading3"`).
- **Optional** for body variants (defaults to `"p"`) and monospace   variants (defaults to `"span"`).
- `children`: ReactNode
  Text content.

**Colors (kumo tokens used):**

`text-kumo-danger`, `text-kumo-default`, `text-kumo-link`, `text-kumo-subtle`

**Styling:**


**Examples:**

```tsx
<div className="grid w-full grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="heading">Heading</Text>
        <Text variant="mono-secondary">text-lg (16px)</Text>
      </div>
      <div className="flex flex-col justify-end gap-1 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
        <Text variant="heading" size="lg" as="h2">
          Heading large
        </Text>
        <Text variant="mono-secondary">text-xl (20px)</Text>
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

```tsx
<div className="w-64 rounded-lg border border-kumo-hairline bg-kumo-base p-4">
      <Text truncate>
        This is a long piece of text that will be truncated with an ellipsis
        when it overflows its container.
      </Text>
    </div>
```


---

### TimeseriesChart

TimeseriesChart — a time-series line or bar chart.

**Type:** component

**Import:** `import { TimeseriesChart } from "@cloudflare/kumo";`

**Category:** Data Visualization

**Props:**

- `echarts`: typeof echarts (required)
  The ECharts core instance imported by the consumer. Passed in rather than imported directly so the consumer controls which ECharts modules are bundled (tree-shaking).
- `type`: "line" | "bar"
  Visual style of each series. Defaults to `"line"`.
- `data`: TimeseriesData[] (required)
  Array of time series data to display on the chart
- `markers`: TimeseriesMarker[]
  Vertical reference markers rendered on the time axis.
- `thresholds`: TimeseriesThreshold[]
  Horizontal threshold lines rendered on the value axis.
- `xAxisName`: string
  Label for the x-axis (time axis)
- `xAxisTickCount`: number
  Number of ticks to display on the x-axis
- `xAxisHideLabelOverlap`: boolean
  Whether overlapping x-axis labels are automatically hidden. Defaults to `true`.
- `xAxisTickFormat`: (value: number) => string
  Custom formatter for x-axis tick labels. Receives the raw timestamp in milliseconds and returns a display string, overriding ECharts' built-in time formatting.
- `yAxisTickFormat`: (value: number) => string
  Custom formatter for y-axis tick labels. Receives the raw value and returns a display string. When omitted, ECharts' built-in formatter is used.
- `yAxisTickLabelFormat`: (value: number) => string
- `yAxisName`: string
  Label for the y-axis (value axis)
- `yAxisTickCount`: number
  Number of ticks to display on the y-axis
- `yAxisMinInterval`: number
  Minimum interval between y-axis ticks. Set to `1` for discrete count data so ECharts does not render fractional tick marks.
- `tooltipValueFormat`: (value: number) => string
  Custom formatter for tooltip values. Receives the raw y-value and returns a display string. When omitted, the raw value is shown. Takes precedence over the deprecated `yAxisTickLabelFormat` prop.
- `tooltipTimestampFormat`: (timestamp: number) => string
  Custom formatter for tooltip timestamps. Receives the raw timestamp in milliseconds and returns a display string. Applies to standard series and marker tooltips. When omitted, timestamps are formatted using the browser's locale and time zone.
- `tooltipFooter`: string
  Footer text rendered below the series rows in the tooltip. Intended for brief context such as data freshness or aggregation details. Available for every `TimeseriesChart` configuration.
- `tooltipMode`: "all" | "single"
  Controls which series are shown in the tooltip. - `"all"` — show all series at the hovered timestamp (default) - `"single"` — show only the series whose value is closest to the cursor
- `tooltipMaxItems`: number
  Maximum number of series rows shown in the tooltip when `tooltipMode` is `"all"`. Additional series are hidden with a `+N more` footer. Defaults to `10`.
- `tooltipBoundary`: "clipping-ancestors" | Element | Element[]
  Constrains the tooltip to stay within a specific element or region. By default the tooltip avoids overflowing any clipping ancestor (scroll containers, viewports, etc.). Pass an `Element` or array of elements to restrict the tooltip to a specific container.
- `tooltipFollowCursor`: "both" | "x"
  Which axis the tooltip follows the cursor on. - `"both"` — tooltip tracks the cursor on both axes, staying near the pointer at all times. This is the default and matches the behaviour of ECharts' built-in tooltip. - `"x"` — tooltip follows the cursor horizontally but is locked to a fixed vertical position relative to the chart. This keeps the tooltip out of the way of the data and avoids vertical jitter as series values change — the same approach used by Recharts and many dashboard UIs. Only these two modes are offered because the x-axis is always time in a `TimeseriesChart`: y-only tracking and fully-fixed positioning don't produce useful tooltip behaviour for time-series data. Powered by Base UI Tooltip's `trackCursorAxis` under the hood.
- `incomplete`: { before?: number; after?: number }
  Indicates incomplete data periods with optional before/after timestamps in ms
- `enableLegendSelection`: boolean
  When `true`, adds a hidden ECharts legend so consumers can drive series visibility imperatively via the `legendSelect` / `legendUnSelect` / `legendToggleSelect` actions (e.g. to build a custom interactive legend). Toggled-off series are also excluded from the tooltip. Requires the consumer to register ECharts' `LegendComponent` (`echarts.use([LegendComponent])`); otherwise the legend actions no-op and, in development, ECharts logs a "component legend is used but not imported" warning.
- `height`: number
  Height of the chart in pixels. Defaults to `350`.
- `isDarkMode`: boolean
  When `true`, switches the chart to ECharts' built-in dark theme
- `gradient`: boolean
  When `true`, renders a vertical gradient fill beneath each line series. The gradient fades from the series' color at the top to transparent at the bottom. Has no effect when `type` is `"bar"`.
- `loading`: boolean
  When `true`, hides the chart and displays a skeleton matching the chart type with a subtle shimmer.
- `ariaDescription`: string
  Accessible description for screen readers. When provided, it is passed to ECharts' `aria.label.description` and announced when the chart receives focus. Consumers are responsible for writing a meaningful description — see the W3C guidance on complex images for recommendations.
- `optionUpdateBehavior`: SetOptionOpts
  Additional options passed as the second argument to `chart.setOption()`. Defaults to `{ notMerge: false, lazyUpdate: true }`.

**Colors (kumo tokens used):**

`bg-kumo-base`, `outline-kumo-line`

**Examples:**

```tsx
<Chart
      echarts={echarts}
      options={options}
      height={400}
      isDarkMode={isDarkMode}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Count"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Requests"
      yAxisMinInterval={1}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      markers={markers}
      xAxisName="Time (UTC)"
      yAxisName="Count"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      thresholds={[
        {
          value: 55,
          label: "Memory limit",
          color: ChartPalette.semantic("Attention", isDarkMode),
        },
      ]}
      xAxisName="Time (UTC)"
      yAxisName="Memory (MB)"
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Requests"
      xAxisTickFormat={(timestamp) =>
        new Intl.DateTimeFormat(undefined, {
          timeZone: "UTC",
          hour: "2-digit",
          minute: "2-digit",
        }).format(timestamp)
      }
      yAxisTickFormat={(value) => {
        if (value >= 1000) return `${value / 1000}k`;
        return value.toString();
      }}
      tooltipValueFormat={(value) => `${(value / 1000).toFixed(1)}k requests`}
      tooltipTimestampFormat={(timestamp) =>
        new Intl.DateTimeFormat(undefined, {
          timeZone: "UTC",
          dateStyle: "medium",
          timeStyle: "medium",
        }).format(timestamp)
      }
    />
```

```tsx
<TimeseriesChart
      yAxisTickCount={2}
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      height={160}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Mbps"
      incomplete={{ after: incompleteTimestamp }}
    />
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="%"
      onTimeRangeChange={(from, to) => {
        alert(
          `Selected range:\nFrom: ${new Date(from).toLocaleString()}\nTo: ${new Date(to).toLocaleString()}`,
        );
      }}
    />
```

```tsx
<div className="space-y-4">
      <h3 className="text-sm font-medium">Active State</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem
          name="Requests"
          color={ChartPalette.semantic("Neutral", isDarkMode)}
          value="1,234"
          unit="req/s"
        />
        <ChartLegend.LargeItem
          name="Storage"
          color={ChartPalette.semantic("Attention", isDarkMode)}
          value="56"
          unit="GB"
        />
        <ChartLegend.LargeItem
          name="Warnings"
          color={ChartPalette.semantic("Warning", isDarkMode)}
          value="128"
        />
      </div>

      <h3 className="mt-12 text-sm font-medium">Inactive State</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem
          name="Requests"
          color={ChartPalette.semantic("Neutral", isDarkMode)}
          value="1,234"
          unit="req/s"
          inactive
        />
        <ChartLegend.LargeItem
          name="Storage"
          color={ChartPalette.semantic("Attention", isDarkMode)}
          value="56"
          unit="GB"
          inactive
        />
        <ChartLegend.LargeItem
          name="Warnings"
          color={ChartPalette.semantic("Warning", isDarkMode)}
          value="128"
          inactive
        />
      </div>

      <h3 className="mt-12 text-sm font-medium">Loading state</h3>

      <div className="flex flex-wrap gap-4 divide-x divide-kumo-hairline">
        <ChartLegend.LargeItem loading />
      </div>
    </div>
```

```tsx
<TimeseriesChart
      echarts={echarts}
      isDarkMode={isDarkMode}
      type="bar"
      data={data}
      xAxisName="Time (UTC)"
      yAxisName="Count"
      tooltipValueFormat={(r) => r.toFixed(2)}
    />
```

```tsx
<div className="flex w-full flex-1 flex-col">
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={[]}
        loading
      />
    </div>
```

```tsx
<div className="flex w-full flex-1 flex-col">
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        type="bar"
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={[]}
        loading
      />
    </div>
```

```tsx
<div className="flex w-full flex-1 flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="flex flex-wrap gap-4">
          {data.map((series) => (
            <ChartLegend.SmallItem
              loading={loading}
              key={series.name}
              name={series.name}
              color={series.color}
              value={Math.round(series.data.at(-1)?.[1] ?? 0).toLocaleString()}
            />
          ))}
        </div>
        <Switch
          label="Loading"
          checked={loading}
          onCheckedChange={setLoading}
        />
      </div>
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        data={loading ? [] : data}
        loading={loading}
      />
    </div>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex gap-4 divide-x divide-kumo-hairline px-2">
          <ChartLegend.LargeItem
            name="P99"
            color={ChartPalette.semantic("Attention", isDarkMode)}
            value="124"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P95"
            color={ChartPalette.semantic("Warning", isDarkMode)}
            value="76"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P75"
            color={ChartPalette.semantic("Neutral", isDarkMode)}
            value="32"
            unit="ms"
          />
          <ChartLegend.LargeItem
            name="P50"
            color={ChartPalette.semantic("Neutral", isDarkMode)}
            value="10"
            unit="ms"
          />
        </div>
        <TimeseriesChart
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
          tooltipFooter="Percentiles use a five-minute rolling window."
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex divide-x divide-kumo-line px-2">
          {series.map((s) => (
            <ChartLegend.LargeItem
              key={s.name}
              name={s.name}
              color={s.color}
              value={s.value}
              unit={s.unit}
              inactive={hoveredSeries !== null && hoveredSeries !== s.name}
              onPointerEnter={() => {
                setHoveredSeries(s.name);
                chartRef.current?.dispatchAction({
                  type: "highlight",
                  seriesName: s.name,
                });
              }}
              onPointerLeave={() => {
                setHoveredSeries(null);
                chartRef.current?.dispatchAction({
                  type: "downplay",
                  seriesName: s.name,
                });
              }}
              className="not-first:pl-4"
            />
          ))}
        </div>
        <TimeseriesChart
          ref={chartRef}
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<LayerCard>
      <LayerCard.Secondary>Read latency</LayerCard.Secondary>
      <LayerCard.Primary>
        <div className="mb-2 flex divide-x divide-kumo-line px-2">
          {series.map((s) => (
            <ChartLegend.LargeItem
              key={s.name}
              name={s.name}
              color={s.color}
              value={s.value}
              unit={s.unit}
              inactive={hiddenSeries[s.name] ?? false}
              onClick={() => handleClick(s.name)}
              className="not-first:pl-4"
            />
          ))}
        </div>
        <TimeseriesChart
          ref={chartRef}
          xAxisName="Time (UTC)"
          echarts={echarts}
          isDarkMode={isDarkMode}
          data={data}
          height={300}
          enableLegendSelection
        />
      </LayerCard.Primary>
    </LayerCard>
```

```tsx
<div className="flex w-full flex-col gap-4">
      <Select
        label="Tooltip follow cursor"
        value={selected}
        onValueChange={(v) => {
          if (v) setSelected(v);
        }}
        renderValue={(v) => v.label}
      >
        {FOLLOW_CURSOR_OPTIONS.map((opt) => (
          <Select.Option key={opt.value} value={opt}>
            {opt.label}
          </Select.Option>
        ))}
      </Select>
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        data={data}
        xAxisName="Time (UTC)"
        yAxisName="Latency (ms)"
        tooltipFollowCursor={selected.value}
      />
    </div>
```

```tsx
<div
      ref={boundaryRef}
      className="w-full overflow-auto rounded-lg border border-kumo-line"
      style={{ height: 300 }}
    >
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={isDarkMode}
        data={data}
        xAxisName="Time (UTC)"
        yAxisName="Count"
        height={280}
        tooltipBoundary={boundary ?? undefined}
      />
    </div>
```


---

### Toasty

Toasty — toast notification provider and viewport.

**Type:** component

**Import:** `import { Toasty } from "@cloudflare/kumo";`

**Category:** Feedback

**Props:**

- `variant`: enum [default: default]
  - `"default"`: Default toast style
  - `"success"`: Success toast for confirmations and positive outcomes
  - `"error"`: Error toast for critical issues
  - `"warning"`: Warning toast for cautionary messages
  - `"info"`: Info toast for neutral informational messages
- `children`: React.ReactNode (required)
  Application content. Toasts render via a portal above this.
- `container`: PortalContainer
  Container element for the portal. Use this to render toasts inside a Shadow DOM or custom container. Overrides `KumoPortalProvider` context.
- `toastManager`: ReturnType<typeof createKumoToastManager>
  Optional toast manager created by `createKumoToastManager()`. When provided, allows code outside the React tree (timers, module-load callbacks, query-cache listeners) to dispatch toasts via the same dedupe-aware manager that `useKumoToastManager()` returns inside the tree. Forwarded to the underlying `@base-ui/react/toast` `Toast.Provider` `toastManager` prop — see https://base-ui.com/react/components/toast for the upstream primitive.

**Colors (kumo tokens used):**

`bg-kumo-base`, `bg-kumo-control`, `bg-kumo-danger-tint`, `bg-kumo-info-tint`, `bg-kumo-success-tint`, `bg-kumo-warning-tint`, `border-kumo-fill`, `ring-kumo-danger`, `ring-kumo-hairline`, `ring-kumo-info`, `ring-kumo-line`, `ring-kumo-success`, `ring-kumo-warning`, `text-kumo-danger`, `text-kumo-default`, `text-kumo-info`, `text-kumo-subtle`, `text-kumo-success`, `text-kumo-warning`

**Styling:**


**Examples:**

```tsx
<Toasty>
      <ToastFunctionalUpdateButton />
    </Toasty>
```


---

### Toolbar

Toolbar component

**Type:** component

**Import:** `import { Toolbar } from "@cloudflare/kumo";`

**Category:** Other

**Props:**

- `disabled`: boolean
- `orientation`: ToolbarRoot.Orientation
  The orientation of the toolbar.
- `loopFocus`: boolean
  If `true`, using keyboard navigation will wrap focus to the other end of the toolbar once the end is reached.
- `id`: string
- `lang`: string
- `title`: string
- `className`: string | object
  CSS class applied to the element, or a function that returns a class based on the component's state.
- `render`: ReactNode
  Allows you to replace the component's HTML element with a different tag, or compose it with another component.

Accepts a `ReactElement` or a function that returns the element to render.
- `children`: ReactNode
  Toolbar controls rendered as one grouped card.
- `size`: enum [default: base]
  - `"xs"`: Extra small toolbar for compact UIs
  - `"sm"`: Small toolbar for secondary controls
  - `"base"`: Default toolbar size
  - `"lg"`: Large toolbar for prominent controls

**Colors (kumo tokens used):**

`bg-kumo-control`, `border-kumo-line`, `ring-kumo-line`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Toolbar.Button

Button sub-component

#### Toolbar.Link

Link sub-component

#### Toolbar.Input

Input sub-component

#### Toolbar.InputGroup

InputGroup sub-component


**Examples:**

```tsx
<Toolbar className="w-full max-w-md">
      <Toolbar.InputGroup aria-label="Search DNS records" className="flex-1">
        <InputGroup.Addon>
          <MagnifyingGlassIcon />
        </InputGroup.Addon>
        <InputGroup.Input placeholder="Search DNS records" />
      </Toolbar.InputGroup>
      <Toolbar.Button icon={FunnelSimpleIcon} aria-label="Filter" />
      <Toolbar.Button icon={GearSixIcon} aria-label="Settings" />
    </Toolbar>
```

```tsx
<div className="grid gap-3">
      {(["xs", "sm", "base", "lg"] as const).map((size) => (
        <div key={size} className="flex items-center gap-3">
          <span className="w-10 text-sm text-kumo-subtle">{size}</span>
          <Toolbar size={size} className="w-fit">
            <Toolbar.Input
              aria-label={`${size} search`}
              placeholder="Search..."
            />
            <Toolbar.Button>Apply</Toolbar.Button>
          </Toolbar>
        </div>
      ))}
    </div>
```

```tsx
<Toolbar className="w-full max-w-lg">
      <Toolbar.InputGroup aria-label="Worker subdomain" className="flex-1">
        <InputGroup.Input placeholder="my-worker" />
        <InputGroup.Suffix>.workers.dev</InputGroup.Suffix>
      </Toolbar.InputGroup>
      <Toolbar.Button>Visit</Toolbar.Button>
    </Toolbar>
```

```tsx
<Toolbar>
      <Toolbar.Button icon={UploadSimpleIcon}>Upload</Toolbar.Button>
      <Toolbar.Button icon={DownloadSimpleIcon}>Download</Toolbar.Button>
    </Toolbar>
```

```tsx
<Toolbar>
      <Toolbar.Link href="/components/button" icon={BookOpenIcon}>
        Button documentation
      </Toolbar.Link>
      <Toolbar.Button icon={DownloadSimpleIcon}>Download</Toolbar.Button>
    </Toolbar>
```

```tsx
<Toolbar className="w-full max-w-lg">
      <Toolbar.Input
        aria-label="Search records"
        className="flex-1"
        placeholder="Search"
      />
      <Toolbar.Button icon={MagnifyingGlassIcon} aria-label="Search" />
    </Toolbar>
```

```tsx
<Toolbar>
      <Toolbar.Button icon={FunnelSimpleIcon}>Filter</Toolbar.Button>
      <Select
        aria-label="Sort records"
        defaultValue="name"
        items={{ name: "Name", created: "Created date", status: "Status" }}
        render={<Toolbar.Button />}
      />
      <Toolbar.Button icon={GearSixIcon} aria-label="View settings" />
    </Toolbar>
```

```tsx
<Toolbar className="w-full max-w-md">
      <Toolbar.Button icon={FunnelSimpleIcon}>Status</Toolbar.Button>
      <Combobox items={toolbarComboboxItems}>
        <Combobox.TriggerInput
          aria-label="Filter status"
          className="flex-1"
          placeholder="Filter status…"
          render={<Toolbar.Input />}
        />
        <Combobox.Content>
          <Combobox.List>
            {(item: string) => (
              <Combobox.Item key={item} value={item}>
                {item}
              </Combobox.Item>
            )}
          </Combobox.List>
          <Combobox.Empty>No matching statuses.</Combobox.Empty>
        </Combobox.Content>
      </Combobox>
    </Toolbar>
```


---

### Tooltip

Accessible popup that shows additional information on hover/focus. Wrap your app or section with `<TooltipProvider>` to enable delay grouping.

**Type:** component

**Import:** `import { Tooltip } from "@cloudflare/kumo";`

**Category:** Overlay

**Props:**

- `side`: enum [default: top]
  - `"top"`: Tooltip appears above the trigger
  - `"bottom"`: Tooltip appears below the trigger
  - `"left"`: Tooltip appears to the left of the trigger
  - `"right"`: Tooltip appears to the right of the trigger
- `className`: string
  Additional CSS classes
- `children`: ReactNode
  Child elements
- `content`: ReactNode (required)
  Content to display in the tooltip

**Colors (kumo tokens used):**

`bg-kumo-base`, `fill-kumo-arrow-edge`, `fill-kumo-arrow-stroke`, `fill-kumo-base`, `outline-kumo-line`, `text-kumo-default`

**Sub-Components:**

This is a compound component. Use these sub-components:

#### Tooltip.Provider

Groups multiple tooltips so that after the first tooltip is shown, switching to another skips the open delay. Place once at your app root or layout.

Props:
- `delay`: number [default: 600] - How long to wait (ms) before opening a tooltip once the pointer enters the trigger.
- `closeDelay`: number [default: 0] - How long to wait (ms) before closing a tooltip.
- `timeout`: number [default: 400] - Grace period (ms) during which a just-closed tooltip's delay is skipped when another tooltip opens.

Usage:
```tsx
<TooltipProvider>
  <App />
</TooltipProvider>
```


**Examples:**

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


---

### InputArea

Multi-line textarea input with Input variants and InputArea-specific dimensions

**Type:** component

**Import:** `import { InputArea } from "@cloudflare/kumo (synthetic - uses Input component)";`

**Category:** Input

**Props:**

- `size`: enum [default: base]
  - `"xs"`: Extra small for compact UIs
  - `"sm"`: Small for secondary fields
  - `"base"`: Default size
  - `"lg"`: Large for prominent fields
- `variant`: enum [default: default]
  Visual variant of the textarea.
- `label`: ReactNode
  Label content for the textarea (enables Field wrapper) — can be a string or any React node.
- `labelTooltip`: ReactNode
  Tooltip content to display next to the label via an info icon.
- `description`: ReactNode
  Helper text displayed below the textarea.
- `error`: string | { message: ReactNode; match: FieldErrorMatch }
  Error message or validation error object.
- `autoResize`: boolean [default: false]
  Automatically resize the textarea based on its content.
- `minRows`: number [default: 1]
  Minimum number of rows to display when `autoResize` is enabled.
- `maxRows`: number
  Maximum number of rows to grow to when `autoResize` is enabled; content beyond this scrolls.
- `onValueChange`: (value: string) => void
  Callback fired with the new string value on every change.

**Styling:**

- **Size Variants:**
  - `xs`:
  - `sm`:
  - `base`:
  - `lg`:

## Quick Reference

**Components by Category:**
- **Other:** Autocomplete, CloudflareLogo, DatePicker, Label, Link, SensitiveInput, Sidebar, Table, TableOfContents, TagInput, Toolbar, DeleteResource
- **Display:** Badge, Breadcrumbs, Code, Collapsible, Empty, LayerCard, Meter, Text
- **Feedback:** Banner, Loader, Toasty
- **Data Visualization:** BubbleMap, Chart, ChoroplethMap, GlobeMap, SankeyChart, TimeseriesChart
- **Action:** Button, ButtonGroup, ClipboardText, InlineCopyText
- **Input:** Checkbox, Combobox, DateRangePicker, Field, Input, InputGroup, Radio, Select, Slider, Switch
- **Navigation:** CommandPalette, MenuBar, Pagination, Tabs
- **Overlay:** Dialog, DropdownMenu, LayerDialog, Popover, Tooltip
- **Layout:** Grid, Surface, PageHeader, ResourceListPage
