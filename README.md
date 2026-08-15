# manga-tracker-desktop

Desktop app for the local-first personal manga reading tracker. Its reason to exist: today,
using this project means installing Bun, cloning three repositories, generating a Prisma
client, migrating a database by hand, writing a service definition and loading an unpacked
extension in developer mode. That is not something you can hand to a friend.

This app replaces all of it with **one download**.

## Install it

**→ [Latest release](https://github.com/gastonlarap-a11y/manga-tracker-desktop/releases/latest)**

| System | File | First run |
|---|---|---|
| **macOS** (Apple Silicon — M1 or newer) | `Manga-Tracker-macOS-AppleSilicon.dmg` | Open it, drag **Manga Tracker** onto **Applications**. macOS cannot verify the developer: System Settings → Privacy & Security → **Open Anyway**. Once per machine. |
| **Windows 10/11** (64-bit) | `Manga-Tracker-Windows-Setup.exe` | Run it — it installs for your user and asks for no administrator prompt. Windows may still block it, in one of [three ways](#why-the-warnings-in-detail), and only one of them offers **Run anyway**. |

Neither is signed with a paid certificate, and neither store is used: both charge recurring
fees for what is a personal project. macOS is Apple Silicon only — the database driver is
per architecture, and building for a machine nobody involved owns would double what has to be
verified.

Opening it is the whole setup. It writes the backend out, registers it with the system and
waits until it answers; from then on it starts at every login on its own. There is no account
and nothing to configure.

The browser extension is the one piece that is not automatic — it lives in the browser, not on
disk. The gear icon installs it, or walks through loading it unpacked while the store review is
pending. Full walkthrough:
[manga-tracker-extension](https://github.com/gastonlarap-a11y/manga-tracker-extension#testing-it-end-to-end-about-five-minutes).

### Updating

Install the new version over the old one — there is nothing to uninstall. Open the app
afterwards: it notices the backend beside it is a version behind, stops it, replaces it and
starts it again. Your library is untouched; it lives outside the directory that gets replaced.

## Where your data lives

In one SQLite file on your own computer, under the application-support directory the settings
screen shows. That is the only copy the app needs, and everything works with nothing else
configured.

**Syncing to a database of your own is optional and off by default.** Its purpose is having the
same library on two computers, and a copy that survives losing one of them. It is a MongoDB-
compatible connection *you* own — the app never has a server of its own to offer. Even with it
on, SQLite still answers every read and write: sync is a background convergence, never
something a page waits for, so it cannot make the library depend on connectivity.

## How the pieces fit

| Piece | Where it lives | Who runs it |
|---|---|---|
| Backend (`manga-tracker-api`) | User's data directory | **A system service** — launchd on macOS, Task Scheduler on Windows |
| Dashboard (`manga-tracker-dashboard`) | Served by the backend | Shown inside this app's window |
| Extension (`manga-tracker-extension`) | The user's browsers | Installed from the Chrome Web Store, or guided step by step |
| This app | `/Applications`, or `%LOCALAPPDATA%\Programs` on Windows | The user, when they want to look at something |

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

## Why the warnings, in detail

The macOS build is ad-hoc signed (`codesign -s -`, which Wails does automatically), so Apple
Silicon does not refuse it outright as damaged — but it is not notarised, which needs a paid
Apple account, so Gatekeeper still asks once.

Windows is the side that is worth spelling out, because the usual advice — *More info → Run
anyway* — describes only one of three things it does, and someone who hits either of the other
two is told to look for a link that is not on their screen:

| What you see | What it is | The way out |
|---|---|---|
| **"Windows protected your PC"**, with **More info** | SmartScreen in its normal *warn* mode | **Run anyway** |
| The same kind of box with **no More info at all**, only a button that closes it | **Smart App Control** (Windows 11), which by design [cannot be bypassed for an individual app](https://support.microsoft.com/en-us/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions) — or SmartScreen set to *block* instead of *warn* | Windows Security → App & browser control → turn Smart App Control off (it can be turned back on), or set *Check apps and files* to **Warn** |
| The file simply disappears after downloading | Defender quarantined it | Windows Security → Virus & threat protection → Protection history → **Allow on device** |

All three have the same cause: the installer is not signed with a paid certificate, and this
project does not buy one. Worth being precise about what buying one would and would not do —
Microsoft's own [SmartScreen guidance for developers](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
says reputation is earned through download volume even *with* a certificate, and that since
March 2024 not even an EV certificate skips the prompt. A certificate is US$219–685 a year, is
now required to live on a physical HSM token, and would still leave the first downloads warned
about.

What the installer does do is stop making itself look worse than it is. It installs per-user
into `%LOCALAPPDATA%\Programs` and requests no elevation, so there is no *Unknown publisher* UAC
prompt — and elevation, writing executables and registering a login task are together what a
heuristic scanner reads as a dropper. That lowers the odds; it is not a signature, and does not
promise the warning is gone.

## License

Personal project. No license granted yet.
