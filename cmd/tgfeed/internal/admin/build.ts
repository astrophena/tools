import * as esbuild from "esbuild";

import { denoPlugin } from "@deno/esbuild-plugin";

await esbuild.build({
  plugins: [denoPlugin()],
  entryPoints: ["frontend/app.tsx"],
  bundle: true,
  outdir: "static/js",
  entryNames: "app.min",
  chunkNames: "chunk-[hash]",
  format: "esm",
  splitting: true,
  minify: true,
  jsx: "automatic",
  jsxImportSource: "react",
});

await esbuild.build({
  entryPoints: ["styles/app.css"],
  bundle: true,
  outdir: "static/css",
  entryNames: "app.min",
  minify: true,
});

esbuild.stop();
