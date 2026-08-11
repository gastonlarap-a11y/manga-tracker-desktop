import { useCallback, useEffect, useState } from "react";
import {
  ClearSync,
  OpenInBrowser,
  RevealExtension,
  SetChapterBrowser,
  Settings as LoadSettings,
  SetSync,
  SetSyncFields,
  UseStoredSync,
} from "../wailsjs/go/main/App";
import type { main } from "../wailsjs/go/models";
import "./Settings.css";

/**
 * Every sentence the user reads lives here, in one language. The Go side
 * returns codes rather than prose precisely so this file is the only place
 * wording has to be reviewed.
 */
const SYNC_PROBLEMS: Record<string, string> = {
  empty: "Escribí la dirección de tu base de datos.",
  srv: "Las direcciones que empiezan con mongodb+srv:// no funcionan en Windows, porque no se resuelven los registros SRV. Usá la forma directa: mongodb://servidor:puerto/?tls=true",
  notMongo: "Eso no parece una dirección de MongoDB. Tiene que empezar con mongodb://",
  noHost: "Falta el servidor en la dirección.",
  credentialsInAddress:
    "La dirección ya trae un usuario y una contraseña adentro. Dejá los campos de abajo vacíos, o sacáselos a la dirección.",
  noUser: "Pusiste una contraseña pero no un usuario.",
  srvUnresolved:
    "No pude averiguar en qué servidor está ese cluster: el DNS no respondió. Revisá la conexión a internet, o pegá la dirección directa (mongodb://servidor:puerto/…) si la tenés.",
};

/**
 * Cómo se está cargando la conexión. Dos formas, no una bandera: quien tiene la
 * cadena entera la pega, y quien recibió servidor, usuario y contraseña por
 * separado los escribe — y en ese caso la contraseña se codifica al armar la
 * URL, que es lo que hoy falla en silencio.
 */
type Entry =
  | { kind: "fields"; address: string; user: string; password: string }
  | { kind: "paste"; url: string };

const EMPTY_FIELDS: Entry = {
  kind: "fields",
  address: "",
  user: "",
  password: "",
};

type Saving =
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
  // Saved, but the backend was still restarting when we looked. Kept apart from
  // "failed": telling someone their sync did not connect when it did is worse
  // than telling them to look again in a moment.
  | { kind: "unsettled" }
  | { kind: "failed"; detail: string };

/**
 * "hace 3 minutos", from an RFC 3339 timestamp. Written here rather than in Go
 * for the same reason every other sentence is: the wording lives in one file,
 * in one language.
 */
function sinceLabel(iso: string): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) {
    return "";
  }
  const minutes = Math.floor((Date.now() - at) / 60_000);
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

function outcomeOf(outcome: main.SyncOutcome): Saving {
  if (!outcome.settled) {
    return { kind: "unsettled" };
  }
  return outcome.connected
    ? {
        kind: "connected",
        usesSrv: outcome.usesSrv,
        convertedTo: outcome.converted ? outcome.host : "",
        secretInConfig: outcome.secretInConfig,
      }
    : { kind: "failed", detail: outcome.lastError };
}

export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [settings, setSettings] = useState<main.Settings | null>(null);
  const [wantsSync, setWantsSync] = useState(false);
  // The form is a thing you open, not the default view. With sync already
  // running there is nothing to fill in, and showing the fields anyway is what
  // made someone ask whether both tabs had to be completed.
  const [editing, setEditing] = useState(false);
  const [entry, setEntry] = useState<Entry>(EMPTY_FIELDS);
  const [showPassword, setShowPassword] = useState(false);
  const [database, setDatabase] = useState("");
  const [saving, setSaving] = useState<Saving>({ kind: "idle" });
  const [manualOpen, setManualOpen] = useState(false);

  const load = useCallback(() => {
    void LoadSettings().then((loaded) => {
      setSettings(loaded);
      setWantsSync(loaded.syncConfigured);
    });
  }, []);

  useEffect(load, [load]);

  const save = useCallback(() => {
    setSaving({ kind: "saving" });
    const stored =
      entry.kind === "paste"
        ? SetSync(entry.url, database)
        : SetSyncFields(entry.address, entry.user, entry.password, database);
    void stored
      .then((outcome) => {
        if (outcome.problem !== "") {
          setSaving({
            kind: "rejected",
            message: SYNC_PROBLEMS[outcome.problem] ?? outcome.problem,
          });
          return;
        }
        setSaving(outcomeOf(outcome));
        // Back to the summary: the form did its job, and leaving it open with
        // the credential still in it invites a second save nobody meant.
        setEditing(false);
        setEntry(EMPTY_FIELDS);
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "failed", detail: String(reason) }),
      );
  }, [entry, database, load]);

  const reuse = useCallback(() => {
    setSaving({ kind: "saving" });
    void UseStoredSync(database)
      .then((outcome) => {
        setSaving(outcomeOf(outcome));
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "failed", detail: String(reason) }),
      );
  }, [database, load]);

  const turnOff = useCallback(() => {
    setSaving({ kind: "saving" });
    void ClearSync()
      .then(() => {
        setWantsSync(false);
        setEditing(false);
        setEntry(EMPTY_FIELDS);
        setShowPassword(false);
        setSaving({ kind: "idle" });
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "failed", detail: String(reason) }),
      );
  }, [load]);

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: the backdrop is a courtesy;
    // the dialog itself is reachable and closable from the keyboard.
    <div className="backdrop" onClick={onClose}>
      <div
        className="dialog"
        role="dialog"
        aria-modal="true"
        aria-label="Configuración"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="dialog-head">
          <h2>Configuración</h2>
          <button
            type="button"
            className="action"
            onClick={onClose}
            aria-label="Cerrar"
          >
            ✕
          </button>
        </header>

        {settings === null ? (
          <p className="detail">Leyendo la configuración…</p>
        ) : (
          <>
            <section>
              <h3>Sincronización</h3>
              <p className="detail">
                Por defecto tus lecturas viven sólo en esta computadora. Si
                querés tenerlas en varias, o un respaldo fuera del equipo, podés
                usar una base de datos propia. Es opcional.
              </p>

              {settings.problem !== "" ? (
                <p className="detail bad">
                  No pude preguntarle al servicio cómo está, así que no sé si
                  hay sincronización configurada.
                  <span className="reason"> {settings.problem}</span>
                </p>
              ) : (
                !settings.installed && (
                  <p className="detail">
                    Primero hay que instalar Manga Tracker en esta computadora.
                  </p>
                )
              )}

              {/* Already synchronising, and not in the middle of changing it:
                  the answer to "am I connected?" comes before any form. */}
              {settings.syncConfigured && !editing && (
                <div className="fields">
                  <p
                    className={`detail ${
                      !settings.syncLive.asked
                        ? ""
                        : settings.syncLive.connected
                          ? "good"
                          : "bad"
                    }`}
                  >
                    {!settings.syncLive.asked
                      ? "Configurada, pero no pude preguntarle al servidor cómo está."
                      : settings.syncLive.connected
                        ? `Conectada${
                            sinceLabel(settings.syncLive.lastSyncAt) === ""
                              ? ""
                              : ` · sincronizado ${sinceLabel(settings.syncLive.lastSyncAt)}`
                          }`
                        : "Configurada, pero no está conectando."}
                    {settings.syncLive.asked &&
                      !settings.syncLive.connected &&
                      settings.syncLive.lastError !== "" && (
                        <span className="reason">
                          {" "}
                          {settings.syncLive.lastError}
                        </span>
                      )}
                  </p>
                  <dl className="status">
                    <dt>Servidor</dt>
                    <dd>
                      <code>{settings.syncHost}</code>
                    </dd>
                    <dt>Base de datos</dt>
                    <dd>
                      <code>{settings.syncDb}</code>
                    </dd>
                    <dt>Contraseña</dt>
                    <dd>
                      {settings.secretInConfig
                        ? "en la configuración del servicio (archivo protegido)"
                        : "en el llavero del sistema"}
                    </dd>
                  </dl>
                  <div className="row">
                    <button
                      type="button"
                      className="action"
                      onClick={() => {
                        setEditing(true);
                        setWantsSync(true);
                        setSaving({ kind: "idle" });
                      }}
                    >
                      Cambiar
                    </button>
                    <button type="button" className="action" onClick={turnOff}>
                      Apagar
                    </button>
                  </div>
                </div>
              )}

              {settings.hasStoredCredential && !settings.syncConfigured && (
                <div className="fields">
                  <p className="detail">
                    Esta computadora ya tiene guardada una conexión de una
                    configuración anterior. Podés seguir usándola sin volver a
                    escribirla.
                  </p>
                  <div className="row">
                    <button
                      type="button"
                      className="action primary"
                      onClick={reuse}
                      disabled={saving.kind === "saving"}
                    >
                      {saving.kind === "saving"
                        ? "Conectando…"
                        : "Usar la que ya tenía"}
                    </button>
                  </div>
                </div>
              )}

              {(!settings.syncConfigured || editing) && (
                <div className="choice">
                  <label>
                    <input
                      type="radio"
                      name="sync"
                      checked={!wantsSync}
                      onChange={() => setWantsSync(false)}
                    />
                    Sólo en esta computadora
                  </label>
                  <label>
                    <input
                      type="radio"
                      name="sync"
                      checked={wantsSync}
                      disabled={!settings.installed}
                      onChange={() => setWantsSync(true)}
                    />
                    También en una base de datos mía
                  </label>
                </div>
              )}

              {wantsSync && (!settings.syncConfigured || editing) && (
                <div className="fields">
                  <div className="row tabs">
                    <button
                      type="button"
                      className={`link${entry.kind === "fields" ? " chosen" : ""}`}
                      onClick={() => setEntry(EMPTY_FIELDS)}
                    >
                      Escribir los datos
                    </button>
                    <button
                      type="button"
                      className={`link${entry.kind === "paste" ? " chosen" : ""}`}
                      onClick={() => setEntry({ kind: "paste", url: "" })}
                    >
                      Pegar la dirección completa
                    </button>
                  </div>

                  {entry.kind === "fields" ? (
                    <>
                      <label className="field">
                        <span>Servidor</span>
                        <input
                          value={entry.address}
                          onChange={(event) =>
                            setEntry({ ...entry, address: event.target.value })
                          }
                          placeholder="servidor:10260/?tls=true"
                          spellCheck={false}
                        />
                      </label>
                      <label className="field">
                        <span>Usuario</span>
                        <input
                          value={entry.user}
                          onChange={(event) =>
                            setEntry({ ...entry, user: event.target.value })
                          }
                          spellCheck={false}
                          autoComplete="off"
                        />
                      </label>
                      <label className="field">
                        <span>Contraseña</span>
                        <input
                          type={showPassword ? "text" : "password"}
                          value={entry.password}
                          onChange={(event) =>
                            setEntry({ ...entry, password: event.target.value })
                          }
                          spellCheck={false}
                          autoComplete="off"
                        />
                      </label>
                      <p className="detail">
                        Escrita acá, una contraseña con{" "}
                        <code>@</code>, <code>:</code>, <code>/</code> o{" "}
                        <code>%</code> funciona. Metida a mano dentro de la
                        dirección, no.
                      </p>
                    </>
                  ) : (
                    <label className="field">
                      <span>Dirección</span>
                      <input
                        type={showPassword ? "text" : "password"}
                        value={entry.url}
                        onChange={(event) =>
                          setEntry({ kind: "paste", url: event.target.value })
                        }
                        placeholder="mongodb://usuario:contraseña@servidor:10260/?tls=true"
                        spellCheck={false}
                        autoComplete="off"
                      />
                    </label>
                  )}

                  <button
                    type="button"
                    className="link"
                    onClick={() => setShowPassword(!showPassword)}
                  >
                    {showPassword ? "Ocultar" : "Mostrar"} la contraseña
                  </button>

                  <label className="field">
                    <span>Base de datos</span>
                    <input
                      value={database}
                      onChange={(event) => setDatabase(event.target.value)}
                      placeholder="mangatracker"
                      spellCheck={false}
                    />
                  </label>
                  <div className="row">
                    <button
                      type="button"
                      className="action primary"
                      onClick={save}
                      disabled={saving.kind === "saving"}
                    >
                      {saving.kind === "saving" ? "Guardando…" : "Guardar y conectar"}
                    </button>
                    {editing && (
                      <button
                        type="button"
                        className="action"
                        onClick={() => {
                          setEditing(false);
                          setEntry(EMPTY_FIELDS);
                          setSaving({ kind: "idle" });
                        }}
                      >
                        Cancelar
                      </button>
                    )}
                  </div>
                </div>
              )}

              {/* Outside the form on purpose: the same outcomes apply whether
                  the credential was typed or carried over from the keystore. */}
              {saving.kind === "rejected" && (
                <p className="detail bad">{saving.message}</p>
              )}
              {saving.kind === "connected" && (
                <>
                  <p className="detail good">Conectado. Ya está sincronizando.</p>
                  {saving.secretInConfig === true && (
                    <p className="detail">
                      En esta computadora el servicio no puede leer el llavero
                      del sistema cuando arranca, así que la contraseña quedó
                      guardada en su archivo de configuración, que sólo tu
                      usuario puede abrir. Es donde estuvo siempre; el llavero
                      era la mejora, y acá no se pudo.
                    </p>
                  )}
                  {saving.convertedTo !== undefined &&
                    saving.convertedTo !== "" && (
                      <p className="detail">
                        Guardé la dirección directa —{" "}
                        <code>{saving.convertedTo}</code> — en lugar de la{" "}
                        <code>mongodb+srv://</code> que pegaste. Es la misma
                        base; así también funciona en Windows, donde las
                        direcciones <code>srv</code> nunca conectan.
                      </p>
                    )}
                  {saving.usesSrv === true && (
                    <p className="detail bad">
                      Ojo: esa dirección empieza con <code>mongodb+srv://</code>.
                      Acá funciona, pero en Windows nunca va a conectar. Para
                      usarla en las dos, convertila a la forma directa
                      (<code>mongodb://servidor:puerto/?tls=true</code>).
                    </p>
                  )}
                </>
              )}
              {saving.kind === "unsettled" && (
                <p className="detail">
                  Se guardó. El servidor está reiniciando con los datos nuevos y
                  todavía no contestó — mirá <strong>Estado</strong> acá abajo en
                  unos segundos.
                </p>
              )}
              {saving.kind === "failed" && (
                <p className="detail bad">
                  Se guardó, pero no pudo conectar.
                  {saving.detail !== "" && (
                    <span className="reason"> {saving.detail}</span>
                  )}
                </p>
              )}
            </section>

            <section>
              <h3>Extensión del navegador</h3>
              <p className="detail">
                Es la que detecta qué capítulo estás leyendo. Sin ella, la
                biblioteca no se llena sola.
              </p>
              {settings.browsers.length === 0 ? (
                <p className="detail">
                  No encontré Chrome, Brave ni Edge en esta computadora.
                </p>
              ) : (
                <div className="row">
                  {settings.browsers.map((browser) => (
                    <button
                      type="button"
                      key={browser.id}
                      className="action"
                      onClick={() => void OpenInBrowser(browser.id)}
                    >
                      Instalar en {browser.name}
                    </button>
                  ))}
                </div>
              )}

              <button
                type="button"
                className="link"
                onClick={() => setManualOpen(!manualOpen)}
              >
                {manualOpen ? "▾" : "▸"} Cargar una copia local en vez de la
                publicada
              </button>
              {manualOpen && (
                <ol className="steps">
                  <li>
                    Abrí la carpeta de la extensión:{" "}
                    <button
                      type="button"
                      className="link"
                      onClick={() => void RevealExtension()}
                    >
                      mostrar en el Finder
                    </button>
                    <code>{settings.extensionDir}</code>
                  </li>
                  <li>
                    En el navegador, entrá a <code>chrome://extensions</code> (en
                    Brave, <code>brave://extensions</code>).
                  </li>
                  <li>
                    Activá <strong>Modo de desarrollador</strong> y apretá{" "}
                    <strong>Cargar descomprimida</strong>, eligiendo esa carpeta.
                  </li>
                </ol>
              )}
            </section>

            <section>
              <h3>Con qué navegador abrir tus mangas</h3>
              <p className="detail">
                Conviene el mismo donde instalaste la extensión: si un capítulo
                se abre en otro, esa lectura no queda registrada.
              </p>
              {!settings.chapterBrowserKnown ? (
                <p className="detail reason">
                  No pude leer tu preferencia guardada, así que no sé cuál
                  elegiste. Volvé a elegir uno acá abajo.
                </p>
              ) : null}
              <select
                className="action"
                value={settings.chapterBrowser}
                onChange={(event) => {
                  const chosen = event.target.value;
                  void SetChapterBrowser(chosen).then(load);
                }}
              >
                <option value="">El navegador por defecto del sistema</option>
                {settings.browsers.map((browser) => (
                  <option key={browser.id} value={browser.id}>
                    {browser.name}
                  </option>
                ))}
                {/* The browser was uninstalled since it was chosen. Listed, or
                    the box would quietly read "por defecto del sistema" while
                    the stored preference still says otherwise. */}
                {settings.chapterBrowser !== "" &&
                !settings.browsers.some(
                  (browser) => browser.id === settings.chapterBrowser,
                ) ? (
                  <option value={settings.chapterBrowser}>
                    {settings.chapterBrowser} (ya no está instalado)
                  </option>
                ) : null}
              </select>
            </section>

            <section>
              <h3>Estado</h3>
              <dl className="status">
                <dt>Servicio</dt>
                <dd>
                  {!settings.hasPayload
                    ? "compilación de desarrollo: no incluye el servidor"
                    : !settings.asked
                      ? "no pude preguntarle"
                      : settings.installed
                        ? `instalado · puerto ${settings.port}`
                        : "no instalado en esta computadora"}
                </dd>
                <dt>Datos</dt>
                <dd>
                  <code>{settings.dataDir}</code>
                </dd>
                <dt>Versión</dt>
                <dd>{settings.version}</dd>
              </dl>
              {settings.problem !== "" && (
                <p className="detail bad">
                  No pude consultar el estado del servicio.
                  <span className="reason"> {settings.problem}</span>
                </p>
              )}
            </section>
          </>
        )}
      </div>
    </div>
  );
}
