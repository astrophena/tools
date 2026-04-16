import * as esbuild from "esbuild";

import { denoPlugin } from "@deno/esbuild-plugin";

const jsDir = "./static/js";
await Deno.remove(jsDir, { recursive: true });
await Deno.mkdir(jsDir, { recursive: true });

await esbuild.build({
  plugins: [denoPlugin()],
  entryPoints: ["frontend/app.tsx"],
  bundle: true,
  outdir: jsDir,
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
