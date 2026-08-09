# manga-tracker-desktop

Desktop app (Wails v2, Go + React) that makes the local-first manga tracker installable by
someone who does not program. One download, and the browser extension is one click away.

**Current state.** The window works: it finds the backend on this machine and shows the
dashboard it serves. The installer and the settings screen are not written yet — do not
describe them here as done.

Sibling repos, consumed but never merged into this one:
- `../manga-tracker-api` — the backend. Runs as a **system service**, not as a child of this
  app.
- `../manga-tracker-dashboard` — the UI the window shows.
- `../manga-tracker-extension` — what the app helps install into the user's browsers.

## What this app is, and what it deliberately is not

Three responsibilities, and nothing else:

1. **Installer.** Copies the backend tree into the user's data directory, registers the
   system service and starts it.
2. **Window.** Shows the dashboard served by that backend.
3. **Settings.** Opt-in sync, and a per-browser button to install the extension.

It **does not own the backend process**. That is the load-bearing decision of the whole
design: if the backend only lived while the app was open, the extension could not record a
chapter with the app closed — which is exactly when someone reads manga. The backend is a
launchd job on macOS and a scheduled task on Windows, the same way it already runs today.

The consequence worth stating: this app can be closed, or never opened, and tracking keeps
working. It is a control panel, not a runtime.

## Layout
- `main.go` — Wails entry point: window options, embedded assets, bindings
- `app.go` — the bound struct; methods here are callable from the frontend. Wiring only:
  anything worth testing lives under `internal/`
- `internal/backend/` — finds the running backend (`GET /health` over 5150-5159, matching on
  `service`). Pure Go with no Wails import, so `go test` reaches it
- `internal/servicecli/` — spawns the backend's bundled `service.js` and reads its JSON.
  Registering a launchd agent or a Windows task correctly is already written and tested in
  `manga-tracker-api/deploy/lib`; this calls it rather than reimplementing it in Go
- `internal/payload/` — the backend, a Bun and the extension build, embedded with `go:embed`
  and extracted on first launch. `dist/` holds only `VERSION` in git: a development build
  carries nothing, `Available()` says so, and `wails dev` keeps working
- `frontend/` — React 19 + Vite 8 + TypeScript 7 (same stack as the dashboard), built with
  **Bun**; `wails.json` points `frontend:install`/`frontend:build` at it
- `frontend/wailsjs/` — generated bindings (`wails build`/`wails dev` regenerate them)
- `internal/browsers/` — which Chromium browsers are installed, and opening **one specific
  one**: someone whose default is Safari still wants the extension in Brave
- `internal/syncurl/` — refuses a connection string that cannot work, and **assembles** one out
  of separately typed fields, returning a code the window turns into a sentence
- `build/` — icons and platform packaging metadata; `build/bin/` is the output (gitignored)
- `sources.json` — the commits of the sibling repos a release bundles, and the Bun version.
  Pinned to commits, not `main`: a tag has to be rebuildable, and a broken commit landing in a
  sibling repo must not silently ship.
  **A change here that depends on a sibling repo bumps its pin in the same PR.** Three releases
  in a row needed a follow-up commit for this, each caught only by reading the pin by hand
  before tagging — and the failure it produces is not a build error but an app that ships and
  then does not work: the bundled CLI answering a question the app has stopped asking

## Commands
- Dev: `wails dev` · Build: `wails build`
- Frontend alone: `cd frontend && bun run build`
- Go: `go vet ./...` · `go test ./...`

## Rules
- **Never vendor the sibling repos.** They are consumed as released artifacts; a copy of
  their source in here would rot the day one of them changes.
- **Nothing personal ships.** `manga-tracker-api/deploy/azure.json` names the author's own
  resource group and vault. It is not a secret, but it has no business inside an artifact
  someone else installs: `deploy/` is excluded from what gets packaged, except the service
  modules the installer reuses (`deploy/lib/{macos,windows,platform}.ts`).
- **Sync is off by default and never shared.** With no credentials the library lives in the
  local SQLite file, there is no automatic sync and no button to trigger one — the backend
  and dashboard already behave that way with `MONGODB_URL` unset. A user who wants sync
  enters **their own** credentials, stored in the system keystore (Keychain / DPAPI). The
  author's Key Vault cascade is not distributed.
- **The backend's port is not 5150 by convention.** The installer picks a free port in
  **5150-5159** — the range the extension probes — and writes it into the service's
  environment. Widening the range means changing it in the extension too.
- The extension's Web Store URL is **configuration, not code**, so the day Google approves
  it the app switches to one-click installation without shipping a new version.
- **`go:embed` does not keep permissions.** Everything comes back read-only, so the extracted
  Bun is chmod'ed explicitly — without it the install fails with "permission denied" on a file
  that is plainly there.
- The payload's version marker is written **last**. A run that dies halfway leaves none, so
  the next launch extracts again instead of trusting a half-written tree.
- **An update is `stop` → extract → `restart`, in that order**, and it happens at startup, not
  behind a button. The backend is a service running out of the tree being replaced: Windows
  will not unlink a running executable, and on both systems it would otherwise keep executing
  the old code until the next login. `stop` failing is *not* fatal — a payload shipped before
  that command existed answers "unknown command", and refusing to update then would strand
  exactly the people an update is for. Which is why extraction falls back to **renaming** the
  old tree aside (`runtime.old`, cleared on a later launch) when it cannot delete it: Windows
  allows the rename it denies the unlink.
- **A credential never goes into a command line.** `CallWithSecret` puts it on the service
  control's stdin; `Call` is for everything else. An argument is readable by every process on
  the machine (`ps`, Task Manager) for as long as the command runs, which is the rule
  manga-tracker-api already holds for `az`. Tests assert the value is absent from the recorded
  arguments, not merely present on stdin.
- **A password is percent-encoded by `net/url`, never by hand.** A MongoDB URI is a URL, so a
  password holding `@`, `:`, `/`, `?`, `#` or `%` breaks one it is pasted into — and the failure
  is an authentication error, not a parse error, so nothing on screen points at the cause. That
  is the whole reason `syncurl.Build` exists and why the fields form is the default.
- **User-facing reasons cross the Go boundary as codes, never as sentences.** Every word
  someone reads is written in `frontend/src`, in one language. A Go error string that reaches
  the screen is technical detail under a sentence the window wrote, not the message itself.
- Measured, so nobody has to guess: the real payload is **80 MB, 485 files, extracted in
  ~130 ms**, and a second launch skips it in microseconds. The app binary is ~96 MB.

## Engineering standards
- Every feature ships with its tests. `go vet` + `go test` + the frontend build must pass on
  macOS and Windows before declaring work done; report real results.
- Handle errors explicitly at boundaries: a bound method returns an error the frontend can
  render, never a silent failure.
- UI strings are Spanish; code, identifiers and comments are English.
