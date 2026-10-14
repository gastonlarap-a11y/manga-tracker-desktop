import type { main } from "../wailsjs/go/models";

/**
 * What the window says about an export, from the codes `ExportLibrary`
 * returns. Pure, so every outcome is tested without rendering.
 */

export type BackupFormat = "json" | "csv";

/** The save dialog's words for each format; Go only shows them. */
export const DIALOG_LABELS: Record<BackupFormat, main.ExportLabels> = {
  json: {
    title: "Guardar un respaldo de la biblioteca",
    fileKind: "Respaldo (JSON)",
  },
  csv: {
    title: "Guardar la biblioteca como planilla",
    fileKind: "Planilla (CSV)",
  },
};

const PROBLEMS: Record<string, string> = {
  "no-backend":
    "El servicio no está respondiendo, así que no hay de dónde sacar la biblioteca.",
  unsupported:
    "El servidor que está corriendo es de una versión que todavía no sabe exportar.",
  download: "No se pudo bajar la biblioteca; no se guardó nada.",
  write: "No se pudo escribir el archivo ahí. Probá en otra carpeta.",
};

const megabytes = new Intl.NumberFormat("es", { maximumFractionDigits: 1 });

/** "240 kB", "3,2 MB": how big the file came out, to the nearest useful unit. */
export function sizeLabel(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 1024)} kB`;
  }
  return `${megabytes.format(bytes / (1024 * 1024))} MB`;
}

/**
 * The line under the buttons, or null for nothing to say — a cancelled dialog
 * is someone changing their mind, not something that went wrong.
 */
export function backupLine(
  outcome: main.ExportOutcome,
): { tone: "good" | "bad"; text: string } | null {
  switch (outcome.state) {
    case "saved":
      return {
        tone: "good",
        text: `Guardado en ${outcome.path} (${sizeLabel(outcome.bytes)}).`,
      };
    case "cancelled":
      return null;
    default:
      // "failed", and any state this window does not know yet: never "saved".
      return {
        tone: "bad",
        text: PROBLEMS[outcome.problem] ?? "No se pudo exportar la biblioteca.",
      };
  }
}
