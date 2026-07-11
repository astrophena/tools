import * as esbuild from "esbuild";

import { denoPlugin } from "@deno/esbuild-plugin";

const jsDir = "./static/js";
await Deno.remove(jsDir, { recursive: true });
await Deno.mkdir(jsDir, { recursive: true });

await esbuild.build({
  plugins: [denoPlugin()],
  entryPoints: ["frontend/app.ts"],
  bundle: true,
  outdir: jsDir,
  entryNames: "app.min",
  chunkNames: "chunk-[name]-[hash].min",
  format: "esm",
  splitting: true,
  minify: true,
});

await esbuild.build({
  entryPoints: ["styles/app.css"],
  bundle: true,
  outdir: "static/css",
  entryNames: "app.min",
  minify: true,
});

esbuild.stop();
