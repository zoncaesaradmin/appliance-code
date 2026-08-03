import { createServer } from "vite";
import { createViteConfig } from "./vite-common.mjs";

const mode = process.env.MODE || "development";
const server = await createServer({
  ...createViteConfig(mode),
  mode
});

await server.listen();
server.printUrls();
