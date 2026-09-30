import { useCallback, useEffect, useState } from "react";
import {
  ClearSync,
  DiagnoseSync,
  OpenInBrowser,
  RetrySync,
  RevealExtension,
  SetChapterBrowser,
  Settings as LoadSettings,
  SetSync,
  SetSyncFields,
  UseStoredSync,
} from "../wailsjs/go/main/App";
import type { browsers, main } from "../wailsjs/go/models";
import { Environment } from "../wailsjs/runtime/runtime";
import "./Settings.css";
import {
  ACTION_FAILED,
  fileManagerLabel,
  NOT_CONNECTED,
  outcomeOf,
  STILL_RESTARTING,
  type Saving,
  sinceLabel,
} from "./syncOutcome";

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
 * Por qué no se llega a la base de datos, en las palabras de esta ventana.
 *
 * El backend informa lo que dijo el driver de MongoDB, y "Server selection timed
 * out after 15000 ms" es la misma frase para una lista de direcciones permitidas
 * que quedó vieja, un nombre que ya no existe y una contraseña equivocada. Go
 * prueba la red y manda un código; la explicación —y qué hacer— se escribe acá.
 */
const REACH_REASONS: Record<string, string> = {
  unreachable:
    "No se llega al servidor: los paquetes salen y no vuelve nada. Casi siempre es que la lista de direcciones permitidas de la base de datos ya no incluye la IP de esta computadora, porque las IP domiciliarias cambian solas. Agregá la IP que aparece abajo desde el panel de tu proveedor — o autorizá 0.0.0.0/0 ahí mismo si preferís no repetirlo cada vez que cambie.",
  unresolved:
    "El nombre del servidor ya no existe en el DNS. Puede que el cluster haya cambiado de dirección, o que lo hayan borrado.",
  reachable:
    "Al servidor se llega bien, así que el problema no es la red: revisá el usuario, la contraseña o el nombre de la base de datos.",
  unknown:
    "No pude averiguar si desde esta computadora se llega al servidor. Puede ser que no haya conexión a internet en este momento.",
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
  // Qué acción está corriendo sobre una conexión ya configurada, para que cada
  // botón muestre su propio "…" y no se puedan disparar dos a la vez.
  const [busy, setBusy] = useState<"none" | "retrying">("none");
  const [reach, setReach] = useState("");
  const [address, setAddress] = useState("");
  // Why the settings could not be read, when they could not. Without it a
  // rejected load left "Leyendo la configuración…" on screen for good.
  const [loadError, setLoadError] = useState("");
  // Failures of the one-shot buttons, each shown beside its own section.
  const [extensionNotice, setExtensionNotice] = useState("");
  const [browserNotice, setBrowserNotice] = useState("");
  const [platform, setPlatform] = useState("");

  const load = useCallback(() => {
    setLoadError("");
    void LoadSettings()
      .then((loaded) => {
        setSettings(loaded);
        setWantsSync(loaded.syncConfigured);
      })
      .catch((reason: unknown) => setLoadError(String(reason)));
  }, []);

  useEffect(load, [load]);

  useEffect(() => {
    // Only a label depends on it, and the neutral one is still true.
    void Environment()
      .then((environment) => setPlatform(environment.platform))
      .catch(() => setPlatform(""));
  }, []);

  // Sólo hay algo que diagnosticar cuando el backend contestó que la conexión
  // está caída: una que anda no necesita explicación, y un backend al que no se
  // le pudo preguntar todavía no dijo que hubiera nada mal.
  const down =
    settings !== null &&
    settings.syncConfigured &&
    settings.syncLive.asked &&
    !settings.syncLive.connected;

  // Después del render y no dentro de Settings(): el diagnóstico marca un
  // socket y puede preguntar la IP afuera, así que metido en la carga dejaría
  // el diálogo cerrado justo en el caso en que alguien lo abre para mirar.
  useEffect(() => {
    if (!down) {
      setReach("");
      setAddress("");
      return;
    }
    void DiagnoseSync()
      .then((diagnosis) => {
        setReach(diagnosis.reach);
        setAddress(diagnosis.address);
      })
      // Not asking is not finding out: "unknown" is the honest answer, and
      // it is one the window already knows how to say.
      .catch(() => {
        setReach("unknown");
        setAddress("");
      });
    // Atado a settings y no sólo a `down`: cada acción que falla vuelve a
    // cargarlos, y un diagnóstico de antes del intento describiría un estado
    // que ya no es el que se está mirando.
  }, [down, settings]);

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
        setSaving(outcomeOf("save", outcome));
        // Back to the summary: the form did its job, and leaving it open with
        // the credential still in it invites a second save nobody meant.
        setEditing(false);
        setEntry(EMPTY_FIELDS);
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "error", action: "save", detail: String(reason) }),
      );
  }, [entry, database, load]);

  /**
   * Volver a usar la credencial que ya está en el llavero de esta computadora.
   *
   * Recibe la base de datos en vez de leer la del formulario: se llama también
   * desde una conexión ya configurada —para sacar la contraseña del archivo del
   * servicio y devolverla al llavero— y ahí el formulario está vacío, así que
   * habría cambiado la base a la de por omisión sin que nadie lo pidiera.
   */
  const reuse = useCallback(
    (db: string) => {
      setSaving({ kind: "saving" });
      void UseStoredSync(db)
        .then((outcome) => {
          setSaving(outcomeOf("reuse", outcome));
          load();
        })
        .catch((reason: unknown) =>
          setSaving({ kind: "error", action: "reuse", detail: String(reason) }),
        );
    },
    [load],
  );

  /**
   * Reintentar la conexión a la base, que es distinto de "Reconectar" en la
   * barra de arriba: aquel busca el backend en esta computadora, y el backend
   * contesta perfectamente bien mientras su conexión a la base está caída.
   */
  const retry = useCallback(() => {
    setBusy("retrying");
    setSaving({ kind: "saving" });
    void RetrySync()
      .then((outcome) => {
        setSaving(outcomeOf("retry", outcome));
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "error", action: "retry", detail: String(reason) }),
      )
      .finally(() => setBusy("none"));
  }, [load]);

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
        setSaving({ kind: "error", action: "turnOff", detail: String(reason) }),
      );
  }, [load]);

  const openStore = useCallback((browser: browsers.Browser) => {
    setExtensionNotice("");
    void OpenInBrowser(browser.id).catch((reason: unknown) =>
      setExtensionNotice(`No pude abrir ${browser.name}. ${String(reason)}`),
    );
  }, []);

  const revealExtension = useCallback(() => {
    setExtensionNotice("");
    void RevealExtension().catch((reason: unknown) =>
      setExtensionNotice(`No pude abrir la carpeta. ${String(reason)}`),
    );
  }, []);

  const chooseBrowser = useCallback(
    (chosen: string) => {
      setBrowserNotice("");
      void SetChapterBrowser(chosen)
        .then(load)
        .catch((reason: unknown) =>
          setBrowserNotice(
            String(reason).includes("unknown-browser")
              ? "Ese navegador ya no está instalado, así que no lo guardé."
              : `No pude guardar tu elección. ${String(reason)}`,
          ),
        );
    },
    [load],
  );

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
          loadError === "" ? (
            <p className="detail">Leyendo la configuración…</p>
          ) : (
            <div className="fields">
              <p className="detail bad">
                No pude leer la configuración.
                <span className="reason"> {loadError}</span>
              </p>
              <div className="row">
                <button type="button" className="action" onClick={load}>
                  Reintentar
                </button>
              </div>
            </div>
          )
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

                  {/* Por qué no conecta, probado en vez de deducido del texto
                      del driver. Esa cadena queda arriba como detalle técnico;
                      esta frase es la que dice qué hay que revisar. */}
                  {settings.syncLive.asked &&
                    !settings.syncLive.connected &&
                    reach !== "" && (
                      <p className="detail">
                        {REACH_REASONS[reach] ?? reach}
                      </p>
                    )}

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
                    {reach === "unreachable" && address !== "" && (
                      <>
                        <dt>IP de esta computadora</dt>
                        <dd>
                          <code>{address}</code>
                        </dd>
                      </>
                    )}
                  </dl>
                  <div className="row">
                    {!settings.syncLive.connected && (
                      <button
                        type="button"
                        className="action"
                        onClick={retry}
                        disabled={busy !== "none"}
                      >
                        {busy === "retrying"
                          ? "Reintentando…"
                          : "Reintentar ahora"}
                      </button>
                    )}
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

                  {/* La salida del estado degradado. Antes no había ninguna: la
                      contraseña entraba al archivo del servicio y se quedaba
                      ahí, porque el botón para volver al llavero sólo aparecía
                      con la sincronización apagada. */}
                  {settings.secretInConfig && settings.hasStoredCredential && (
                    <div className="fields">
                      <p className="detail">
                        La contraseña está en el archivo de configuración del
                        servicio porque alguna vez la conexión no levantó. Si ya
                        funciona, podés devolverla al llavero del sistema sin
                        volver a escribirla.
                      </p>
                      <div className="row">
                        <button
                          type="button"
                          className="action"
                          onClick={() => reuse(settings.syncDb)}
                          disabled={saving.kind === "saving"}
                        >
                          Devolver la contraseña al llavero
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Only on an installed machine. The credential survives an
                  uninstall, so the offer appeared before installing too — and
                  taking it wrote a configuration the service control then read
                  as an installation, which the install button refused. */}
              {settings.installed &&
                settings.hasStoredCredential &&
                !settings.syncConfigured && (
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
                      onClick={() => reuse(database)}
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
                          placeholder="servidor:27017/?tls=true"
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
                        placeholder="mongodb://usuario:contraseña@servidor:27017/?tls=true"
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
                  {STILL_RESTARTING[saving.action]} — mirá <strong>Estado</strong>{" "}
                  acá abajo en unos segundos.
                </p>
              )}
              {saving.kind === "failed" && (
                <p className="detail bad">
                  {NOT_CONNECTED[saving.action]}
                  {saving.detail !== "" && (
                    <span className="reason"> {saving.detail}</span>
                  )}
                </p>
              )}
              {saving.kind === "error" && (
                <p className="detail bad">
                  {ACTION_FAILED[saving.action]}
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
                      onClick={() => openStore(browser)}
                    >
                      Instalar en {browser.name}
                    </button>
                  ))}
                </div>
              )}
              {extensionNotice !== "" && (
                <p className="detail bad">{extensionNotice}</p>
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
                      onClick={revealExtension}
                    >
                      {fileManagerLabel(platform)}
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
                onChange={(event) => chooseBrowser(event.target.value)}
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
              {browserNotice !== "" && (
                <p className="detail bad">{browserNotice}</p>
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
