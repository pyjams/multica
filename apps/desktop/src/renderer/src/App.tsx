import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";

// Read URLs injected by the main process via additionalArguments
function getURLFromArgs(flag: string): string | undefined {
  // process.argv is available in the renderer via Electron's contextBridge/preload
  // or directly when sandbox: false
  if (typeof process !== "undefined" && Array.isArray(process.argv)) {
    const arg = process.argv.find((a) => a.startsWith(`${flag}=`));
    if (arg) return arg.slice(flag.length + 1);
  }
  return undefined;
}

const apiBaseUrl =
  getURLFromArgs("--server-url") ||
  import.meta.env.VITE_API_URL ||
  "http://localhost:8080";

const wsUrl =
  getURLFromArgs("--ws-url") ||
  import.meta.env.VITE_WS_URL ||
  "ws://localhost:8080/ws";

function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  if (!user) return <DesktopLoginPage />;
  return <DesktopShell />;
}

export default function App() {
  return (
    <ThemeProvider>
      <CoreProvider apiBaseUrl={apiBaseUrl} wsUrl={wsUrl}>
        <AppContent />
      </CoreProvider>
      <Toaster />
    </ThemeProvider>
  );
}
