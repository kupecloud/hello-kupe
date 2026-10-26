// Local Docker/kind only. Every Kubernetes command uses this isolated kubeconfig.
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = fileURLToPath(new URL("../", import.meta.url));
const state = path.join(root, ".tmp/kind");
const kubeconfig = path.join(root, ".tmp/kind-kubeconfig");
const cluster = "hello-kupe-local";
const context = `kind-${cluster}`;
const appImage = "hello-kupe:kind-local";
const postgresSource =
  "postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336";
const postgresImage = "hello-kupe-postgres:kind-local";
const databaseHost = "postgres.hello-kupe-db.svc.cluster.local";
const kubectl = ["--kubeconfig", kubeconfig, "--context", context];
const helm = ["--kubeconfig", kubeconfig, "--kube-context", context];

function run(command, args, { input, capture = false, quiet = false } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: root,
      stdio: [
        input ? "pipe" : "ignore",
        capture ? "pipe" : "inherit",
        quiet ? "ignore" : "inherit",
      ],
    });
    let output = "";
    child.stdout?.on("data", (data) => {
      output += data;
    });
    child.once("error", reject);
    child.once("close", (code) =>
      code === 0
        ? resolve(output.trim())
        : reject(new Error(`${command} exited with status ${code}`)),
    );
    if (input) child.stdin.end(input);
  });
}
const exists = (file) =>
  access(file).then(
    () => true,
    () => false,
  );
const apply = (items) =>
  run("kubectl", [...kubectl, "apply", "-f", "-"], {
    input: JSON.stringify({ apiVersion: "v1", kind: "List", items }),
  });
const secret = (namespace, name, stringData) => ({
  apiVersion: "v1",
  kind: "Secret",
  metadata: { namespace, name },
  type: "Opaque",
  stringData,
});

async function up() {
  await mkdir(state, { recursive: true, mode: 0o700 });
  const clusters = (
    await run("kind", ["get", "clusters"], { capture: true })
  ).split("\n");
  if (!clusters.includes(cluster)) {
    await run("kind", [
      "create",
      "cluster",
      "--name",
      cluster,
      "--image",
      "kindest/node:v1.35.0",
      "--kubeconfig",
      kubeconfig,
      "--wait",
      "120s",
    ]);
  } else {
    await run("kind", [
      "export",
      "kubeconfig",
      "--name",
      cluster,
      "--kubeconfig",
      kubeconfig,
    ]);
  }
  await run("kubectl", [
    ...kubectl,
    "wait",
    "--for=condition=Ready",
    "nodes",
    "--all",
    "--timeout=180s",
  ]);
  await run("docker", ["build", "-t", appImage, "."]);
  if (
    !(await run("docker", ["image", "inspect", postgresSource], {
      capture: true,
      quiet: true,
    }).then(
      () => true,
      () => false,
    ))
  ) {
    await run("docker", ["pull", postgresSource]);
  }
  await run("docker", ["tag", postgresSource, postgresImage]);
  await run("kind", [
    "load",
    "docker-image",
    "--name",
    cluster,
    appImage,
    postgresImage,
  ]);

  const credentialsFile = path.join(state, "credentials.json");
  if (!(await exists(credentialsFile))) {
    const hasDatabase = await run(
      "kubectl",
      [
        ...kubectl,
        "-n",
        "hello-kupe-db",
        "get",
        "pvc",
        "postgres-data",
        "-o",
        "name",
      ],
      { capture: true, quiet: true },
    ).then(
      () => true,
      () => false,
    );
    if (hasDatabase) {
      throw new Error(
        "The local database exists but its credentials are missing. Restore .tmp/kind/credentials.json before rerunning.",
      );
    }
    await writeFile(
      credentialsFile,
      JSON.stringify(
        Object.fromEntries(
          ["admin", "owner", "readwrite", "readonly"].map((role) => [
            role,
            randomBytes(24).toString("base64url"),
          ]),
        ),
      ),
      { mode: 0o600 },
    );
  }
  const credentials = JSON.parse(await readFile(credentialsFile, "utf8"));
  if (!(await exists(path.join(state, "server.crt")))) {
    await run(
      "openssl",
      [
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-days",
        "365",
        "-subj",
        "/CN=Hello Kupe local CA",
        "-keyout",
        path.join(state, "ca.key"),
        "-out",
        path.join(state, "ca.crt"),
      ],
      { quiet: true },
    );
    await run(
      "openssl",
      [
        "req",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-subj",
        `/CN=${databaseHost}`,
        "-keyout",
        path.join(state, "server.key"),
        "-out",
        path.join(state, "server.csr"),
      ],
      { quiet: true },
    );
    await writeFile(
      path.join(state, "server.ext"),
      `subjectAltName=DNS:${databaseHost},DNS:postgres.hello-kupe-db.svc,DNS:postgres\nextendedKeyUsage=serverAuth\n`,
    );
    await run(
      "openssl",
      [
        "x509",
        "-req",
        "-days",
        "365",
        "-in",
        path.join(state, "server.csr"),
        "-CA",
        path.join(state, "ca.crt"),
        "-CAkey",
        path.join(state, "ca.key"),
        "-CAcreateserial",
        "-extfile",
        path.join(state, "server.ext"),
        "-out",
        path.join(state, "server.crt"),
      ],
      { quiet: true },
    );
  }
  const ca = await readFile(path.join(state, "ca.crt"), "utf8");
  const binding = (role) => ({
    type: "postgresql",
    host: databaseHost,
    port: "5432",
    database: "hello_kupe",
    username: `app_${role}`,
    password: credentials[role],
    sslmode: "verify-full",
    "ca.crt": ca,
  });
  const init = (
    await readFile(path.join(root, "scripts/local-postgres/init.sql"), "utf8")
  )
    .replace("local-owner", credentials.owner)
    .replace("local-readwrite", credentials.readwrite)
    .replace("local-readonly", credentials.readonly);
  await apply(
    ["hello-kupe", "hello-kupe-migrate", "hello-kupe-db"].map((name) => ({
      apiVersion: "v1",
      kind: "Namespace",
      metadata: { name },
    })),
  );
  await apply([
    secret("hello-kupe-db", "postgres-admin", { password: credentials.admin }),
    secret("hello-kupe-db", "postgres-init", { "init.sql": init }),
    secret("hello-kupe-db", "postgres-tls", {
      "server.crt": await readFile(path.join(state, "server.crt"), "utf8"),
      "server.key": await readFile(path.join(state, "server.key"), "utf8"),
    }),
    secret("hello-kupe", "hello-kupe-db-readwrite", binding("readwrite")),
    secret("hello-kupe-migrate", "hello-kupe-db-owner", binding("owner")),
    {
      apiVersion: "v1",
      kind: "PersistentVolumeClaim",
      metadata: { name: "postgres-data", namespace: "hello-kupe-db" },
      spec: {
        accessModes: ["ReadWriteOnce"],
        resources: { requests: { storage: "1Gi" } },
      },
    },
    {
      apiVersion: "v1",
      kind: "Service",
      metadata: { name: "postgres", namespace: "hello-kupe-db" },
      spec: {
        selector: { app: "hello-kupe-postgres" },
        ports: [{ port: 5432, targetPort: 5432 }],
      },
    },
    {
      apiVersion: "apps/v1",
      kind: "Deployment",
      metadata: { name: "postgres", namespace: "hello-kupe-db" },
      spec: {
        replicas: 1,
        strategy: { type: "Recreate" },
        selector: { matchLabels: { app: "hello-kupe-postgres" } },
        template: {
          metadata: { labels: { app: "hello-kupe-postgres" } },
          spec: {
            automountServiceAccountToken: false,
            initContainers: [
              {
                name: "prepare-tls",
                image: postgresImage,
                imagePullPolicy: "Never",
                command: [
                  "sh",
                  "-ec",
                  "cp /tls/* /server-tls/; chmod 600 /server-tls/server.key; chown 999:999 /server-tls/*",
                ],
                volumeMounts: [
                  { name: "tls", mountPath: "/tls", readOnly: true },
                  { name: "server-tls", mountPath: "/server-tls" },
                ],
              },
            ],
            containers: [
              {
                name: "postgres",
                image: postgresImage,
                imagePullPolicy: "Never",
                args: [
                  "-c",
                  "ssl=on",
                  "-c",
                  "ssl_cert_file=/server-tls/server.crt",
                  "-c",
                  "ssl_key_file=/server-tls/server.key",
                ],
                env: [
                  { name: "POSTGRES_DB", value: "hello_kupe" },
                  {
                    name: "POSTGRES_PASSWORD",
                    valueFrom: {
                      secretKeyRef: { name: "postgres-admin", key: "password" },
                    },
                  },
                ],
                ports: [{ containerPort: 5432 }],
                readinessProbe: {
                  exec: {
                    command: [
                      "pg_isready",
                      "-U",
                      "postgres",
                      "-d",
                      "hello_kupe",
                    ],
                  },
                  initialDelaySeconds: 2,
                  periodSeconds: 2,
                },
                resources: {
                  requests: { cpu: "100m", memory: "128Mi" },
                  limits: { cpu: "1", memory: "512Mi" },
                },
                volumeMounts: [
                  { name: "data", mountPath: "/var/lib/postgresql" },
                  {
                    name: "init",
                    mountPath: "/docker-entrypoint-initdb.d",
                    readOnly: true,
                  },
                  {
                    name: "server-tls",
                    mountPath: "/server-tls",
                    readOnly: true,
                  },
                ],
              },
            ],
            volumes: [
              {
                name: "data",
                persistentVolumeClaim: { claimName: "postgres-data" },
              },
              { name: "init", secret: { secretName: "postgres-init" } },
              { name: "tls", secret: { secretName: "postgres-tls" } },
              { name: "server-tls", emptyDir: {} },
            ],
          },
        },
      },
    },
  ]);
  await run("kubectl", [
    ...kubectl,
    "-n",
    "hello-kupe-db",
    "rollout",
    "status",
    "deployment/postgres",
    "--timeout=180s",
  ]);
  await run("helm", [
    "upgrade",
    "--install",
    "hello-kupe",
    "./chart",
    ...helm,
    "--namespace",
    "hello-kupe",
    "--values",
    "local/kind/values.yaml",
    "--wait",
    "--timeout",
    "180s",
  ]);
  // A reused local tag must also refresh already-running app pods.
  await run("kubectl", [
    ...kubectl,
    "-n",
    "hello-kupe",
    "rollout",
    "restart",
    "deployment/hello-kupe",
  ]);
  await run("kubectl", [
    ...kubectl,
    "-n",
    "hello-kupe",
    "rollout",
    "status",
    "deployment/hello-kupe",
    "--timeout=180s",
  ]);
  console.log(
    "\nPostgreSQL and two Hello Kupe replicas are ready. Run make kind-forward, then open http://localhost:18080.",
  );
}

async function forward() {
  let stopping = false;
  let child;
  for (const signal of ["SIGINT", "SIGTERM"])
    process.on(signal, () => {
      stopping = true;
      child?.kill("SIGTERM");
    });
  console.log(
    "Hello Kupe: http://localhost:18080 — Ctrl+C stops forwarding; the cluster and database remain.",
  );
  while (!stopping) {
    await new Promise((resolve, reject) => {
      child = spawn(
        "kubectl",
        [
          ...kubectl,
          "-n",
          "hello-kupe",
          "port-forward",
          "service/hello-kupe",
          "18080:80",
          "--address",
          "127.0.0.1",
        ],
        { stdio: "inherit" },
      );
      child.once("error", reject);
      child.once("close", resolve);
    });
    if (!stopping) await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

try {
  switch (process.argv[2] || "up") {
    case "up":
      await up();
      break;
    case "forward":
      await forward();
      break;
    case "status":
      await run("kubectl", [...kubectl, "get", "pods,pvc", "-A"]);
      break;
    case "down":
      await run("kind", [
        "delete",
        "cluster",
        "--name",
        cluster,
        "--kubeconfig",
        kubeconfig,
      ]);
      break;
    default:
      throw new Error("Use up, forward, status or down.");
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
