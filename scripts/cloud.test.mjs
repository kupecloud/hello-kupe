import { test } from "node:test";
import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  copyFile,
  writeFile,
  symlink,
  readFile,
  stat,
  rm,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const deployment = {
  tenant: "test-tenant",
  cluster: "demo",
  app_namespace: "custom-app",
  migration_namespace: "custom-migrate",
  owner_secret_name: "custom-owner",
  readwrite_secret_name: "custom-readwrite",
  registry_endpoint: "registry.kupe.cloud/test-tenant",
  url: "https://hello-kupe.demo.test-tenant.clusters.kupe.cloud",
};
async function fixture(t) {
  const dir = await mkdtemp(join(tmpdir(), "kupe-cloud-test-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  await mkdir(join(dir, "scripts"));
  await mkdir(join(dir, "bin"));
  await mkdir(join(dir, "infra"));
  await copyFile(
    new URL("./cloud.mjs", import.meta.url),
    join(dir, "scripts/cloud.mjs"),
  );
  await writeFile(
    join(dir, "infra/terraform.tfvars.example"),
    'tenant = "your-tenant"\n',
  );
  const mock = join(dir, "bin/mock");
  await writeFile(
    mock,
    `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path');
const name=path.basename(process.argv[1]),args=process.argv.slice(2);
let input='';if(args.includes('--password-stdin'))input=fs.readFileSync(0,'utf8');
fs.appendFileSync(process.env.CALLS,JSON.stringify({name,args,hasPassword:input==='test-secret\\n',apiToken:process.env.KUPE_API_TOKEN==='test-key'})+'\\n');
if(process.env.FAIL_BUILD&&name==='docker'&&args[0]==='buildx')process.exit(1);
if(name==='tofu'&&args.includes('output')){const key=args.at(-1);console.log(JSON.stringify(key==='deployment'?JSON.parse(process.env.DEPLOYMENT):key==='registry_build_username'?'test-user':'test-secret'));}
if(name==='kupe'&&args.includes('kubeconfig'))console.log('apiVersion: v1\\nclusters: []');
if(name==='docker'&&args.includes('--metadata-file'))fs.writeFileSync(args[args.indexOf('--metadata-file')+1],JSON.stringify({'containerimage.digest':'sha256:'+'a'.repeat(64)}));
`,
    { mode: 0o700 },
  );
  for (const name of ["tofu", "kupe", "docker", "helm", "kubectl"])
    await symlink(mock, join(dir, "bin", name));
  const env = {
    ...process.env,
    PATH: join(dir, "bin") + ":" + process.env.PATH,
    KUPE_API_KEY: "test-key",
    KUPE_TOKEN: "",
    TF: "tofu",
    CALLS: join(dir, "calls"),
    DEPLOYMENT: JSON.stringify(deployment),
  };
  const run = (action, extra = {}) =>
    spawnSync(process.execPath, [join(dir, "scripts/cloud.mjs"), action], {
      env: { ...env, ...extra },
      encoding: "utf8",
    });
  const calls = async () =>
    (await readFile(join(dir, "calls"), "utf8"))
      .trim()
      .split("\n")
      .map(JSON.parse);
  return { dir, run, calls };
}
test("deployment targets only Terraform cluster and namespaces, pins digest and keeps credentials private", async (t) => {
  const f = await fixture(t);
  const result = f.run("deploy");
  assert.equal(result.status, 0, result.stderr);
  const calls = await f.calls();
  const serialized = JSON.stringify(calls);
  assert(!serialized.includes("test-secret"));
  assert(!serialized.includes("test-key"));
  for (const call of calls.filter((c) =>
    ["kupe", "kubectl", "helm"].includes(c.name),
  )) {
    if (call.name === "kupe") {
      assert(call.apiToken);
      assert.equal(
        call.args[call.args.indexOf("--tenant") + 1],
        deployment.tenant,
      );
    } else {
      assert(call.args.includes("--kubeconfig"));
      assert.equal(
        call.args[
          call.args.indexOf(
            call.name === "helm" ? "--kube-context" : "--context",
          ) + 1
        ],
        "kupe-test-tenant-demo",
      );
    }
  }
  assert(
    calls.find((c) => c.name === "docker" && c.args[0] === "login").hasPassword,
  );
  const base = join(f.dir, ".tmp/cloud/test-tenant/demo");
  assert.equal((await stat(join(base, "kubeconfig"))).mode & 0o777, 0o600);
  const values = JSON.parse(await readFile(join(base, "values.json"), "utf8"));
  assert.equal(values.database.readwriteSecretName, "custom-readwrite");
  assert.equal(values.database.ownerSecretName, "custom-owner");
  assert.equal(values.migrations.namespace, "custom-migrate");
  assert.equal(values.replicaCount, 2);
  assert(values.image.tag.endsWith("@sha256:" + "a".repeat(64)));
  assert(calls.find((c) => c.name === "helm").args.includes("custom-app"));
  assert.equal(
    calls.filter((c) => c.name === "kubectl" && c.args.includes("--for=create"))
      .length,
    2,
  );
});
test("a failed image build stops before Helm", async (t) => {
  const f = await fixture(t);
  const result = f.run("deploy", { FAIL_BUILD: "1" });
  assert.notEqual(result.status, 0);
  assert(!(await f.calls()).some((c) => c.name === "helm"));
});
test("init preserves existing settings and requires an explicit tenant for new configuration", async (t) => {
  const f = await fixture(t);
  assert.notEqual(f.run("init", { DEMO_TENANT: "example" }).status, 0);
  assert.equal(f.run("init", { DEMO_TENANT: "new-demo" }).status, 0);
  const path = join(f.dir, "infra/terraform.tfvars");
  const first = await readFile(path, "utf8");
  assert(first.includes("new-demo"));
  assert.equal(f.run("init", { DEMO_TENANT: "different" }).status, 0);
  assert.equal(await readFile(path, "utf8"), first);
});
