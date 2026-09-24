import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Test-only config. The Wails vite plugin is deliberately absent: tests run
// against the stubbed bindings in src/test/setup.ts, never the runtime.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    globals: true,
  },
});
