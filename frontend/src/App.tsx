import { useCallback, useEffect, useState } from "react";
import { FindBackend } from "../wailsjs/go/main/App";
import "./App.css";

/**
 * What the window is showing. A union rather than a pair of booleans: "found
 * but no URL" and "searching and missing at once" are states that must not be
 * representable.
 */
type Status =
  | { kind: "searching" }
  | { kind: "found"; baseUrl: string }
  | { kind: "missing"; firstPort: number; lastPort: number };

function App() {
  const [status, setStatus] = useState<Status>({ kind: "searching" });

  const look = useCallback(() => {
    setStatus({ kind: "searching" });
    void FindBackend().then((result) =>
      setStatus(
        result.found
          ? { kind: "found", baseUrl: result.baseUrl }
          : {
              kind: "missing",
              firstPort: result.firstPort,
              lastPort: result.lastPort,
            },
      ),
    );
  }, []);

  useEffect(look, [look]);

  return (
    <div className="app">
      <header className="bar">
        <span className="title">Manga Tracker</span>
        <span className="where">
          {status.kind === "found" ? status.baseUrl : "sin conexión"}
        </span>
        <button type="button" className="action" onClick={look}>
          Reconectar
        </button>
      </header>
      <main className="body">
        {status.kind === "searching" && (
          <p className="message">Buscando Manga Tracker en tu computadora…</p>
        )}
        {status.kind === "missing" && (
          <div className="message">
            <p>No encontré Manga Tracker corriendo en esta computadora.</p>
            <p className="detail">
              Busqué en los puertos {status.firstPort} a {status.lastPort}. Si
              recién lo instalaste, puede tardar unos segundos en arrancar.
            </p>
            <button type="button" className="action" onClick={look}>
              Buscar de nuevo
            </button>
          </div>
        )}
        {status.kind === "found" && (
          // The dashboard is served by the backend, so inside this frame it is
          // same-origin with its own API — no CORS involved, and no copy of the
          // dashboard shipped in this app that could fall out of date.
          <iframe className="dashboard" src={status.baseUrl} title="Manga Tracker" />
        )}
      </main>
    </div>
  );
}

export default App;
