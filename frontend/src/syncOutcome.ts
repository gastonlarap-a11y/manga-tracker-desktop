/**
 * What the settings dialog says after each sync action, as pure functions of
 * what Go returned — here rather than inside the component so the wording and
 * the branching behind it are tested without rendering anything.
 */
import type { main } from "../wailsjs/go/models";

/**
 * What was asked for. The same three outcomes — connected, still restarting,
 * did not connect — read differently depending on it: "se guardó" is true of a
 * save and false of a retry, which saves nothing.
 */
export type ConnectAction = "save" | "reuse" | "retry";
export type Action = ConnectAction | "turnOff";

export const NOT_CONNECTED: Record<ConnectAction, string> = {
  save: "Se guardó, pero no pudo conectar.",
  reuse: "Se activó, pero no pudo conectar.",
  retry: "Reintenté y sigue sin conectar.",
};

export const STILL_RESTARTING: Record<ConnectAction, string> = {
  save: "Se guardó. El servidor está reiniciando con los datos nuevos y todavía no contestó",
  reuse: "Se activó. El servidor está reiniciando y todavía no contestó",
  retry: "El servidor está reiniciando y todavía no contestó",
};

/**
 * The action itself failed — nothing was saved, switched or restarted. Kept
 * apart from NOT_CONNECTED, which every rejection used to land on: "se guardó,
 * pero no pudo conectar" over a save that never happened.
 */
export const ACTION_FAILED: Record<Action, string> = {
  save: "No se pudo guardar la conexión.",
  reuse: "No se pudo activar la conexión que ya tenías.",
  retry: "No se pudo reiniciar el servidor para reintentar.",
  turnOff: "No se pudo apagar la sincronización.",
};

export type Saving =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "rejected"; message: string }
  // convertedTo is set when what was stored is not what was pasted: a
  // mongodb+srv:// address resolved into its direct form. Saying so beats
  // letting someone find a different string than the one they typed.
  | {
      kind: "connected";
      usesSrv?: boolean;
      convertedTo?: string;
      // True when the keystore could not be read at startup on this machine and
      // the credential had to go into the service's configuration instead.
      secretInConfig?: boolean;
    }
  // Done, but the backend was still restarting when we looked. Kept apart from
  // "failed": telling someone their sync did not connect when it did is worse
  // than telling them to look again in a moment.
  | { kind: "unsettled"; action: ConnectAction }
  // Done, and it answered: not connected.
  | { kind: "failed"; action: ConnectAction; detail: string }
  // The action itself did not happen.
  | { kind: "error"; action: Action; detail: string };

/** The fields of a SyncOutcome this reads, so a test can pass a plain object. */
export type SyncOutcomeFields = Pick<
  main.SyncOutcome,
  | "settled"
  | "connected"
  | "usesSrv"
  | "converted"
  | "host"
  | "secretInConfig"
  | "lastError"
>;

export function outcomeOf(
  action: ConnectAction,
  outcome: SyncOutcomeFields,
): Saving {
  if (!outcome.settled) {
    return { kind: "unsettled", action };
  }
  return outcome.connected
    ? {
        kind: "connected",
        usesSrv: outcome.usesSrv,
        convertedTo: outcome.converted ? outcome.host : "",
        secretInConfig: outcome.secretInConfig,
      }
    : { kind: "failed", action, detail: outcome.lastError };
}

/**
 * "hace 3 minutos", from an RFC 3339 timestamp. Written here rather than in Go
 * for the same reason every other sentence is: the wording lives in one place,
 * in one language.
 */
export function sinceLabel(iso: string, now: number = Date.now()): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) {
    return "";
  }
  const minutes = Math.floor((now - at) / 60_000);
  if (minutes < 1) {
    return "recién";
  }
  if (minutes < 60) {
    return `hace ${minutes} ${minutes === 1 ? "minuto" : "minutos"}`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `hace ${hours} ${hours === 1 ? "hora" : "horas"}`;
  }
  const days = Math.floor(hours / 24);
  return `hace ${days} ${days === 1 ? "día" : "días"}`;
}

/** The platform's name for its file manager, for the "show the folder" link. */
export function fileManagerLabel(platform: string): string {
  switch (platform) {
    case "darwin":
      return "mostrar en el Finder";
    case "windows":
      return "mostrar en el Explorador de archivos";
    default:
      return "mostrar la carpeta";
  }
}
