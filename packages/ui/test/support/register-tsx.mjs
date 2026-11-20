// Registers `tsx-hooks.mjs` for the test run: see there.
import { register } from "node:module";

register("./tsx-hooks.mjs", import.meta.url);
