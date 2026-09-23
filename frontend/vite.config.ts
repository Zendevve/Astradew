import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

/**
 * main.go embeds frontend/dist, and a //go:embed directive needs at least one
 * file to match, so the directory has to exist even before the frontend has
 * been built — otherwise a fresh clone cannot run `go build` or `go vet`.
 * Vite empties the output directory on every build, which would delete the
 * placeholder that guarantees that; emit it again as part of the bundle.
 */
function keepDistDirectory(): Plugin {
  return {
    name: "astradew:keep-dist-directory",
    generateBundle() {
      this.emitFile({ type: "asset", fileName: ".gitkeep", source: "" });
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [react(), wails("./bindings"), keepDistDirectory()],
});
