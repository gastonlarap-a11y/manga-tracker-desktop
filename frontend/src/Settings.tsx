import { useCallback, useEffect, useState } from "react";
import {
  ClearSync,
  OpenInBrowser,
  RevealExtension,
  Settings as LoadSettings,
  SetSync,
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
};

type Saving =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "rejected"; message: string }
  | { kind: "connected" }
  | { kind: "failed"; detail: string };

export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [settings, setSettings] = useState<main.Settings | null>(null);
  const [wantsSync, setWantsSync] = useState(false);
  const [url, setUrl] = useState("");
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
    void SetSync(url, database)
      .then((outcome) => {
        if (outcome.problem !== "") {
          setSaving({
            kind: "rejected",
            message: SYNC_PROBLEMS[outcome.problem] ?? outcome.problem,
          });
          return;
        }
        setSaving(
          outcome.connected
            ? { kind: "connected" }
            : { kind: "failed", detail: outcome.lastError },
        );
        load();
      })
      .catch((reason: unknown) =>
        setSaving({ kind: "failed", detail: String(reason) }),
      );
  }, [url, database, load]);

  const turnOff = useCallback(() => {
    setSaving({ kind: "saving" });
    void ClearSync()
      .then(() => {
        setWantsSync(false);
        setUrl("");
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

              {!settings.installed && (
                <p className="detail">
                  Primero hay que instalar Manga Tracker en esta computadora.
                </p>
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
                  <label className="field">
                    <span>Dirección</span>
                    <input
                      value={url}
                      onChange={(event) => setUrl(event.target.value)}
                      placeholder="mongodb://servidor:puerto/?tls=true"
                      spellCheck={false}
                    />
                  </label>
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

                  {saving.kind === "rejected" && (
                    <p className="detail bad">{saving.message}</p>
                  )}
                  {saving.kind === "connected" && (
                    <p className="detail good">Conectado. Ya está sincronizando.</p>
                  )}
                  {saving.kind === "failed" && (
                    <p className="detail bad">
                      Se guardó, pero no pudo conectar.
                      {saving.detail !== "" && (
                        <span className="reason"> {saving.detail}</span>
                      )}
                    </p>
                  )}
                </div>
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
