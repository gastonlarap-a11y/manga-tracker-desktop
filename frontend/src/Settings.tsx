import { useCallback, useEffect, useState } from "react";
import {
  ClearSync,
  OpenInBrowser,
  RevealExtension,
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
  | { kind: "connected"; usesSrv?: boolean }
  // Saved, but the backend was still restarting when we looked. Kept apart from
  // "failed": telling someone their sync did not connect when it did is worse
  // than telling them to look again in a moment.
  | { kind: "unsettled" }
  | { kind: "failed"; detail: string };

function outcomeOf(outcome: main.SyncOutcome): Saving {
  if (!outcome.settled) {
    return { kind: "unsettled" };
  }
  return outcome.connected
    ? { kind: "connected", usesSrv: outcome.usesSrv }
    : { kind: "failed", detail: outcome.lastError };
}

export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [settings, setSettings] = useState<main.Settings | null>(null);
  const [wantsSync, setWantsSync] = useState(false);
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

              {wantsSync && (
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
                    {settings.syncConfigured && (
                      <button type="button" className="action" onClick={turnOff}>
                        Apagar
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
                {manualOpen ? "▾" : "▸"} Todavía no está aprobada: cargarla a mano
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
