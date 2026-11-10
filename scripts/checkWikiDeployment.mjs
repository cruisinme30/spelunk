#!/usr/bin/env node
// Optional observation of the deployed mirror; never replaces source validation.
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { wikiDifferences } from "./wikiFiles.mjs";

const mirror = process.argv[2];
if (!mirror || process.argv.length !== 3) throw new Error("Usage: checkWikiDeployment.mjs WIKI_CHECKOUT");
const source = join(dirname(fileURLToPath(import.meta.url)), "../docs/wiki");
const differences = wikiDifferences(source, mirror);
for (const name of differences) console.log(`Wiki deployment differs: ${name}`);
if (differences.length > 0) process.exitCode = 1;
else console.log("Deployed wiki content matches docs/wiki.");
