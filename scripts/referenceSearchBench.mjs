// Benchmark native text-search CLIs on exactly Spelunk's accepted files.
// CLI startup, discovery and full-line formatting are included; this is not
// equivalent work to a resident engine that emits a 500-row page. Validate
// complete counts and emitted line identities before recording any timings.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import {
  existsSync,
  linkSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

// cspell:ignore NOSYSTEM -- part of Git's documented environment variable name.
const TOOLS = ["ripgrep", "git-grep"];
const LIMITATIONS = [
  "Spelunk uses a resident index, emits at most 500 rows and counts up to 50,000; this is not equal work.",
  "git grep uses --no-index filesystem mode, not tracked-index or historical-tree search.",
  "The filesystem cache is warm and shared; no OS cache eviction or cold-disk claim.",
  "Native Unicode case/word rules can differ; mismatched corpus identities are excluded.",
  "Spelunk identity checks cover emitted rows plus the full count, not all identities above 500.",
];

/** Translate only the simple ASCII subset shared by the three engines. */
export function compatibleQuery(text) {
  const literal = /^(?:(case:yes|word:yes) )?content:([A-Za-z_][A-Za-z_0-9]*)$/.exec(text);
  if (literal) {
    const [, flag, pattern] = literal;
    return { pattern, fixed: true, ignoreCase: flag !== "case:yes", word: flag === "word:yes" };
  }
  const regex = /^\/([A-Za-z_0-9]+\.\*[A-Za-z_0-9]+)\/$/.exec(text);
  return regex ? { pattern: regex[1], fixed: false, ignoreCase: true, word: false } : undefined;
}

/** Read native NUL-separated filenames without confusing colons or line text. */
export function identities(output, tool) {
  const rows = [];
  for (let offset = 0; offset < output.length;) {
    const endPath = output.indexOf(0, offset);
    if (endPath === -1) throw new Error(`${tool}: missing NUL after filename`);
    let path = output.subarray(offset, endPath);
    while (path.subarray(0, 2).equals(Buffer.from("./"))) path = path.subarray(2);
    const endNumber = output.indexOf(tool === "ripgrep" ? 58 : 0, endPath + 1);
    if (endNumber === -1) throw new Error(`${tool}: missing line-number separator`);
    const numberText = output.subarray(endPath + 1, endNumber).toString();
    const line = Number(numberText);
    if (!/^\d+$/.test(numberText) || !Number.isSafeInteger(line) || line < 1) {
      throw new Error(`${tool}: invalid line number ${numberText}`);
    }
    const endLine = output.indexOf(10, endNumber + 1);
    rows.push({ path, line });
    offset = endLine === -1 ? output.length : endLine + 1;
  }
  return rows.sort((a, b) => Buffer.compare(a.path, b.path) || a.line - b.line);
}

/** Hash sorted path/NUL/decimal-line/LF records, preserving duplicate rows. */
export function identityDigest(rows) {
  const hash = createHash("sha256");
  for (const { path, line } of rows) hash.update(path).update(`\0${line}\n`);
  return hash.digest("hex");
}

/** Reject incomplete evidence and mixed filename/content result pages. */
export function comparableRecord(record) {
  if (!record) return "no Spelunk validation record";
  if (!record.all_complete) return "Spelunk query is capped, timed out, or incomplete";
  if (!record.all_results_and_stats_stable) return "Spelunk results or statistics varied across samples";
  const sample = record.first_session_pass;
  if (!sample || !("line_identities_sha256" in sample) || !("line_emitted" in sample)) {
    return "Spelunk validation lacks tool-neutral line identities";
  }
  if (sample.line_emitted !== sample.emitted) return "Spelunk also emits non-line results";
  if (sample.emitted !== Math.min(500, sample.count)) return "Spelunk page is not the expected 500-row prefix";
}

/** Build commands with native line output, explicit case rules and worker count. */
export function nativeCommand(tool, specification, threads) {
  const { pattern, fixed, ignoreCase, word } = specification;
  if (tool === "ripgrep") {
    return [
      "rg",
      "--no-config",
      "--hidden",
      "--no-ignore",
      "--text",
      "--no-heading",
      "--line-number",
      "--with-filename",
      "--null",
      "--color",
      "never",
      "--threads",
      String(threads),
      "--encoding",
      "none",
      ignoreCase ? "--ignore-case" : "--case-sensitive",
      ...(fixed ? ["--fixed-strings"] : []),
      ...(word ? ["--word-regexp"] : []),
      "--",
      pattern,
      ".",
    ];
  }
  return [
    "git",
    "-c",
    "color.grep=false",
    "-c",
    "core.quotePath=false",
    "grep",
    "--no-index",
    `--threads=${threads}`,
    "--text",
    "--line-number",
    "--null",
    "--no-color",
    "--no-exclude-standard",
    fixed ? "--fixed-strings" : "--extended-regexp",
    ...(ignoreCase ? ["--ignore-case"] : []),
    ...(word ? ["--word-regexp"] : []),
    "-e",
    pattern,
    "--",
    ".",
  ];
}

/** Pin the locale and isolate user Git and ripgrep configuration. */
function nativeEnvironment() {
  const omitted = new Set(["GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"]);
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !omitted.has(key)));
  return { ...env, LC_ALL: "C.UTF-8", GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" };
}

/** Measure startup through exit, with a hard timeout and no parsing in the timer. */
export function runNative(options) {
  const { argv, cwd, env, capture, timeoutMs = 120_000 } = options;
  const started = process.hrtime.bigint();
  const result = spawnSync(argv[0], argv.slice(1), {
    cwd,
    env,
    stdio: ["ignore", capture ? "pipe" : "ignore", "pipe"],
    timeout: timeoutMs,
    killSignal: "SIGKILL",
    maxBuffer: 512 * 1024 * 1024,
  });
  const elapsedMs = Number(process.hrtime.bigint() - started) / 1_000_000;
  if (result.error) throw result.error;
  if (result.signal) throw new Error(`command terminated by ${result.signal}`);
  if (result.status !== 0 && result.status !== 1) {
    throw new Error(`command exited ${result.status}: ${result.stderr?.toString()}`);
  }
  if (result.stderr?.length) throw new Error(`command warned: ${result.stderr.toString()}`);
  return { elapsedMs, output: result.stdout };
}

/** Read JSON query records embedded in Go test logs. */
function loadSearchRecords(path) {
  const records = new Map();
  for (const line of readFileSync(path, "utf8").split("\n")) {
    const start = line.indexOf("{");
    if (start === -1) continue;
    let record;
    try {
      record = JSON.parse(line.slice(start));
    } catch {
      continue;
    }
    if (record.event === "query") records.set(record.query, combineSearchRecords(records.get(record.query), record));
  }
  return records;
}

/** Require every repeated session record to retain the same complete answer. */
export function combineSearchRecords(previous, next) {
  if (!previous) return next;
  const first = previous.first_session_pass;
  const later = next.first_session_pass;
  const fields = [
    "count",
    "emitted",
    "line_emitted",
    "line_identities_sha256",
    "ordered_results_sha256",
    "stats_sha256",
  ];
  const same = fields.every((field) => first?.[field] === later?.[field]);
  return {
    ...previous,
    all_complete: previous.all_complete && next.all_complete,
    all_results_and_stats_stable: previous.all_results_and_stats_stable && next.all_results_and_stats_stable && same,
  };
}

/** Validate each path before a hard link can reference the source inode. */
function manifestFile(root, name) {
  if (isAbsolute(name) || name.split("/").some((part) => part === ".." || part === "")) {
    throw new Error(`unsafe manifest path: ${name}`);
  }
  const source = join(root, name);
  const information = lstatSync(source);
  if (!information.isFile() || realpathSync(source) !== source) {
    throw new Error(`manifest path is not a regular contained file: ${name}`);
  }
  return { source, size: information.size };
}

/** Hash raw source bytes and optionally hard link the exact accepted snapshot.
 * Never chmod hard links or write source files. This raw-byte hash differs from
 * the daemon's decoded-content manifest; pre/post hashes detect source changes.
 */
async function sourceDigest(root, files, snapshot) {
  const hash = createHash("sha256");
  for (const name of files) {
    const { source, size } = manifestFile(root, name);
    hash.update(`${Buffer.byteLength(name)}:${name}${size}:`);
    for await (const chunk of createReadStream(source)) hash.update(chunk);
    if (snapshot) {
      const target = join(snapshot, name);
      mkdirSync(dirname(target), { recursive: true });
      linkSync(source, target);
    }
  }
  return hash.digest("hex");
}

/** Check ordering, uniqueness and containment before copying any corpus paths. */
function readInputs(options) {
  const manifestBytes = readFileSync(options.manifest);
  const manifest = JSON.parse(manifestBytes);
  const root = realpathSync(manifest.root);
  const outputRelative = relative(root, resolve(options.output));
  if (!outputRelative.startsWith("..") && !isAbsolute(outputRelative)) {
    throw new Error("benchmark output must be outside the source corpus");
  }
  const sorted = [...new Set(manifest.files)].sort();
  if (sorted.length === 0 || JSON.stringify(sorted) !== JSON.stringify(manifest.files)) {
    throw new Error("manifest needs unique file paths sorted by filename");
  }
  const queryBytes = readFileSync(options.queries);
  return { root, manifest, manifestBytes, queryBytes, queries: JSON.parse(queryBytes) };
}

/** Save raw samples after each completed round; incomplete files stay marked. */
function saveResult(path, result) {
  mkdirSync(dirname(resolve(path)), { recursive: true });
  const staging = `${path}.tmp`;
  writeFileSync(staging, `${JSON.stringify(result, null, 2)}\n`);
  renameSync(staging, path);
}

/** Metadata commands use the same timeout and warning checks as timed searches. */
function metadataOutput(argv, context) {
  return runNative({ argv, ...context, capture: true })
    .output.toString()
    .trim();
}

/** Describe inputs and scope without implying an end-to-end product comparison. */
function measurementHeader(options, inputs, env) {
  return {
    method: "Native CLI startup, recursive discovery, scan, and full-line formatting to /dev/null",
    limitations: LIMITATIONS,
    threads: options.threads,
    runs: options.runs,
    locale: env.LC_ALL,
    manifest_file_sha256: sha256(inputs.manifestBytes),
    spelunk_content_manifest_sha256: inputs.manifest.content_manifest_sha256,
    query_file_sha256: sha256(inputs.queryBytes),
    spelunk_validation_log_sha256: sha256(readFileSync(options.spelunkResults)),
    files: inputs.manifest.files.length,
    versions: {
      ripgrep: metadataOutput(["rg", "--version"], { env }),
      git: metadataOutput(["git", "--version"], { env }),
    },
    corpus_revision: metadataOutput(["git", "rev-parse", "HEAD"], { cwd: inputs.root, env }),
    queries: [],
    excluded: [],
    complete: false,
  };
}

/** Hash input artifacts so the exact manifest and query file can be identified. */
function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

/** Capture each native answer once outside timing; keep both full and page hashes. */
function validateNative(specification, context, threads) {
  const references = {};
  for (const tool of TOOLS) {
    const { output } = runNative({ argv: nativeCommand(tool, specification, threads), ...context, capture: true });
    const rows = identities(output, tool);
    references[tool] = {
      count: rows.length,
      full_line_identities_sha256: identityDigest(rows),
      page_line_identities_sha256: identityDigest(rows.slice(0, 500)),
      samples_ms: [],
    };
  }
  return references;
}

/** Native tools must agree on all rows; Spelunk must agree on its whole emitted page and count. */
export function referencesAgree(references, sample) {
  const sameTools =
    references[TOOLS[0]].full_line_identities_sha256 === references[TOOLS[1]].full_line_identities_sha256;
  return (
    sameTools &&
    Object.values(references).every(
      (reference) =>
        reference.count === sample.count && reference.page_line_identities_sha256 === sample.line_identities_sha256,
    )
  );
}

/** Select only complete, stable, line-only queries with matching native answers. */
function verifiedQueries(inputs, records, result, context) {
  for (const query of inputs.queries) {
    const specification = compatibleQuery(query.text);
    const record = records.get(query.text);
    const reason = specification ? comparableRecord(record) : "outside supported single-pattern ASCII subset";
    if (reason) {
      result.excluded.push({ query: query.text, reason });
      continue;
    }
    const sample = record.first_session_pass;
    const references = validateNative(specification, context, result.threads);
    if (!referencesAgree(references, sample)) {
      result.excluded.push({
        query: query.text,
        reason: "corpus identities/counts disagree",
        references,
        spelunk: sample,
      });
      continue;
    }
    result.queries.push({
      query: query.text,
      family: query.family,
      specification,
      references,
      spelunk_count: sample.count,
      spelunk_identity_scope: sample.count <= 500 ? "all rows" : "first 500 file-ordered rows plus full count",
    });
  }
}

/** Alternate tool order each round, never running two CLIs concurrently. */
function timedRounds(options, result, context) {
  for (let round = 0; round < options.runs; round++) {
    for (const row of result.queries) {
      const tools = round % 2 === 0 ? TOOLS : TOOLS.toReversed();
      for (const tool of tools) {
        const argv = nativeCommand(tool, row.specification, options.threads);
        const { elapsedMs } = runNative({ argv, ...context, capture: false });
        row.references[tool].samples_ms.push(elapsedMs);
      }
    }
    saveResult(options.output, result);
    console.log(`completed round ${round + 1}/${options.runs}`);
  }
}

/** Summarize only after all timings and the final corpus integrity check pass. */
function finishMeasurements(result) {
  for (const row of result.queries) {
    for (const reference of Object.values(row.references)) {
      const samples = [...reference.samples_ms].sort((a, b) => a - b);
      reference.p50_ms = (samples[Math.floor((samples.length - 1) / 2)] + samples[Math.floor(samples.length / 2)]) / 2;
      reference.p95_ms = samples[Math.ceil(0.95 * samples.length) - 1];
    }
  }
  result.complete = true;
}

/** Prepare the accepted snapshot, verify answers, measure, and prove no source changed. */
async function benchmark(options) {
  const inputs = readInputs(options);
  const env = nativeEnvironment();
  const result = measurementHeader(options, inputs, env);
  const records = loadSearchRecords(options.spelunkResults);
  const snapshot = mkdtempSync(join(tmpdir(), "spelunk-reference-"));
  try {
    result.raw_source_sha256_before = await sourceDigest(inputs.root, inputs.manifest.files, snapshot);
    const context = { cwd: snapshot, env };
    verifiedQueries(inputs, records, result, context);
    if (result.queries.length === 0) throw new Error("no complete, identity-verified queries remain");
    timedRounds(options, result, context);
    result.raw_source_sha256_after = await sourceDigest(inputs.root, inputs.manifest.files);
    if (result.raw_source_sha256_before !== result.raw_source_sha256_after) {
      throw new Error("source corpus changed during measurement; results are invalid");
    }
    finishMeasurements(result);
  } finally {
    saveResult(options.output, result);
    if (existsSync(snapshot)) rmSync(snapshot, { recursive: true });
  }
}

/** Require reproducibility inputs and at least 20 measured samples. */
function options() {
  const { values } = parseArgs({
    options: {
      manifest: { type: "string" },
      queries: { type: "string" },
      "spelunk-results": { type: "string" },
      output: { type: "string" },
      runs: { type: "string", default: "20" },
      threads: { type: "string", default: "4" },
    },
  });
  for (const name of ["manifest", "queries", "spelunk-results", "output"]) {
    if (!values[name]) throw new Error(`missing --${name}`);
  }
  const runs = Number(values.runs);
  const threads = Number(values.threads);
  if (!Number.isInteger(runs) || runs < 20 || runs > 1000 || !Number.isInteger(threads) || threads < 1) {
    throw new Error("use 20–1000 measured runs and at least one thread");
  }
  return { ...values, runs, threads, spelunkResults: values["spelunk-results"] };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) await benchmark(options());
