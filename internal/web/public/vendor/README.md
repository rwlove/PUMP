# Vendored front-end assets

Served from the binary (`go:embed public/*`) at `/fs/public/vendor/` so the
web UI makes no requests to a third-party CDN or font host. Each directory
holds the upstream file(s) unmodified unless noted, plus the upstream license.

| Directory | Package (npm) | Files | License |
|---|---|---|---|
| `bootstrap/` | `bootstrap@5.3.3` | `dist/js/bootstrap.bundle.min.js` | MIT |
| `bootswatch-cosmo/` | `aceberg-bootswatch-fork@5.3.3-2` | `dist/cosmo/bootstrap.min.css` (modified, see below) | MIT |
| `bootstrap-icons/` | `bootstrap-icons@1.11.3` | `font/bootstrap-icons.css`, `font/fonts/*` | MIT |
| `chart.js/` | `chart.js@4.4.1` | `dist/chart.umd.js` | MIT |
| `source-sans-pro/` | `@fontsource/source-sans-pro@5.3.0` | latin + latin-ext woff2, weights 300/400/700 | SIL OFL 1.1 |

**Modification:** the cosmo theme CSS began with a Google Fonts `@import` of
Source Sans Pro 300/400/700. That line is replaced with a comment, and
`source-sans-pro/source-sans-pro.css` (built from the fontsource per-weight
CSS, woff2 only) supplies the same faces locally. `layout.html` loads it.

**Upgrading:** `npm pack <package>@<version>`, copy the files listed above,
re-apply the `@import` removal, and update the version comments in
`internal/web/templates/layout.html`. Check the new files for absolute
`http(s)://` URLs in `@import`/`url()` before committing.
