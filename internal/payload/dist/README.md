# Payload

Filled by the release workflow, not by hand. In git this directory holds only
`VERSION` and this file, so `go:embed` has something to compile against and a
development build stays buildable — `payload.Available()` returns false there,
and the app simply does not offer to install anything.

A release writes:

| Path | What |
|---|---|
| `app/` | everything the backend needs, **flat**: `bun`, `index.js`, `service.js`, `migrations/`, `node_modules/`, `public/` |
| `extension/` | the built MV3 extension, for loading unpacked while the store review is pending |
| `VERSION` | the release tag — the app re-extracts when it changes |

`app/` is flat because that is the shape the service CLI expects: it looks for
the interpreter at `<app-dir>/bun`, runs `index.js` with `<app-dir>` as the
working directory, and points `MIGRATIONS_DIR` at `<app-dir>/migrations`. A
prettier layout here would only mean translating between the two.
