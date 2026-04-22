import { app, shell, BrowserWindow } from "electron";
import { join } from "path";
import { electronApp, optimizer, is } from "@electron-toolkit/utils";
import { startServer, stopServer } from "./server-manager";

let mainWindow: BrowserWindow | null = null;
let serverURL = process.env["VITE_API_URL"] ?? "http://localhost:8080";
let wsURL = process.env["VITE_WS_URL"] ?? "ws://localhost:8080/ws";

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 13 },
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
      webSecurity: false,
      additionalArguments: [
        `--server-url=${serverURL}`,
        `--ws-url=${wsURL}`,
      ],
    },
  });

  mainWindow.on("ready-to-show", () => {
    mainWindow?.show();
  });

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url);
    return { action: "deny" };
  });

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

app.whenReady().then(async () => {
  electronApp.setAppUserModelId("ai.multica.desktop");

  app.on("browser-window-created", (_, window) => {
    optimizer.watchWindowShortcuts(window);
  });

  // Start embedded server if no explicit API URL is configured
  if (!process.env["VITE_API_URL"]) {
    try {
      const url = await startServer();
      serverURL = url;
      wsURL = url.replace("http://", "ws://") + "/ws";
    } catch (err) {
      console.error("Failed to start embedded server:", err);
      // Fall through and let the UI show an error or use default URL
    }
  }

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  stopServer();
  if (process.platform !== "darwin") app.quit();
});

app.on("before-quit", () => {
  stopServer();
});
