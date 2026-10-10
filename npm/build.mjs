// Builds the npm packages for packmyrepo, and optionally publishes them.
//
//   node npm/build.mjs 0.1.0              build into npm/dist (nothing is published)
//   node npm/build.mjs 0.1.0 --publish    build, then publish to npm
//   node npm/build.mjs 0.1.0 --publish --dry-run
//
// Layout (the same pattern esbuild and Biome use):
//   packmyrepo                 small launcher, depends on all platform packages
//   @packmyrepo/<os>-<cpu>     one native binary each; npm installs only the one
//                              that matches the user's computer (os/cpu fields)

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "..");
const out = path.join(here, "dist");

const TARGETS = [
  { goos: "windows", goarch: "amd64", os: "win32", cpu: "x64" },
  { goos: "windows", goarch: "arm64", os: "win32", cpu: "arm64" },
  { goos: "darwin", goarch: "amd64", os: "darwin", cpu: "x64" },
  { goos: "darwin", goarch: "arm64", os: "darwin", cpu: "arm64" },
  { goos: "linux", goarch: "amd64", os: "linux", cpu: "x64" },
  { goos: "linux", goarch: "arm64", os: "linux", cpu: "arm64" },
];

const args = process.argv.slice(2);
const version = (args.find((a) => !a.startsWith("--")) || "").replace(/^v/, "");
const publish = args.includes("--publish");
const dryRun = args.includes("--dry-run");

if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version)) {
  console.error(
    "usage: node npm/build.mjs <version like 0.1.0> [--publish] [--dry-run]",
  );
  process.exit(1);
}

const cli = JSON.parse(
  fs.readFileSync(path.join(here, "cli", "package.json"), "utf8"),
);
const common = {
  license: cli.license,
  homepage: cli.homepage,
  repository: cli.repository,
  bugs: cli.bugs,
};

fs.rmSync(out, { recursive: true, force: true });
const packageDirs = [];

// 1. one package per platform, each holding a native binary
for (const t of TARGETS) {
  const name = `@packmyrepo/${t.os}-${t.cpu}`;
  const dir = path.join(out, `${t.os}-${t.cpu}`);
  const exe = t.os === "win32" ? "packmyrepo.exe" : "packmyrepo";
  fs.mkdirSync(path.join(dir, "bin"), { recursive: true });

  console.log(`building ${name}`);
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags",
      `-s -w -X github.com/sahbadal/packmyrepo/internal/app.version=${version}`,
      "-o",
      path.join(dir, "bin", exe),
      ".",
    ],
    {
      cwd: root,
      stdio: "inherit",
      env: { ...process.env, GOOS: t.goos, GOARCH: t.goarch, CGO_ENABLED: "0" },
    },
  );
  fs.chmodSync(path.join(dir, "bin", exe), 0o755);

  writeJSON(path.join(dir, "package.json"), {
    name,
    version,
    description: `The ${t.os} ${t.cpu} binary for packmyrepo. Install "packmyrepo" instead of this package.`,
    ...common,
    os: [t.os],
    cpu: [t.cpu],
    files: ["bin", "LICENSE", "THIRD_PARTY_NOTICES.md"],
    preferUnplugged: true,
  });
  fs.writeFileSync(
    path.join(dir, "README.md"),
    `# ${name}\n\nThe ${t.os} ${t.cpu} binary for [packmyrepo](https://www.npmjs.com/package/packmyrepo). Install \`packmyrepo\` instead.\n`,
  );
  copyLegal(dir);
  packageDirs.push(dir);
}

// 2. the launcher package that users install
const main = path.join(out, "packmyrepo");
fs.mkdirSync(path.join(main, "bin"), { recursive: true });
fs.copyFileSync(
  path.join(here, "cli", "bin", "packmyrepo.js"),
  path.join(main, "bin", "packmyrepo.js"),
);
fs.chmodSync(path.join(main, "bin", "packmyrepo.js"), 0o755);
fs.copyFileSync(path.join(root, "README.md"), path.join(main, "README.md"));
copyLegal(main);
writeJSON(path.join(main, "package.json"), {
  ...cli,
  version,
  optionalDependencies: Object.fromEntries(
    TARGETS.map((t) => [`@packmyrepo/${t.os}-${t.cpu}`, version]),
  ),
});
packageDirs.push(main); // published last, so its dependencies already exist

console.log(
  `\nbuilt ${packageDirs.length} packages in ${path.relative(root, out)}`,
);

// 3. publish (platform packages first)
if (publish) {
  for (const dir of packageDirs) {
    const pkg = JSON.parse(
      fs.readFileSync(path.join(dir, "package.json"), "utf8"),
    );
    if (alreadyPublished(pkg.name, version)) {
      console.log(`skip ${pkg.name}@${version} (already on npm)`);
      continue;
    }
    console.log(`publishing ${pkg.name}@${version}`);
    const flags = ["publish", "--access", "public"];
    if (process.env.GITHUB_ACTIONS === "true") flags.push("--provenance"); // signed link to the exact commit
    if (dryRun) flags.push("--dry-run");
    if (version.includes("-")) flags.push("--tag", "next"); // 0.2.0-beta.1 must not become "latest"
    execFileSync("npm", flags, {
      cwd: dir,
      stdio: "inherit",
      shell: process.platform === "win32",
    });
  }
}

function alreadyPublished(name, v) {
  try {
    const res = execFileSync("npm", ["view", `${name}@${v}`, "version"], {
      stdio: ["ignore", "pipe", "ignore"],
      shell: process.platform === "win32",
    });
    return res.toString().trim() === v;
  } catch {
    return false;
  }
}

function copyLegal(dir) {
  for (const f of ["LICENSE", "THIRD_PARTY_NOTICES.md"]) {
    if (fs.existsSync(path.join(root, f)))
      fs.copyFileSync(path.join(root, f), path.join(dir, f));
  }
}

function writeJSON(file, data) {
  fs.writeFileSync(file, JSON.stringify(data, null, 2) + "\n");
}
