# Payload

Filled by the release workflow, not by hand. In git this directory holds only
`VERSION` and this file, so `go:embed` has something to compile against and a
development build stays buildable — `payload.Available()` returns false there,
and the app simply does not offer to install anything.

A release writes:

| Path | What |
|---|---|
| `backend/` | the bundled server and service CLI, migrations, the native driver, and the dashboard build |
| `runtime/` | the Bun binary for this platform |
| `extension/` | the built MV3 extension, for loading unpacked while the store review is pending |
| `VERSION` | the release tag — the app re-extracts when it changes |
