# Brand assets

Canonical source-of-truth for the **Multiticketing** product brand. These files
are the origin for every derived icon; they are not imported at runtime.

| File | What it is | Derived into |
| --- | --- | --- |
| `favicon-light.svg` | Blue tile (`#0e84c1`) with white mark. True vector. | Each app's `app/icon.svg`, the multi-size `app/favicon.ico`, `app/apple-icon.png`, and `public/icon-{192,512}.png`. |
| `favicon-dark.svg` | White tile with blue mark. True vector. | The `<LogoMark>` path in `../components/logo.tsx` (the mark shape is drawn in `currentColor`). |
| `logo-marketing.svg` | Large white mark on transparent (raster wrapped in SVG). | Marketing / social (OG) use only — too heavy and not square for favicons. |

Brand blue is **`#0e84c1`**. The design-system `--primary`/`--ring` tokens use a
slightly darkened twin (`#0d7bb3`) so white button text clears WCAG AA; the exact
brand blue stays on the mark, favicon, and the Staff `theme-color`.

## Regenerating icons

The mark geometry lives in two places by necessity — the SVG files here and the
inline `d=` in `logo.tsx`. When the mark changes, update both, then regenerate
the raster/ICO icons from `favicon-light.svg`:

```sh
for s in 16 32 48 180 192 512; do
  inkscape favicon-light.svg --export-type=png --export-filename=icon-$s.png -w $s -h $s
done
convert icon-16.png icon-32.png icon-48.png favicon.ico
# then copy icon.svg/favicon.ico/apple-icon.png into each app's app/ dir
# and icon-192/512.png into each app's public/ dir
```
