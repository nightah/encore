import { type ChildProcess, execFileSync, spawn } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { E2E_ECHO_ID, realServer } from "@e2e/real/env";

/**
 * Starts a fake Kodi and a fresh Encore (empty data directory) for the real-server specs, and stops both afterwards.
 *
 * Encore is built first: the UI with `vite build` (into internal/server/dist, which the binary embeds), then the binary with `go build` into the run's temporary directory, so the specs always test the current code. Set `E2E_ENCORE_BIN` to use a binary you've already built instead, relative to the repository root (CI does, from its build step). The fake Kodi is built with `go build` too. The one-time setup token is read from Encore's log and handed to the specs as `E2E_SETUP_TOKEN`; it is never printed.
 */
export default async function globalSetup() {
  const repo = resolve(import.meta.dirname, "../../..");
  const prebuilt = process.env.E2E_ENCORE_BIN;
  const dataDir = await mkdtemp(join(tmpdir(), "encore-e2e-"));
  // One unlinked Echo, so the devices page has something to link (no Alexa request can reach a test server).
  await writeFile(join(dataDir, "encore.yml"), `devices:\n  - id: ${E2E_ECHO_ID}\n    name: ""\n`);

  const children: ChildProcess[] = [];
  const stop = async () => {
    await Promise.all(children.map(exited));
    await rm(dataDir, { force: true, recursive: true });
  };

  try {
    let bin: string;

    if (prebuilt) {
      bin = resolve(repo, prebuilt);
    } else {
      bin = join(dataDir, "encore");
      execFileSync("pnpm", ["build"], { cwd: join(repo, "web"), stdio: "inherit" });
      execFileSync("go", ["build", "-trimpath", "-o", bin, "./cmd/encore"], {
        cwd: repo,
        env: { ...process.env, CGO_ENABLED: "0" },
        stdio: "inherit",
      });
    }

    const fakeKodi = join(dataDir, "fakekodi");
    execFileSync("go", ["build", "-o", fakeKodi, "./internal/backend/kodi/fakekodi/cmd/fakekodi"], {
      cwd: repo,
      stdio: "inherit",
    });
    const kodi = spawn(fakeKodi, ["-addr", realServer.kodiAddr], {
      stdio: ["ignore", "inherit", "inherit"],
    });
    children.push(kodi);

    let log = "";
    const encore = spawn(bin, ["serve"], {
      cwd: dataDir,
      env: {
        ...process.env,
        ENCORE_DATA_DIR: dataDir,
        ENCORE_LOG_FORMAT: "text",
        ENCORE_LOG_LEVEL: "info",
        ENCORE_SERVER_ADDRESS: realServer.addr,
      },
      stdio: ["ignore", "pipe", "pipe"],
    });
    children.push(encore);
    encore.on("error", (error) => {
      log += `\n${error.message}`;
    });
    const capture = (chunk: Buffer) => {
      log += chunk.toString();
      if (process.env.E2E_SERVER_LOG)
        process.stderr.write(chunk.toString().replace(/setup_token=\S+/g, "setup_token=…"));
    };
    encore.stdout?.on("data", capture);
    encore.stderr?.on("data", capture);

    await waitFor(`${realServer.baseURL}api/v1/healthz`, () => log, encore);
    await waitFor(`http://${realServer.kodiAddr}/jsonrpc`, () => "", kodi, "POST");
    const token = log.match(/setup_token=(\S+)/)?.[1];
    if (!token) throw new Error("encore did not log a setup token; is the data directory fresh?");
    process.env.E2E_SETUP_TOKEN = token;
  } catch (error) {
    await stop();
    throw error;
  }

  return stop;
}

/** Stops a child process and resolves once it has exited, so its files can be removed. */
function exited(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve();
  return new Promise((done) => {
    const timer = setTimeout(() => child.kill("SIGKILL"), 10_000);
    child.once("exit", () => {
      clearTimeout(timer);
      done();
    });
    child.kill("SIGTERM");
  });
}

async function waitFor(url: string, log: () => string, child: ChildProcess, method = "GET", timeoutMs = 120_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(
        `${url}: the process exited (${child.exitCode})\n${log().replace(/setup_token=\S+/g, "setup_token=…")}`,
      );
    }
    try {
      const response = await fetch(url, {
        method,
        ...(method === "POST"
          ? {
              body: '{"jsonrpc":"2.0","id":1,"method":"JSONRPC.Ping"}',
              headers: { "Content-Type": "application/json" },
            }
          : {}),
      });
      if (response.ok) return;
    } catch {
      // Not up yet.
    }
    await new Promise((done) => setTimeout(done, 250));
  }
  throw new Error(`${url} did not come up in ${timeoutMs / 1000} s`);
}
