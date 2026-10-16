import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { SessionProvider } from "./session";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing app root");
createRoot(root).render(
  <React.StrictMode>
    <SessionProvider><App /></SessionProvider>
  </React.StrictMode>,
);
