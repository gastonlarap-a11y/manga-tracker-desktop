# manga-tracker-desktop

Desktop app for the local-first personal manga reading tracker. Its reason to exist: today,
using this project means installing Bun, cloning three repositories, generating a Prisma
client, migrating a database by hand, writing a service definition and loading an unpacked
extension in developer mode. That is not something you can hand to a friend.

This app replaces all of it with **one download**.

> **Status.** The window works: it finds the backend running on this machine and shows the
> dashboard. The installer and the settings screen are not written yet — see `AGENTS.md`.

## How the pieces fit

| Piece | Where it lives | Who runs it |
|---|---|---|
| Backend (`manga-tracker-api`) | User's data directory | **A system service** — launchd on macOS, Task Scheduler on Windows |
| Dashboard (`manga-tracker-dashboard`) | Served by the backend | Shown inside this app's window |
| Extension (`manga-tracker-extension`) | The user's browsers | Installed from the Chrome Web Store, or guided step by step |
| This app | `/Applications` or `Program Files` | The user, when they want to look at something |

The backend is **not** a child process of this app. If it were, closing the app would stop
tracking — and the app is closed exactly when someone is reading. So it runs on its own, and
this app is a control panel over it: it can be closed, or never opened, and readings keep
being recorded.

## Finding the backend

The app does not assume a port. It probes `GET /health` on **5150-5159** and takes the first
that identifies itself as `manga-tracker-api`, which is what lets an installer pick whatever
port is free on that machine. The browser extension probes the same range, so widening it
means widening it in both places.

The dashboard is then shown in a frame pointing at that backend, rather than bundled into
this app. Two things follow: the dashboard is same-origin with its own API (no CORS
involved), and a dashboard update reaches the window without rebuilding this app.

## Requirements

To build it (a user installing the release needs none of this):

- [Go](https://go.dev) 1.26+
- [Bun](https://bun.sh) 1.3+
- [Wails CLI v2](https://wails.io/docs/gettingstarted/installation): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## Commands

| Command | What it does |
|---|---|
| `wails dev` | Development mode with hot reload |
| `wails build` | Production build into `build/bin/` |
| `go vet ./...` / `go test ./...` | Go checks |
| `cd frontend && bun run build` | Frontend alone |

Wails v2 is deliberate: v3 is still in alpha, and this app does not need anything it adds.
Because the backend runs as a system service, the app's job is small enough for the stable
line.

## Installing a release

The app is **not signed** with a paid certificate, so both systems warn the first time. Once
per machine:

- **macOS** — the build is ad-hoc signed (`codesign -s -`, which Wails does automatically),
  so it does not fail with "the app is damaged" on Apple Silicon. Gatekeeper still asks:
  System Settings → Privacy & Security → **Open anyway**.
- **Windows** — SmartScreen: **More info** → **Run anyway**.

Neither store is used: the Mac App Store and Microsoft Store both charge recurring fees for
what is a personal project.

## License

Personal project. No license granted yet.
