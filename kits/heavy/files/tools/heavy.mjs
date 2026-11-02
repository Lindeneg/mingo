{{block "header" .}}// Runs a heavy script (a check, the tests, a build, the dev server) under one of the
// machine's slots where mingo is installed, and at once where it is not:
//
//     node tools/heavy.mjs check -- {{if eq .CI.Stack "npm"}}npm{{else}}pnpm{{end}} run check:run
//
// The slots are mingo's (`mingo heavy`, shared by every project on the machine): a
// command waits while the slots are taken or the free memory is low, and one started
// under a holder runs at once.
{{end}}import { spawn } from "node:child_process";
import { constants } from "node:os";

{{block "turbo" .}}{{end}}const isWindows = process.platform === "win32";

// Runs `file` with the terminal and resolves its exit code. At a terminal,
// Ctrl+C reaches the whole process group, the command with it, so the shim
// only waits for it to end: a second SIGINT of its own would make turbo and
// Playwright quit without their cleanup. Without a terminal a SIGINT is the
// shim's alone and is passed on, as are SIGTERM and SIGHUP.
const run = (file, args, env) =>
  new Promise((resolve, reject) => {
    const child = spawn(file, args, { stdio: "inherit", env, shell: isWindows });
    const forward = (signal) => child.kill(signal);
    const onInterrupt = (signal) => {
      if (process.stdin.isTTY !== true) forward(signal);
    };
    const stop = () => {
      process.off("SIGINT", onInterrupt);
      process.off("SIGTERM", forward);
      process.off("SIGHUP", forward);
    };
    process.on("SIGINT", onInterrupt);
    process.on("SIGTERM", forward);
    process.on("SIGHUP", forward);
    child.on("error", (error) => {
      stop();
      reject(error);
    });
    child.on("exit", (code, signal) => {
      stop();
      resolve(code ?? 128 + (constants.signals[signal] ?? 0));
    });
  });

const [label, separator, ...command] = process.argv.slice(2);
if (label === undefined || separator !== "--" || command.length === 0) {
  console.error("heavy: usage: node tools/heavy.mjs <label> -- <command> [args]");
  process.exit(2);
}
{{block "env" .}}const env = process.env;{{end}}
const [file, ...args] = command;
if (isWindows) {
  process.exitCode = await run(file, args, env);
} else {
  try {
    process.exitCode = await run("mingo", ["heavy", label, "--", ...command], env);
  } catch (error) {
    // No mingo on this machine: nothing ran, so the command runs without a slot.
    if (error.code !== "ENOENT") throw error;
    process.exitCode = await run(file, args, env);
  }
}
