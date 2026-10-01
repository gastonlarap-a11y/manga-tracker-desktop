import { Download, RefreshCw, X } from "lucide-react";
import { useEffect, useState } from "react";
import {
  CheckForUpdate,
  DismissUpdate,
  OpenUpdatePage,
  SetUpdateChecks,
} from "../wailsjs/go/main/App";
import type { main } from "../wailsjs/go/models";
import { canCheckAgain, noticeFor, updateLine } from "./updateStatus";

/** What a failed action says; the Go side returns codes, never sentences. */
const UPDATE_PROBLEMS: Record<string, string> = {
  "no-update": "No hay una versión nueva para descargar.",
  "bad-version": "Esa no es una versión.",
};

function problemOf(reason: unknown): string {
  const code = String(reason);
  return UPDATE_PROBLEMS[code] ?? `No se pudo completar. ${code}`;
}

/**
 * The corner notice: a newer release, the way to its page, and a way to close
 * it for that release. Over the dashboard, bottom left — clear of the floating
 * gear and of anything the dashboard puts at the top — and only when there is
 * something to say.
 */
export function UpdateNotice({
  status,
  onDismissed,
}: {
  status: main.UpdateStatus | null;
  onDismissed: (version: string) => void;
}) {
  const [problem, setProblem] = useState("");
  const version = noticeFor(status);
  if (version === null) {
    return null;
  }

  return (
    <aside className="update-notice" aria-label="Versión nueva">
      <Download aria-hidden="true" />
      <span className="update-text">
        Hay una versión nueva: <strong>{version}</strong>
      </span>
      <button
        type="button"
        className="action primary"
        onClick={() => {
          OpenUpdatePage().catch((reason: unknown) =>
            setProblem(problemOf(reason)),
          );
        }}
      >
        Descargar
      </button>
      <button
        type="button"
        className="icon-button"
        aria-label="No volver a avisar de esta versión"
        title="No volver a avisar de esta versión"
        onClick={() => {
          DismissUpdate(version)
            .then(() => onDismissed(version))
            .catch((reason: unknown) => setProblem(problemOf(reason)));
        }}
      >
        <X aria-hidden="true" />
      </button>
      {problem !== "" && <p className="update-problem">{problem}</p>}
    </aside>
  );
}

/**
 * The settings screen's section: where this build stands, the download when
 * there is one, asking again, and turning the check off — which stops the one
 * request the app makes to GitHub on its own.
 */
export function UpdatesSection() {
  const [status, setStatus] = useState<main.UpdateStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [problem, setProblem] = useState("");

  useEffect(() => {
    let current = true;
    // Remembered for this launch on the Go side, so this answers at once.
    CheckForUpdate(false)
      .then((found) => {
        if (current) {
          setStatus(found);
        }
      })
      .catch((reason: unknown) => {
        if (current) {
          setProblem(problemOf(reason));
        }
      });
    return () => {
      current = false;
    };
  }, []);

  async function checkAgain(): Promise<void> {
    setChecking(true);
    setProblem("");
    try {
      setStatus(await CheckForUpdate(true));
    } catch (reason) {
      setProblem(problemOf(reason));
    } finally {
      setChecking(false);
    }
  }

  async function setChecks(on: boolean): Promise<void> {
    setProblem("");
    try {
      await SetUpdateChecks(on);
      // Turned on, it asks now rather than at the next launch.
      setStatus(await CheckForUpdate(on));
    } catch (reason) {
      setProblem(problemOf(reason));
    }
  }

  return (
    <section className="card">
      <h3>
        <RefreshCw aria-hidden="true" />
        Actualizaciones
      </h3>
      {status === null ? (
        problem === "" && <p className="detail">Comprobando…</p>
      ) : (
        <>
          <p className="detail">{updateLine(status)}</p>
          {status.state === "unknown" && status.problem !== "" && (
            <p className="reason">{status.problem}</p>
          )}
          <div className="row">
            {status.state === "available" && (
              <button
                type="button"
                className="action primary"
                onClick={() => {
                  OpenUpdatePage().catch((reason: unknown) =>
                    setProblem(problemOf(reason)),
                  );
                }}
              >
                <Download aria-hidden="true" />
                Descargar {status.latest}
              </button>
            )}
            {canCheckAgain(status) && (
              <button
                type="button"
                className="action"
                disabled={checking}
                onClick={() => void checkAgain()}
              >
                {checking ? "Buscando…" : "Buscar de nuevo"}
              </button>
            )}
          </div>
          {status.state !== "development" && (
            <label className="update-toggle">
              <input
                type="checkbox"
                checked={status.state !== "off"}
                onChange={(event) => void setChecks(event.target.checked)}
              />
              Buscar versiones nuevas al abrir la app
            </label>
          )}
          <p className="detail update-privacy">
            Para saberlo, la app le pregunta a GitHub una vez por apertura. No
            se envía nada tuyo; GitHub solo ve la dirección desde la que llega
            la pregunta.
          </p>
        </>
      )}
      {problem !== "" && <p className="detail bad">{problem}</p>}
    </section>
  );
}
