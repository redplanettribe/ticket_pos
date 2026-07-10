# @ticket-pos/ui

Shared design system for Ticket POS Staff and Storefront apps.

Built with Tailwind CSS v4 and shadcn/ui-style primitives.
Canonical usage guidelines live in [docs/design/README.md](../../docs/design/README.md).

## Usage

Add the workspace dependency:

```json
"@ticket-pos/ui": "workspace:*"
```

Import global styles in the app CSS entrypoint:

```css
@import "@ticket-pos/ui/globals.css";
@source "../../../packages/ui/src";
@source "../";
```

Import components:

```tsx
import { Button, Card, Input } from "@ticket-pos/ui";
```

## Adding components

`components.json` is configured for future `shadcn` CLI use.
New primitives should be added under `src/components/ui/` and exported from `src/index.ts`.
