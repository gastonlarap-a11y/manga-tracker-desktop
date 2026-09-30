import { useCallback, useEffect, useRef, useState } from "react";
import {
  Install,
  Look,
  OpenChapter,
  StartService,
} from "../wailsjs/go/main/App";
import { SettingsDialog } from "./Settings";
import "./App.css";

// The two halves of the link bridge, matching src/lib/embed.ts in
// manga-tracker-dashboard. The dashboard installs nothing until it is greeted,
// so an embedder that never says hello gets the plain browser behaviour.
const EMBED_HELLO = "manga-tracker:embed-hello";
const OPEN_EXTERNAL = "manga-tracker:open-external";

/**
 * What the window is showing. A union rather than a pile of booleans:
 * "connected but with no address" and "installing and already running at once"
 * are states that must not be representable.
 */
type View =
  | { kind: "looking" }
  | { kind: "connected"; baseUrl: string }
  | { kind: "installable" }
  | { kind: "installing" }
  // Installed, but nothing answered: restarting after an update, or stopped.
  | { kind: "stopped" }
  | { kind: "starting" }
  // Nothing answered and the service could not be asked either.
  | { kind: "unknown" }
  | { kind: "noPayload" }
  // A release that could not put its server on disk, or bring it back after
  // an update. Never shown as "noPayload": that screen says this is a
  // development build, which for someone holding a release is simply untrue.
  | { kind: "setupFailed"; detail: string }
  | { kind: "refused"; message: string }
  // Which button failed, because the sentence depends on it: a service that
  // would not start used to be reported as an installation that did not finish.
  | { kind: "failed"; action: "install" | "start"; reason: string };

const FAILURES: Record<"install" | "start", string> = {
  install: "No se pudo completar la instalación.",
  start: "No se pudo arrancar el servicio.",
};

/**
 * A refusal is the guard working, not a fault, so it reads as an explanation
 * rather than an error.
 */
const REFUSALS: Record<string, string> = {
  running:
    "Ya hay un Manga Tracker funcionando en esta computadora, así que no toqué nada.",
  installed:
    "Esta computadora ya tiene Manga Tracker instalado, aunque ahora esté detenido. No sobrescribo una instalación existente.",
};

function App() {
  const [view, setView] = useState<View>({ kind: "looking" });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const frame = useRef<HTMLIFrameElement>(null);

  // Chapter links inside the dashboard are cross-origin `target="_blank"`
  // anchors, and Wails implements no handler for a new-window request: the
  // click did nothing at all. The dashboard forwards them here instead.
  const embedOrigin = view.kind === "connected" ? new URL(view.baseUrl).origin : null;
  useEffect(() => {
    if (embedOrigin === null) {
      return;
    }
    const onMessage = (event: MessageEvent) => {
      // Only the frame we are showing gets to ask this window to open things.
      if (event.origin !== embedOrigin) {
        return;
      }
      const data: unknown = event.data;
      if (
        typeof data !== "object" ||
        data === null ||
        (data as { type?: unknown }).type !== OPEN_EXTERNAL
      ) {
        return;
      }
      const url = (data as { url?: unknown }).url;
      if (typeof url !== "string") {
        return;
      }
      // The scheme is validated on the Go side, in one place. Its only refusal
      // is a link that is not http(s), which is refused on purpose and has
      // nowhere on screen to be explained: the click came from inside the frame.
      void OpenChapter(url).catch((reason: unknown) =>
        console.warn("[manga-tracker] chapter link not opened", url, reason),
      );
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [embedOrigin]);

  const look = useCallback(() => {
    setView({ kind: "looking" });
    // Handled on both paths: a Look that rejected used to leave the window on
    // "Buscando…" forever. A rejection says nothing about what is installed,
    // so it lands on "unknown" rather than on any screen that claims to know.
    void Look()
      .then((state) => {
        switch (state.kind) {
          case "running":
            setView({ kind: "connected", baseUrl: state.baseUrl });
            return;
          case "installable":
          case "stopped":
          case "noPayload":
            setView({ kind: state.kind });
            return;
          case "setupFailed":
            setView({ kind: "setupFailed", detail: state.detail });
            return;
          default:
            // "unknown", or a kind this window does not know yet. Either way
            // it cannot say what is installed — and must not guess
            // "development build", which is what the old fallthrough did.
            setView({ kind: "unknown" });
        }
      })
      .catch(() => setView({ kind: "unknown" }));
  }, []);

  const start = useCallback(() => {
    setView({ kind: "starting" });
    void StartService()
      .then(look)
      .catch((reason: unknown) =>
        setView({ kind: "failed", action: "start", reason: String(reason) }),
      );
  }, [look]);

  useEffect(look, [look]);

  const install = useCallback(() => {
    setView({ kind: "installing" });
    void Install()
      .then((outcome) => {
        if (outcome.refused !== "") {
          setView({
            kind: "refused",
            message: REFUSALS[outcome.refused] ?? outcome.refused,
          });
          return;
        }
        setView({ kind: "connected", baseUrl: outcome.baseUrl });
      })
      .catch((reason: unknown) =>
        setView({ kind: "failed", action: "install", reason: String(reason) }),
      );
  }, []);

  return (
    <div className="app">
      <header className="bar">
        <span className="title">Manga Tracker</span>
        <span className="where">
          {view.kind === "connected" ? view.baseUrl : "sin conexión"}
        </span>
        {/* "Buscar de nuevo" y no "Reconectar": esto vuelve a sondear el
            backend de esta computadora, que contesta perfectamente bien
            mientras su conexión a la base de datos está caída. Quien tenía la
            sincronización rota lo apretaba esperando que reintentara la
            conexión, y no pasaba nada porque no es lo que hace. Reintentar la
            base está en Configuración, al lado del estado que lo dice. */}
        <button type="button" className="action" onClick={look}>
          Buscar de nuevo
        </button>
        <button
          type="button"
          className="gear"
          onClick={() => setSettingsOpen(true)}
          aria-label="Configuración"
          title="Configuración"
        >
          ⚙
        </button>
      </header>
      {settingsOpen && (
        <SettingsDialog
          onClose={() => {
            setSettingsOpen(false);
            // Installing the extension or turning sync on changes what the
            // dashboard shows, so the window catches up on close.
            look();
          }}
        />
      )}
      <main className="body">
        {view.kind === "looking" && (
          <p className="message">Buscando Manga Tracker en tu computadora…</p>
        )}

        {view.kind === "installable" && (
          <div className="message">
            <p>Todavía no está instalado en esta computadora.</p>
            <p className="detail">
              Se instala en un momento y queda funcionando solo: arranca cada vez
              que inicies sesión, sin que tengas que abrir esta ventana.
            </p>
            <button type="button" className="action primary" onClick={install}>
              Instalar Manga Tracker
            </button>
          </div>
        )}

        {view.kind === "installing" && (
          <p className="message">Instalando y arrancando el servicio…</p>
        )}

        {view.kind === "stopped" && (
          <div className="message">
            <p>Manga Tracker está instalado, pero ahora mismo no responde.</p>
            <p className="detail">
              Puede estar reiniciándose después de una actualización. Si acabás
              de actualizar, esperá unos segundos y buscá de nuevo. Si no,
              arrancalo desde acá: tu biblioteca y tu configuración quedan como
              están.
            </p>
            <div className="row">
              <button type="button" className="action primary" onClick={start}>
                Arrancar el servicio
              </button>
              <button type="button" className="action" onClick={look}>
                Buscar de nuevo
              </button>
            </div>
          </div>
        )}

        {view.kind === "starting" && (
          <p className="message">Arrancando el servicio…</p>
        )}

        {view.kind === "unknown" && (
          <div className="message">
            <p>No pude averiguar si Manga Tracker está instalado.</p>
            <p className="detail">
              No respondió nada, y tampoco pude preguntarle al servicio. No te
              ofrezco instalarlo porque no sé qué hay en esta computadora, y
              reinstalar encima de una instalación que funciona borraría su
              configuración.
            </p>
            <button type="button" className="action" onClick={look}>
              Buscar de nuevo
            </button>
          </div>
        )}

        {view.kind === "noPayload" && (
          <div className="message">
            <p>Esta versión no trae el servidor incluido.</p>
            <p className="detail">
              Es una compilación de desarrollo. Para verla funcionar, arrancá el
              backend por tu cuenta, o usá el instalador publicado.
            </p>
            <button type="button" className="action" onClick={look}>
              Buscar de nuevo
            </button>
          </div>
        )}

        {view.kind === "setupFailed" && (
          <div className="message">
            <p>No pude preparar el servidor de Manga Tracker en esta computadora.</p>
            <p className="detail">
              Tu biblioteca no se tocó: vive fuera de la carpeta que se estaba
              escribiendo. Suele ser falta de espacio en el disco, o un permiso
              sobre la carpeta de datos. Buscar de nuevo lo vuelve a intentar.
            </p>
            <p className="detail reason">{view.detail}</p>
            <button type="button" className="action" onClick={look}>
              Buscar de nuevo
            </button>
          </div>
        )}

        {view.kind === "refused" && (
          <div className="message">
            <p>{view.message}</p>
            <button type="button" className="action" onClick={look}>
              Buscar de nuevo
            </button>
          </div>
        )}

        {view.kind === "failed" && (
          <div className="message">
            <p>{FAILURES[view.action]}</p>
            <p className="detail reason">{view.reason}</p>
            <button type="button" className="action" onClick={look}>
              Reintentar
            </button>
          </div>
        )}

        {view.kind === "connected" && (
          // The dashboard is served by the backend, so inside this frame it is
          // same-origin with its own API — no CORS involved, and no copy of the
          // dashboard shipped in this app that could fall out of date.
          <iframe
            ref={frame}
            className="dashboard"
            src={view.baseUrl}
            title="Manga Tracker"
            onLoad={() => {
              // The greeting that turns the dashboard's link bridge on. Here
              // the target origin is exact and known, unlike the reply: this
              // window is `wails://wails/`, which is not a usable targetOrigin.
              frame.current?.contentWindow?.postMessage(
                { type: EMBED_HELLO },
                new URL(view.baseUrl).origin,
              );
            }}
          />
        )}
      </main>
    </div>
  );
}

export default App;
