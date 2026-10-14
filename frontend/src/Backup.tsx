import { Archive, FileBraces, FileSpreadsheet } from "lucide-react";
import { useState } from "react";
import { ExportLibrary } from "../wailsjs/go/main/App";
import type { main } from "../wailsjs/go/models";
import { type BackupFormat, backupLine, DIALOG_LABELS } from "./backupOutcome";

type BackupState =
  | { kind: "idle" }
  | { kind: "working"; format: BackupFormat }
  | { kind: "done"; outcome: main.ExportOutcome }
  | { kind: "error"; detail: string };

/**
 * The settings screen's section for keeping a copy of the library. Here, not
 * in the dashboard, because the window is what can save a file: a download
 * inside the embedded dashboard does nothing on macOS.
 *
 * Not gated on the service being installed: a backend run from a checkout can
 * export too, and "no-backend" says so when nothing answers.
 */
export function BackupSection() {
  const [state, setState] = useState<BackupState>({ kind: "idle" });
  const working = state.kind === "working";

  async function exportAs(format: BackupFormat): Promise<void> {
    setState({ kind: "working", format });
    try {
      // Blocks through the save dialog and the whole download.
      setState({
        kind: "done",
        outcome: await ExportLibrary(format, DIALOG_LABELS[format]),
      });
    } catch (reason) {
      setState({ kind: "error", detail: String(reason) });
    }
  }

  const line = state.kind === "done" ? backupLine(state.outcome) : null;
  return (
    <section className="card">
      <h3>
        <Archive aria-hidden="true" />
        Tu biblioteca
      </h3>
      <p className="detail">
        Un respaldo guarda todo: cada serie con su estado, sus etiquetas y sus
        otros nombres, y cada capítulo leído con su fecha y su sitio. La planilla
        es una fila por serie, para abrirla en Excel o Google Sheets o llevarla
        a otro tracker que importe por título.
      </p>
      <div className="row">
        <button
          type="button"
          className="action"
          disabled={working}
          onClick={() => void exportAs("json")}
        >
          <FileBraces aria-hidden="true" />
          {state.kind === "working" && state.format === "json"
            ? "Exportando…"
            : "Exportar respaldo (JSON)"}
        </button>
        <button
          type="button"
          className="action"
          disabled={working}
          onClick={() => void exportAs("csv")}
        >
          <FileSpreadsheet aria-hidden="true" />
          {state.kind === "working" && state.format === "csv"
            ? "Exportando…"
            : "Exportar planilla (CSV)"}
        </button>
      </div>
      {line !== null && (
        <p className={`detail ${line.tone}`} role="status">
          {line.text}
          {state.kind === "done" && state.outcome.detail !== "" && (
            <span className="reason"> {state.outcome.detail}</span>
          )}
        </p>
      )}
      {state.kind === "error" && (
        <p className="detail bad" role="alert">
          No se pudo exportar la biblioteca.
          <span className="reason"> {state.detail}</span>
        </p>
      )}
    </section>
  );
}
