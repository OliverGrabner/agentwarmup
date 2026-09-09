import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { lstat, mkdir, mkdtemp, readdir, realpath, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { downloadAsset } from '../../npm/lib/launcher.js';
import { npmCLI, versionPattern } from './package-npm.mjs';

const repository = 'OliverGrabner/agentwarmup';
const tag = process.env.RELEASE_TAG ?? '';
const version = tag.slice(1);
assert.equal(process.env.GITHUB_REPOSITORY, repository, 'Published smoke is restricted to the AgentWarmup repository.');
assert.ok(tag.startsWith('v') && version.includes('-') && versionPattern.test(version), 'RELEASE_TAG must be an exact prerelease semantic version beginning with v.');
assert.ok(process.argv.length === 2 || (process.argv.length === 3 && process.argv[2] === '--check-only'), 'Only --check-only is accepted.');
const asset = `agentwarmup-${version}.tgz`;
const baseURL = `https://github.com/${repository}/releases/download/${tag}/`;
if (process.argv[2] === '--check-only') {
  console.log(`Validated published preview: ${baseURL}${asset}`);
  process.exit(0);
}

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const temporary = await realpath(os.tmpdir());
const root = await mkdtemp(path.join(temporary, 'agentwarmup-published-smoke-'));
const appRoot = path.join(root, 'fresh-application');
const profile = path.join(root, 'profile');
const configRoot = path.join(root, 'config');
const downloads = path.join(root, 'temporary-downloads');
const taskName = `agentwarmup-${hash(process.platform === 'win32' ? appRoot.toLowerCase() : appRoot).slice(0, 16)}`;
const definitions = process.platform === 'win32'
  ? [path.join(process.env.SystemRoot, 'System32', 'Tasks', taskName)]
  : process.platform === 'darwin'
    ? [path.join(profile, 'Library', 'LaunchAgents', `com.${taskName}.plist`)]
    : ['service', 'timer'].map(ext => path.join(configRoot, 'systemd', 'user', `${taskName}.${ext}`));
async function absent(file) {
  try { await lstat(file); assert.fail(`Smoke created unexpected application/scheduler files: ${file}`); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
}
try {
  for (const dir of [profile, configRoot, downloads]) await mkdir(dir);
  for (const file of [appRoot, ...definitions]) await absent(file);
  const checksums = (await downloadAsset(`${baseURL}checksums.txt`, { maxBytes: 64 * 1024 })).toString('utf8');
  const entries = checksums.split(/\r?\n/).map(line => /^([a-f0-9]{64}) {2}(.+)$/.exec(line)).filter(Boolean);
  const matching = entries.filter(entry => entry[2] === asset);
  assert.equal(matching.length, 1, 'Release checksums must name the exact npm tarball once.');
  const tarball = await downloadAsset(`${baseURL}${asset}`);
  assert.equal(hash(tarball), matching[0][1], 'Published npm tarball checksum mismatch.');
  const localTarball = path.join(root, asset);
  await writeFile(localTarball, tarball, { flag: 'wx' });
  // npm exec is npx's implementation. Execute verified local bytes so a second
  // URL fetch cannot replace the tarball after checksum verification.
  const result = spawnSync(process.execPath, [await npmCLI(), 'exec', '--offline', '--yes', '--ignore-scripts', '--package', localTarball, '--', 'agentwarmup', 'status', '--home', appRoot], {
    cwd: root, encoding: 'utf8', windowsHide: true, timeout: 180_000, maxBuffer: 4 * 1024 * 1024,
    env: { ...process.env, npm_config_cache: path.join(root, 'npm-cache'), npm_config_update_notifier: 'false',
      HOME: profile, USERPROFILE: profile, APPDATA: path.join(profile, 'AppData', 'Roaming'), LOCALAPPDATA: path.join(profile, 'AppData', 'Local'),
      XDG_CONFIG_HOME: configRoot, CODEX_HOME: path.join(profile, 'codex'), CLAUDE_CONFIG_DIR: path.join(profile, 'claude'),
      TMPDIR: downloads, TMP: downloads, TEMP: downloads,
    },
  });
  assert.ifError(result.error);
  const output = `${result.stdout ?? ''}${result.stderr ?? ''}`;
  console.log(output.trim());
  assert.equal(result.status, 1, 'Fresh-home status must return the native not-set-up error.');
  assert.ok(output.includes(`Preparing AgentWarmup ${version}...`), 'Published launcher did not prepare the native executable.');
  assert.match(output, /not set up yet; run agentwarmup setup/, 'Published native executable did not report fresh-home status.');
  for (const file of [appRoot, ...definitions]) await absent(file);
  assert.deepEqual(await readdir(downloads), [], 'Temporary native executable was not cleaned up.');
  console.log(`Passed published npm/native smoke: ${tag}, ${process.platform}-${process.arch}, Node ${process.versions.node}; no application or job created.`);
} finally {
  const resolved = await realpath(root);
  assert.equal(path.dirname(resolved), temporary);
  assert.equal((await lstat(root)).isSymbolicLink(), false);
  await rm(resolved, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
}
