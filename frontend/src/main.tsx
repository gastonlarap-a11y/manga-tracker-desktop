import React from "react";
import { createRoot } from "react-dom/client";
import { WindowSetBackgroundColour } from "../wailsjs/runtime/runtime";
import "./style.css";
import { App } from "./App";
import { followSystemTheme } from "./theme";

const container = document.getElementById("root");
// index.html declares it; a build without it is broken, and saying so beats
// the non-null assertion's silent TypeError deeper inside React.
if (container === null) {
  throw new Error("index.html has no #root element to render into");
}

// Decided before the first frame: on macOS the window is translucent (main.go)
// and this page must already be transparent when it first paints. The user
// agent is the one thing known that early — Environment() answers after it.
// Under `wails dev` in a browser on a Mac this is true too; the page is then
// transparent over a plain browser background, which only a developer sees.
const translucent = /Macintosh/.test(navigator.userAgent);
document.documentElement.dataset.window = translucent ? "translucent" : "opaque";

if (!translucent) {
  followSystemTheme(window.matchMedia("(prefers-color-scheme: dark)"), (rgb) => {
    try {
      // Alpha 255: Windows reads anything but 0 that way regardless.
      WindowSetBackgroundColour(rgb.r, rgb.g, rgb.b, 255);
    } catch (reason) {
      // Only outside the app (a plain browser, no Wails runtime), where there
      // is no window background to set.
      console.warn("[manga-tracker] window background not set", reason);
    }
  });
}

createRoot(container).render(
  <React.StrictMode>
    <App translucent={translucent} />
  </React.StrictMode>,
);
