import { app } from "electron";
import { spawn, ChildProcess } from "child_process";
import { join } from "path";
import { existsSync } from "fs";
import http from "http";
import { is } from "@electron-toolkit/utils";

const SERVER_PORT = process.env["MULTICA_LITE_PORT"] ?? "8081";

let serverProcess: ChildProcess | null = null;

function getServerBinaryPath(): string | null {
  if (is.dev) {
    // In dev, look for a pre-built binary next to the server-lite source
    const candidates = [
      join(__dirname, "../../../../server-lite/server-lite"),
      join(__dirname, "../../../../server-lite/server-lite.exe"),
    ];
    for (const c of candidates) {
      if (existsSync(c)) return c;
    }
    return null;
  }

  // In production, the binary is bundled in extraResources/server-lite
  const ext = process.platform === "win32" ? ".exe" : "";
  return join(process.resourcesPath, "server-lite", `server-lite${ext}`);
}

export async function startServer(): Promise<string> {
  const binaryPath = getServerBinaryPath();

  if (!binaryPath || !existsSync(binaryPath)) {
    // No embedded server — assume an external server is running or will be started
    console.warn("server-lite binary not found, assuming external server on port", SERVER_PORT);
    return `http://localhost:${SERVER_PORT}`;
  }

  const dataDir = join(app.getPath("userData"), "multica-lite");

  return new Promise((resolve, reject) => {
    serverProcess = spawn(binaryPath, [], {
      env: {
        ...process.env,
        PORT: SERVER_PORT,
        MULTICA_DATA_DIR: dataDir,
      },
      stdio: ["ignore", "pipe", "pipe"],
    });

    serverProcess.stdout?.on("data", (data: Buffer) => {
      console.log("[server-lite]", data.toString().trim());
    });
    serverProcess.stderr?.on("data", (data: Buffer) => {
      console.error("[server-lite]", data.toString().trim());
    });
    serverProcess.on("error", (err) => {
      console.error("Failed to start server-lite:", err);
      reject(err);
    });
    serverProcess.on("exit", (code) => {
      console.log("server-lite exited with code", code);
      serverProcess = null;
    });

    waitForServer(`http://127.0.0.1:${SERVER_PORT}/health`, 30).then(() => {
      resolve(`http://127.0.0.1:${SERVER_PORT}`);
    }).catch(reject);
  });
}

export function stopServer(): void {
  if (serverProcess) {
    serverProcess.kill("SIGTERM");
    serverProcess = null;
  }
}

function waitForServer(url: string, timeoutSec: number): Promise<void> {
  const deadline = Date.now() + timeoutSec * 1000;
  return new Promise((resolve, reject) => {
    function attempt() {
      http.get(url, (res) => {
        if (res.statusCode === 200) {
          resolve();
        } else {
          retry();
        }
        res.resume();
      }).on("error", () => {
        retry();
      });
    }
    function retry() {
      if (Date.now() >= deadline) {
        reject(new Error("server-lite did not start in time"));
        return;
      }
      setTimeout(attempt, 250);
    }
    attempt();
  });
}

export function getServerURL(): string {
  return `http://127.0.0.1:${SERVER_PORT}`;
}

export function getWsURL(): string {
  return `ws://127.0.0.1:${SERVER_PORT}`;
}
