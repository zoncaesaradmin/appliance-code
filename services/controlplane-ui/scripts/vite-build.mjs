import { build } from "vite";
import { createViteConfig } from "./vite-common.mjs";

const mode = process.env.MODE || "production";
await build(createViteConfig(mode));
