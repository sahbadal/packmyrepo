#!/usr/bin/env node
"use strict";

// Thin launcher: finds the native packmyrepo binary that npm installed for this
// computer (one of the @packmyrepo/<os>-<cpu> packages) and runs it.

const { spawnSync } = require("child_process");
const fs = require("fs");

const PACKAGES = {
  "win32 x64": "@packmyrepo/win32-x64",
  "win32 arm64": "@packmyrepo/win32-arm64",
  "darwin x64": "@packmyrepo/darwin-x64",
  "darwin arm64": "@packmyrepo/darwin-arm64",
  "linux x64": "@packmyrepo/linux-x64",
  "linux arm64": "@packmyrepo/linux-arm64",
};

function fail(message) {
  process.stderr.write("packmyrepo: " + message + "\n");
  process.exit(1);
}

function findBinary() {
  // Escape hatch for testing a local build: PACKMYREPO_BINARY=/path/to/packmyrepo
  if (process.env.PACKMYREPO_BINARY) return process.env.PACKMYREPO_BINARY;

  const key = process.platform + " " + process.arch;
  const pkg = PACKAGES[key];
  if (!pkg) {
    fail(
      "there is no build for " + key + " yet. Please open an issue on GitHub.",
    );
  }
  const exe = process.platform === "win32" ? "packmyrepo.exe" : "packmyrepo";
  try {
    return require.resolve(pkg + "/bin/" + exe);
  } catch (e) {
    fail(
      "the package " +
        pkg +
        " is missing.\n" +
        "  It is installed automatically unless optional dependencies are turned off.\n" +
        "  Reinstall without --no-optional / --omit=optional, for example:\n" +
        "    npm install -g packmyrepo",
    );
  }
}

const bin = findBinary();

// npm sometimes drops the executable bit; fix it once instead of failing.
if (process.platform !== "win32") {
  try {
    fs.accessSync(bin, fs.constants.X_OK);
  } catch (e) {
    try {
      fs.chmodSync(bin, 0o755);
    } catch (_) {
      /* reported below if it still fails */
    }
  }
}

const result = spawnSync(bin, process.argv.slice(2), {
  stdio: "inherit",
  windowsHide: false,
});
if (result.error) {
  fail("could not start " + bin + ": " + result.error.message);
}
if (result.signal) {
  process.kill(process.pid, result.signal);
} else {
  process.exit(result.status === null ? 1 : result.status);
}
