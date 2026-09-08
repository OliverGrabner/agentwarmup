import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { gzipSync, gunzipSync } from 'node:zlib';
import { packageNpm, verifyBinaryAssets } from './package-npm.mjs';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
const json = async (file) => JSON.parse(await readFile(file, 'utf8'));
const putJSON = (file, value) => writeFile(file, JSON.stringify(value));

async function fixture(t) {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'agentwarmup-npm-'));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const root = path.join(temporary, 'source ü & 100%');
  const output = path.join(temporary, 'release ü & 100%');
  await mkdir(output);
  for (const relative of ['npm/package.json', 'npm/manifest.json', 'npm/bin/agentwarmup.js', 'npm/lib/launcher.js', 'npm/README.md', 'LICENSE']) {
    await mkdir(path.dirname(path.join(root, relative)), { recursive: true });
    await copyFile(path.join(repo, relative), path.join(root, relative));
  }
  await writeFile(path.join(root, 'npm/lib/launcher.test.js'), 'throw new Error("must never be packaged");');
  await writeFile(path.join(root, 'npm/.npmrc'), 'this-is-development-only=true');
  const release = { repository: 'OliverGrabner/agentwarmup', command: 'agentwarmup', version: '1.2.3-preview.4', tag: 'v1.2.3-preview.4', targets: [] };
  const binaries = new Map();
  for (const targetOS of ['windows', 'darwin', 'linux']) {
    for (const arch of ['amd64', 'arm64']) {
      release.targets.push({ os: targetOS, arch });
      const work = path.join(output, `.work-${targetOS}-${arch}`);
      await mkdir(work);
      const bytes = Buffer.concat([Buffer.from(`fake executable: ${targetOS}/${arch}\0${release.version}\n`), Buffer.from([0, 255, 13, 10])]);
      await writeFile(path.join(work, targetOS === 'windows' ? 'agentwarmup.exe' : 'agentwarmup'), bytes);
      binaries.set(`${targetOS}-${arch}`, bytes);
    }
  }
  await putJSON(path.join(output, 'release.json'), release);
  return { root, output, release, binaries };
}

// npm's pack output is an ordinary tar.gz; inspect bytes without extracting paths to disk.
function tarFiles(bytes) {
  const tar = gunzipSync(bytes);
  const files = new Map();
  for (let offset = 0; offset + 512 <= tar.length;) {
    const header = tar.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) break;
    const text = (start, length) => header.subarray(start, start + length).toString().replace(/\0.*$/s, '');
    const name = text(0, 100);
    const size = Number.parseInt(text(124, 12).trim(), 8);
    const mode = Number.parseInt(text(100, 8).trim(), 8);
    assert.ok(Number.isSafeInteger(size));
    assert.ok(!files.has(name), `duplicate tar member ${name}`);
    files.set(name, { mode, bytes: tar.subarray(offset + 512, offset + 512 + size) });
    offset += 512 + Math.ceil(size / 512) * 512;
  }
  return files;
}

test('actual npm pack pins six binary assets and ships only the dependency-free launcher', async (t) => {
  const f = await fixture(t);
  // PowerShell 5 emits a BOM in release.json.
  await writeFile(path.join(f.output, 'release.json'), `\uFEFF${JSON.stringify(f.release)}`);
  const { filename, manifest } = await packageNpm(f);
  assert.equal(filename, `agentwarmup-${f.release.version}.tgz`);
  assert.deepEqual(Object.keys(manifest.targets).sort(), ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64', 'win32-arm64', 'win32-x64']);
  for (const [key, target] of Object.entries(manifest.targets)) {
    const [platform, architecture] = key.split('-');
    const targetOS = platform === 'win32' ? 'windows' : platform;
    const arch = architecture === 'x64' ? 'amd64' : architecture;
    const suffix = targetOS === 'windows' ? '.exe' : '';
    assert.equal(target.asset, `agentwarmup_${f.release.version}_${targetOS}_${arch}${suffix}.gz`);
    const compressed = await readFile(path.join(f.output, target.asset));
    assert.equal(hash(compressed), target.sha256);
    assert.equal(hash(f.binaries.get(`${targetOS}-${arch}`)), target.binarySha256);
    assert.deepEqual(gunzipSync(compressed), f.binaries.get(`${targetOS}-${arch}`));
  }
  const files = tarFiles(await readFile(path.join(f.output, filename)));
  assert.deepEqual([...files.keys()].sort(), ['package/LICENSE', 'package/README.md', 'package/bin/agentwarmup.js', 'package/lib/launcher.js', 'package/manifest.json', 'package/package.json']);
  const packed = JSON.parse(files.get('package/package.json').bytes);
  assert.equal(packed.version, f.release.version);
  assert.equal(packed.private, undefined);
  assert.equal(packed.scripts, undefined);
  assert.equal(packed.dependencies, undefined);
  assert.equal(packed.devDependencies, undefined);
  assert.deepEqual(packed.bin, { agentwarmup: 'bin/agentwarmup.js' });
  assert.ok(files.get('package/bin/agentwarmup.js').bytes.toString().startsWith('#!/usr/bin/env node\n'));
  // npm sets executable bits on installation; Windows cannot represent them while packing.
  if (process.platform !== 'win32') assert.ok(files.get('package/bin/agentwarmup.js').mode & 0o111);
  assert.deepEqual(JSON.parse(files.get('package/manifest.json').bytes), manifest);
  assert.equal((await json(path.join(f.root, 'npm/package.json'))).private, true);
  assert.equal((await json(path.join(f.root, 'npm/package.json'))).version, '0.0.0-development');
  assert.deepEqual((await json(path.join(f.root, 'npm/manifest.json'))).targets, {});
  assert.equal((await readdir(f.output)).some((entry) => entry.startsWith('.work-npm-')), false);
});

test('asset verification rejects corrupted compressed bytes, raw hash mismatches, and traversal', async (t) => {
  const f = await fixture(t);
  const bytes = gzipSync(Buffer.from('expected executable'));
  const target = { asset: 'binary.gz', sha256: hash(bytes), binarySha256: hash(Buffer.from('expected executable')) };
  const manifest = { targets: { 'linux-x64': target } };
  await writeFile(path.join(f.output, target.asset), bytes);
  await verifyBinaryAssets(f.output, manifest);
  await writeFile(path.join(f.output, target.asset), Buffer.concat([bytes, Buffer.from('corrupt')]));
  await assert.rejects(verifyBinaryAssets(f.output, manifest), /Compressed checksum mismatch/);
  await writeFile(path.join(f.output, target.asset), bytes);
  target.binarySha256 = '0'.repeat(64);
  await assert.rejects(verifyBinaryAssets(f.output, manifest), /Binary checksum mismatch/);
  target.asset = '../outside.gz';
  await assert.rejects(verifyBinaryAssets(f.output, manifest), /Invalid binary asset path/);
});

test('release versions, targets, and source package are validated before writing assets', async (t) => {
  const cases = [
    ['tag mismatch', (f) => { f.release.tag = 'v9.9.9'; }, /matching its tag/],
    ['version traversal', (f) => { f.release.version = '../outside'; }, /semantic version/],
    ['non-semver leading zero', (f) => { f.release.version = '01.2.3'; f.release.tag = 'v01.2.3'; }, /semantic version/],
    ['empty prerelease component', (f) => { f.release.version = '1.2.3-alpha..1'; f.release.tag = 'v1.2.3-alpha..1'; }, /semantic version/],
    ['target traversal', (f) => { f.release.targets[0].os = '../outside'; }, /Unsupported release target/],
    ['duplicate target', (f) => { f.release.targets[0] = f.release.targets[1]; }, /Duplicate release target/],
    ['missing target', (f) => { f.release.targets.pop(); }, /all six/],
    ['repository mismatch', (f) => { f.release.repository = 'someone/else'; }, /Unexpected release repository/],
    ['npm artifact mismatch', (f) => { f.release.npmPackage = 'agentwarmup-9.9.9.tgz'; }, /artifact version/],
    ['source version mismatch', async (f) => {
      const file = path.join(f.root, 'npm/package.json'); const pkg = await json(file); pkg.version = '9.9.9'; await putJSON(file, pkg);
    }, /private development package/],
    ['installation hook', async (f) => {
      const file = path.join(f.root, 'npm/package.json'); const pkg = await json(file); pkg.scripts.postinstall = 'node unsafe.js'; await putJSON(file, pkg);
    }, /no runtime dependencies or installation/],
    ['runtime dependency', async (f) => {
      const file = path.join(f.root, 'npm/package.json'); const pkg = await json(file); pkg.dependencies = { unneeded: '*' }; await putJSON(file, pkg);
    }, /no runtime dependencies/],
  ];
  for (const [name, change, expected] of cases) {
    await t.test(name, async (subtest) => {
      const f = await fixture(subtest);
      await change(f);
      await putJSON(path.join(f.output, 'release.json'), f.release);
      await assert.rejects(packageNpm(f), expected);
      assert.equal((await readdir(f.output)).some((entry) => entry.endsWith('.gz') || entry.endsWith('.tgz')), false);
    });
  }
});
