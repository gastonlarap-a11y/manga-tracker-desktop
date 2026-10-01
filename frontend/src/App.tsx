import {
  BookOpen,
  CirclePause,
  CircleQuestionMark,
  CircleX,
  Download,
  Info,
  LoaderCircle,
  type LucideIcon,
  Settings,
  TriangleAlert,
  Wrench,
} from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import {
  Install,
  Look,
  OpenChapter,
  StartService,
} from "../wailsjs/go/main/App";
import type { installer } from "../wailsjs/go/models";
import { hello, readFrameMessage } from "./bridge";
import { SettingsDialog } from "./Settings";
import "./App.css";

/**
 * How long the dashboard has to answer the greeting before this window stops
 * counting on it for the settings button. Generous: the greeting goes out when
 * the frame has loaded, so all that is left is one round of messages.
 */
const READY_TIMEOUT_MS = 3000;

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

/**
 * Whether the dashboard in the frame has said it carries the settings button.
 * Until it does, the window cannot assume it — "silent" is a dashboard older
 * than the bridge, and gets a button from this window instead.
 */
type Bridge = "waiting" | "ready" | "silent";

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

function viewOf(state: installer.State): View {
  switch (state.kind) {
    case "running":
      return { kind: "connected", baseUrl: state.baseUrl };
    case "installable":
    case "stopped":
    case "noPayload":
      return { kind: state.kind };
    case "setupFailed":
      return { kind: "setupFailed", detail: state.detail };
    default:
      // "unknown", or a kind this window does not know yet. Either way it
      // cannot say what is installed — and must not guess "development
      // build", which is what the old fallthrough did.
      return { kind: "unknown" };
  }
}

export function App({ translucent }: { translucent: boolean }) {
  const [view, setView] = useState<View>({ kind: "looking" });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [bridge, setBridge] = useState<Bridge>("waiting");
  const frame = useRef<HTMLIFrameElement>(null);
  const readyTimer = useRef<number | undefined>(undefined);

  // Chapter links inside the dashboard are cross-origin `target="_blank"`
  // anchors, and Wails implements no handler for a new-window request: the
  // click did nothing at all. The dashboard forwards them here instead, and
  // asks for the settings the same way.
  const embedOrigin =
    view.kind === "connected" ? new URL(view.baseUrl).origin : null;
  useEffect(() => {
    if (embedOrigin === null) {
      return;
    }
    const onMessage = (event: MessageEvent) => {
      // Only the frame we are showing gets to ask this window for anything.
      if (event.origin !== embedOrigin) {
        return;
      }
      const message = readFrameMessage(event.data);
      if (message === null) {
        return;
      }
      switch (message.kind) {
        case "openExternal":
          // The scheme is validated on the Go side, in one place. Its only
          // refusal is a link that is not http(s), which is refused on
          // purpose and has nowhere on screen to be explained: the click came
          // from inside the frame.
          void OpenChapter(message.url).catch((reason: unknown) =>
            console.warn(
              "[manga-tracker] chapter link not opened",
              message.url,
              reason,
            ),
          );
          return;
        case "ready":
          setBridge(message.features.includes("settings") ? "ready" : "silent");
          return;
        case "openSettings":
          setSettingsOpen(true);
          return;
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [embedOrigin]);

  useEffect(() => () => window.clearTimeout(readyTimer.current), []);

  const look = useCallback(() => {
    setView({ kind: "looking" });
    // Handled on both paths: a Look that rejected used to leave the window on
    // "Buscando…" forever. A rejection says nothing about what is installed,
    // so it lands on "unknown" rather than on any screen that claims to know.
    void Look()
      .then((state) => setView(viewOf(state)))
      .catch(() => setView({ kind: "unknown" }));
  }, []);

  /**
   * Looks again without tearing the window down first. Closing the settings
   * used to go through "Buscando…", which unmounted the frame and reloaded the
   * whole dashboard to land on the same page; now the frame stays unless the
   * answer is actually different.
   */
  const refresh = useCallback(() => {
    void Look()
      .then((state) => {
        const next = viewOf(state);
        setView((current) =>
          current.kind === "connected" &&
          next.kind === "connected" &&
          current.baseUrl === next.baseUrl
            ? current
            : next,
        );
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

  const openSettings = useCallback(() => setSettingsOpen(true), []);

  return (
    <div className="app">
      {view.kind === "connected" ? (
        <>
          {/* The dashboard is served by the backend, so inside this frame it
              is same-origin with its own API — no CORS involved, and no copy
              of the dashboard shipped in this app that could fall out of
              date. It is the whole window: its bar is the only one. */}
          <iframe
            ref={frame}
            className="dashboard"
            src={view.baseUrl}
            title="Manga Tracker"
            onLoad={() => {
              // Here the target origin is exact and known, unlike the reply:
              // this window is `wails://wails/`, which is not a usable
              // targetOrigin.
              frame.current?.contentWindow?.postMessage(
                hello(translucent),
                new URL(view.baseUrl).origin,
              );
              setBridge((current) => (current === "ready" ? current : "waiting"));
              window.clearTimeout(readyTimer.current);
              readyTimer.current = window.setTimeout(
                () =>
                  setBridge((current) =>
                    current === "waiting" ? "silent" : current,
                  ),
                READY_TIMEOUT_MS,
              );
            }}
          />
          {bridge === "silent" && (
            <button
              type="button"
              className="floating-gear"
              onClick={openSettings}
              aria-label="Configuración"
              title="Configuración"
            >
              <Settings aria-hidden="true" />
            </button>
          )}
        </>
      ) : (
        <StatusScreen
          view={view}
          onLook={look}
          onInstall={install}
          onStart={start}
          onSettings={openSettings}
        />
      )}
      {settingsOpen && (
        <SettingsDialog
          onClose={() => {
            setSettingsOpen(false);
            // Installing the extension or turning sync on changes what the
            // dashboard shows, so the window checks again on close.
            refresh();
          }}
        />
      )}
    </div>
  );
}

type Tone = "neutral" | "busy" | "good" | "warn" | "bad";

interface Screen {
  readonly Icon: LucideIcon;
  readonly tone: Tone;
  readonly title: string;
  readonly body?: ReactNode;
  readonly reason?: string;
  readonly actions?: ReactNode;
}

function StatusScreen({
  view,
  onLook,
  onInstall,
  onStart,
  onSettings,
}: {
  view: Exclude<View, { kind: "connected" }>;
  onLook: () => void;
  onInstall: () => void;
  onStart: () => void;
  onSettings: () => void;
}) {
  const lookAgain = (
    <button type="button" className="action" onClick={onLook}>
      Buscar de nuevo
    </button>
  );
  const screen = screenFor(view, { lookAgain, onLook, onInstall, onStart });
  const { Icon } = screen;

  return (
    <main className="screen">
      <button
        type="button"
        className="screen-gear"
        onClick={onSettings}
        aria-label="Configuración"
        title="Configuración"
      >
        <Settings aria-hidden="true" />
      </button>
      <section className="status-card" aria-live="polite">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            <BookOpen />
          </span>
          Manga Tracker
        </div>
        <span className={`status-icon tone-${screen.tone}`} aria-hidden="true">
          <Icon />
        </span>
        <h1>{screen.title}</h1>
        {screen.body}
        {screen.reason && <p className="detail reason">{screen.reason}</p>}
        {screen.actions && <div className="row">{screen.actions}</div>}
      </section>
    </main>
  );
}

function screenFor(
  view: Exclude<View, { kind: "connected" }>,
  handlers: {
    lookAgain: ReactNode;
    onLook: () => void;
    onInstall: () => void;
    onStart: () => void;
  },
): Screen {
  switch (view.kind) {
    case "looking":
      return {
        Icon: LoaderCircle,
        tone: "busy",
        title: "Buscando Manga Tracker en tu computadora…",
      };
    case "installable":
      return {
        Icon: Download,
        tone: "neutral",
        title: "Todavía no está instalado en esta computadora.",
        body: (
          <p className="detail">
            Se instala en un momento y queda funcionando solo: arranca cada vez
            que inicies sesión, sin que tengas que abrir esta ventana.
          </p>
        ),
        actions: (
          <button
            type="button"
            className="action primary"
            onClick={handlers.onInstall}
          >
            Instalar Manga Tracker
          </button>
        ),
      };
    case "installing":
      return {
        Icon: LoaderCircle,
        tone: "busy",
        title: "Instalando y arrancando el servicio…",
      };
    case "stopped":
      return {
        Icon: CirclePause,
        tone: "warn",
        title: "Manga Tracker está instalado, pero ahora mismo no responde.",
        body: (
          <p className="detail">
            Puede estar reiniciándose después de una actualización. Si acabás de
            actualizar, esperá unos segundos y buscá de nuevo. Si no, arrancalo
            desde acá: tu biblioteca y tu configuración quedan como están.
          </p>
        ),
        actions: (
          <>
            <button
              type="button"
              className="action primary"
              onClick={handlers.onStart}
            >
              Arrancar el servicio
            </button>
            {handlers.lookAgain}
          </>
        ),
      };
    case "starting":
      return {
        Icon: LoaderCircle,
        tone: "busy",
        title: "Arrancando el servicio…",
      };
    case "unknown":
      return {
        Icon: CircleQuestionMark,
        tone: "warn",
        title: "No pude averiguar si Manga Tracker está instalado.",
        body: (
          <p className="detail">
            No respondió nada, y tampoco pude preguntarle al servicio. No te
            ofrezco instalarlo porque no sé qué hay en esta computadora, y
            reinstalar encima de una instalación que funciona borraría su
            configuración.
          </p>
        ),
        actions: handlers.lookAgain,
      };
    case "noPayload":
      return {
        Icon: Wrench,
        tone: "neutral",
        title: "Esta versión no trae el servidor incluido.",
        body: (
          <p className="detail">
            Es una compilación de desarrollo. Para verla funcionar, arrancá el
            backend por tu cuenta, o usá el instalador publicado.
          </p>
        ),
        actions: handlers.lookAgain,
      };
    case "setupFailed":
      return {
        Icon: TriangleAlert,
        tone: "bad",
        title:
          "No pude preparar el servidor de Manga Tracker en esta computadora.",
        body: (
          <p className="detail">
            Tu biblioteca no se tocó: vive fuera de la carpeta que se estaba
            escribiendo. Suele ser falta de espacio en el disco, o un permiso
            sobre la carpeta de datos. Buscar de nuevo lo vuelve a intentar.
          </p>
        ),
        reason: view.detail,
        actions: handlers.lookAgain,
      };
    case "refused":
      return {
        Icon: Info,
        tone: "neutral",
        title: view.message,
        actions: handlers.lookAgain,
      };
    case "failed":
      return {
        Icon: CircleX,
        tone: "bad",
        title: FAILURES[view.action],
        reason: view.reason,
        actions: (
          <button type="button" className="action" onClick={handlers.onLook}>
            Reintentar
          </button>
        ),
      };
  }
}
