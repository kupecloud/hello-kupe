import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile, rename, access } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const work = join(root, ".tmp/cloud");
const tf = process.env.TF || "tofu";
const action = process.argv[2];

// No shell interpolation: credentials stay in the environment or private files.
async function run(command, args, { capture = false, input, env = {} } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: root,
      env: { ...process.env, ...env },
      stdio: [
        input === undefined ? "inherit" : "pipe",
        capture ? "pipe" : "inherit",
        "inherit",
      ],
    });
    let output = "";
    child.stdout?.on("data", (data) => (output += data));
    child.on("error", reject);
    child.on("close", (code) =>
      code === 0
        ? resolve(output)
        : reject(new Error(`${command} failed (${code}).`)),
    );
    if (input !== undefined) child.stdin.end(input);
  });
}
function requireKey() {
  if (!process.env.KUPE_API_KEY)
    throw new Error("Export KUPE_API_KEY for your demo tenant first.");
  if (process.env.KUPE_TOKEN)
    throw new Error("Unset KUPE_TOKEN: this workflow uses KUPE_API_KEY.");
}
async function privateFile(path, content) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  const temporary = `${path}.tmp`;
  await writeFile(temporary, content, { mode: 0o600 });
  await rename(temporary, path);
}
const tofu = (...args) => run(tf, ["-chdir=infra", ...args]);
const output = async (name) =>
  JSON.parse(
    await run(tf, ["-chdir=infra", "output", "-json", name], { capture: true }),
  );

async function config() {
  const cfg = await output("deployment");
  for (const key of [
    "tenant",
    "cluster",
    "app_namespace",
    "migration_namespace",
    "owner_secret_name",
    "readwrite_secret_name",
  ]) {
    if (!cfg[key] || !/^[a-z0-9][a-z0-9-]*$/.test(cfg[key]))
      throw new Error(
        `Invalid Terraform deployment.${key}; run make cloud-apply first.`,
      );
  }
  cfg.context = `kupe-${cfg.tenant}-${cfg.cluster}`;
  cfg.kubeconfig = join(work, cfg.tenant, cfg.cluster, "kubeconfig");
  cfg.values = join(work, cfg.tenant, cfg.cluster, "values.json");
  return cfg;
}
function kupe(cfg, args, options = {}) {
  return run(
    "kupe",
    ["--api-url", "https://api.kupe.cloud", "--tenant", cfg.tenant, ...args],
    {
      ...options,
      env: { KUPE_API_TOKEN: process.env.KUPE_API_KEY },
    },
  );
}
function kubeArgs(cfg) {
  return ["--kubeconfig", cfg.kubeconfig, "--context", cfg.context];
}
async function kubeconfig(cfg) {
  await kupe(cfg, [
    "cluster",
    "wait",
    cfg.cluster,
    "--for",
    "running",
    "--timeout",
    "15m",
  ]);
  const yaml = await kupe(
    cfg,
    ["cluster", "kubeconfig", cfg.cluster, "--context-name", cfg.context],
    { capture: true },
  );
  if (!yaml.includes("apiVersion:") || !yaml.includes("clusters:"))
    throw new Error("Kupe did not return a kubeconfig.");
  await privateFile(cfg.kubeconfig, yaml);
  console.log(`Private kubeconfig: ${cfg.kubeconfig}`);
}
async function deploy(cfg) {
  if (
    !cfg.registry_endpoint ||
    !/^registry\.kupe\.cloud\/[a-z0-9-]+$/.test(cfg.registry_endpoint)
  ) {
    throw new Error(
      "Enable the managed registry and apply infrastructure before deploying.",
    );
  }
  // Read only these outputs; never print a full Terraform state/output document.
  const username = await output("registry_build_username");
  const secret = await output("registry_build_secret");
  if (!username || !secret)
    throw new Error(
      "Set enable_build_token = true and run make cloud-apply, then retry.",
    );
  await kubeconfig(cfg);
  for (const [namespace, name] of [
    [cfg.app_namespace, cfg.readwrite_secret_name],
    [cfg.migration_namespace, cfg.owner_secret_name],
  ]) {
    await run("kubectl", [
      ...kubeArgs(cfg),
      "-n",
      namespace,
      "wait",
      "--for=create",
      `secret/${name}`,
      "--timeout=5m",
    ]);
  }
  await run(
    "docker",
    [
      "login",
      "registry.kupe.cloud",
      "--username",
      username,
      "--password-stdin",
    ],
    { input: `${secret}\n` },
  );
  const repository = `${cfg.registry_endpoint}/hello-kupe`;
  const tag = `demo-${Date.now()}`;
  const metadata = join(dirname(cfg.values), "build.json");
  await run("docker", [
    "buildx",
    "build",
    "--platform",
    "linux/amd64,linux/arm64",
    "--tag",
    `${repository}:${tag}`,
    "--metadata-file",
    metadata,
    "--push",
    ".",
  ]);
  const digest = JSON.parse(await readFile(metadata, "utf8"))[
    "containerimage.digest"
  ];
  if (!/^sha256:[a-f0-9]{64}$/.test(digest || ""))
    throw new Error(
      "Build did not return an image digest; deployment stopped.",
    );
  await privateFile(
    cfg.values,
    JSON.stringify(
      {
        tenant: cfg.tenant,
        cluster: cfg.cluster,
        replicaCount: 2,
        image: { repository, tag: `${tag}@${digest}` },
        database: {
          enabled: true,
          ownerSecretName: cfg.owner_secret_name,
          readwriteSecretName: cfg.readwrite_secret_name,
        },
        migrations: { namespace: cfg.migration_namespace },
      },
      null,
      2,
    ) + "\n",
  );
  await run("helm", [
    "upgrade",
    "--install",
    "hello-kupe",
    "./chart",
    "--kubeconfig",
    cfg.kubeconfig,
    "--kube-context",
    cfg.context,
    "--namespace",
    cfg.app_namespace,
    "--reset-values",
    "--values",
    cfg.values,
    "--wait",
    "--timeout",
    "10m",
  ]);
  await run("kubectl", [
    ...kubeArgs(cfg),
    "-n",
    cfg.app_namespace,
    "rollout",
    "status",
    "deployment/hello-kupe",
    "--timeout=5m",
  ]);
  console.log(`\nHello Kupe: ${cfg.url}\nHelm values: ${cfg.values}`);
}

try {
  switch (action) {
    case "init": {
      const path = join(root, "infra/terraform.tfvars");
      try {
        await access(path);
      } catch {
        const tenant = process.env.DEMO_TENANT;
        if (
          !tenant ||
          tenant === "example" ||
          !/^[a-z][a-z0-9-]*$/.test(tenant)
        )
          throw new Error(
            "Use make cloud-init TENANT=your-tenant (or create infra/terraform.tfvars yourself).",
          );
        const template = await readFile(
          join(root, "infra/terraform.tfvars.example"),
          "utf8",
        );
        await writeFile(
          path,
          template.replace('tenant = "your-tenant"', `tenant = "${tenant}"`),
          { flag: "wx", mode: 0o600 },
        );
        console.log(
          "Created infra/terraform.tfvars for your tenant; review before applying.",
        );
      }
      await tofu("init");
      break;
    }
    case "plan":
      requireKey();
      await tofu("plan");
      break;
    case "apply":
      requireKey();
      await tofu("apply");
      break;
    case "kubeconfig":
      requireKey();
      await kubeconfig(await config());
      break;
    case "deploy":
      requireKey();
      await deploy(await config());
      break;
    case "status": {
      const cfg = await config();
      await run("kubectl", [
        ...kubeArgs(cfg),
        "-n",
        cfg.app_namespace,
        "get",
        "pods,svc,httproute",
      ]);
      console.log(cfg.url);
      break;
    }
    default:
      throw new Error("Use init, plan, apply, kubeconfig, deploy or status.");
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
