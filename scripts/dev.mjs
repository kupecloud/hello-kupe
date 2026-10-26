// One local entry point for the Go API and Next.js hot-reload server.
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = fileURLToPath(new URL("../", import.meta.url));
const web = path.join(root, "web");
const children = new Set();
let stopping = false;
const apiPort = port(process.env.PORT || "8080");
const webPort = port(process.env.WEB_PORT || "3000");
const origin = `http://localhost:${webPort}`;
const env = {
  ...process.env,
  GOCACHE: process.env.GOCACHE || path.join(root, ".tmp/go-build"),
};

function port(value) {
  if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 65535) {
    throw new Error(`Invalid local port: ${value}`);
  }
  return Number(value);
}

async function stop(code) {
  if (stopping) return;
  stopping = true;
  const running = [...children];
  const closed = running.map(
    (child) => new Promise((resolve) => child.once("close", resolve)),
  );
  for (const child of running) child.kill("SIGTERM");
  const deadline = setTimeout(() => {
    for (const child of running) child.kill("SIGKILL");
  }, 5000);
  await Promise.all(closed);
  clearTimeout(deadline);
  process.exit(code);
}
process.on("SIGINT", () => void stop(130));
process.on("SIGTERM", () => void stop(0));

function launch(command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: root,
    stdio: "inherit",
    env,
    ...options,
  });
  children.add(child);
  child.once("close", () => children.delete(child));
  return child;
}
function run(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = launch(command, args, options);
    child.once("error", reject);
    child.once("close", (code) =>
      code === 0
        ? resolve()
        : reject(new Error(`${command} exited with status ${code}`)),
    );
  });
}
async function checkPort(number, host) {
  await new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", () =>
      reject(
        new Error(
          `Port ${number} is already in use. Set PORT and WEB_PORT to choose other ports.`,
        ),
      ),
    );
    probe.listen(number, host, () => probe.close(resolve));
  });
}

async function main() {
  if (apiPort === webPort)
    throw new Error("PORT and WEB_PORT must be different.");
  await checkPort(apiPort, "127.0.0.1");
  await checkPort(webPort, "localhost");
  await mkdir(path.join(root, ".tmp"), { recursive: true });
  const lock = await readFile(path.join(web, "package-lock.json"));
  const installed = await readFile(path.join(root, ".tmp/dev-web-lock")).catch(
    () => null,
  );
  const next = path.join(web, "node_modules/next/dist/bin/next");
  const hasNext = await access(next).then(
    () => true,
    () => false,
  );
  if (!hasNext || !installed?.equals(lock)) {
    console.log("Installing frontend dependencies…");
    await run("npm", ["ci", "--no-fund"], { cwd: web });
    await writeFile(path.join(root, ".tmp/dev-web-lock"), lock);
  }
  console.log("Building the Go backend…");
  const binary = path.join(root, ".tmp/hello-kupe-dev");
  await run(process.env.GO || "go", [
    "build",
    "-o",
    binary,
    "./cmd/hello-kupe",
  ]);
  if (stopping) return;
  const api = launch(binary, [], {
    env: { ...env, PORT: String(apiPort), DEVELOPMENT_ORIGIN: origin },
  });
  const frontend = launch(
    process.execPath,
    [next, "dev", "--hostname", "localhost", "--port", String(webPort)],
    {
      cwd: web,
      env: {
        ...env,
        KUPE_API_URL: `http://127.0.0.1:${apiPort}`,
        NEXT_TELEMETRY_DISABLED: "1",
      },
    },
  );
  for (const [name, child] of [
    ["Go backend", api],
    ["Next.js frontend", frontend],
  ]) {
    child.once("error", (error) => {
      console.error(`${name}: ${error.message}`);
      void stop(1);
    });
    child.once("close", (code) => {
      if (!stopping) {
        console.error(`${name} stopped.`);
        void stop(code || 0);
      }
    });
  }
  console.log(
    `\nHello Kupe: ${origin}\nAPI: http://127.0.0.1:${apiPort}\nCtrl+C stops both servers.\n`,
  );
}
main().catch((error) => {
  console.error(error.message);
  void stop(1);
});
