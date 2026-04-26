import { $, fs, os, path } from "zx";

$.verbose = false;

const cmd = process.argv[2];
if (cmd === undefined || cmd === "") {
  process.stderr.write("chix.try: missing required arg `cmd`\n");
  process.exit(2);
}

const cwd = process.cwd();
if (!cwd.startsWith("/")) {
  process.stderr.write(`chix.try: cwd is not absolute: ${cwd}\n`);
  process.exit(2);
}

const tmp = await fs.mkdtemp(path.join(os.tmpdir(), "chix-try-"));
const drvFile = path.join(tmp, "default.nix");

const drv = `{ pkgs ? import <nixpkgs> {} }:
let
  cwd = /. + builtins.getEnv "CHIX_TRY_CWD";
  src = pkgs.nix-gitignore.gitignoreSource [ ".git\n.direnv\n.tmp\nresult\nresult-*\n" ] cwd;
in
pkgs.runCommand "chix-try" {
  __impure = true;
  CHIX_TRY_CMD = builtins.getEnv "CHIX_TRY_CMD";
  buildInputs = with pkgs; [ bashInteractive coreutils ];
  inherit src;
} ''
  mkdir -p $out
  cd $src
  set +e
  bash -c "$CHIX_TRY_CMD" > $out/stdout 2> $out/stderr
  echo $? > $out/exit
  set -e
''
`;

fs.writeFileSync(drvFile, drv);

const r = await $({
  env: { ...process.env, CHIX_TRY_CMD: cmd, CHIX_TRY_CWD: cwd },
  nothrow: true,
})`nix build --impure --no-link --print-out-paths --file ${drvFile}`;

if (r.exitCode !== 0) {
  process.stderr.write("chix.try: nix build failed\n");
  process.stderr.write(r.stderr);
  await fs.rm(tmp, { recursive: true, force: true }).catch(() => {});
  process.exit(1);
}

const outPath = r.stdout.trim();
const stdout = fs.readFileSync(path.join(outPath, "stdout"), "utf-8");
const stderr = fs.readFileSync(path.join(outPath, "stderr"), "utf-8");
const exitText = fs.readFileSync(path.join(outPath, "exit"), "utf-8").trim();

await fs.rm(tmp, { recursive: true, force: true }).catch(() => {});

let out = `exit: ${exitText}\n`;
out += "--- stdout ---\n";
out += stdout;
if (stdout.length > 0 && !stdout.endsWith("\n")) out += "\n";
out += "--- stderr ---\n";
out += stderr;
if (stderr.length > 0 && !stderr.endsWith("\n")) out += "\n";

process.stdout.write(out);
