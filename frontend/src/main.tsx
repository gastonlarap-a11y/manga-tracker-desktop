import React from "react";
import { createRoot } from "react-dom/client";
import "./style.css";
import { App } from "./App";

const container = document.getElementById("root");
// index.html declares it; a build without it is broken, and saying so beats
// the non-null assertion's silent TypeError deeper inside React.
if (container === null) {
  throw new Error("index.html has no #root element to render into");
}

createRoot(container).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
