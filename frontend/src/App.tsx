import { useCallback, useEffect, useState } from "react";
import { Install, Look } from "../wailsjs/go/main/App";
import { SettingsDialog } from "./Settings";
import "./App.css";

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
  | { kind: "noPayload" }
  | { kind: "refused"; message: string }
  | { kind: "failed"; reason: string };

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

  const look = useCallback(() => {
    setView({ kind: "looking" });
    void Look().then((state) => {
      if (state.kind === "running") {
        setView({ kind: "connected", baseUrl: state.baseUrl });
      } else if (state.kind === "installable") {
        setView({ kind: "installable" });
      } else {
        setView({ kind: "noPayload" });
      }
    });
  }, []);

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
        setView({ kind: "failed", reason: String(reason) }),
      );
  }, []);

  return (
    <div className="app">
      <header className="bar">
        <span className="title">Manga Tracker</span>
        <span className="where">
          {view.kind === "connected" ? view.baseUrl : "sin conexión"}
        </span>
        <button type="button" className="action" onClick={look}>
          Reconectar
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
            <p>No se pudo completar la instalación.</p>
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
            className="dashboard"
            src={view.baseUrl}
            title="Manga Tracker"
          />
        )}
      </main>
    </div>
  );
}

export default App;
