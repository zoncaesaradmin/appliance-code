import { preview } from "vite";
import { createViteConfig } from "./vite-common.mjs";

const mode = process.env.MODE || "production";
const server = await preview({
  ...createViteConfig(mode),
  mode
});

server.printUrls();
