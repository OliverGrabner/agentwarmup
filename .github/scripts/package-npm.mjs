import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { chmod, copyFile, lstat, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';
import { gzipSync, gunzipSync } from 'node:zlib';

const repository = 'OliverGrabner/agentwarmup';
const sourceVersion = '0.0.0-development';
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?$/;
const platforms = { windows: 'win32', darwin: 'darwin', linux: 'linux' };
const architectures = { amd64: 'x64', arm64: 'arm64' };
const runtimeFiles = ['bin/agentwarmup.js', 'lib/launcher.js', 'README.md'];
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
const readJSON = async (file) => JSON.parse((await readFile(file, 'utf8')).replace(/^\uFEFF/, ''));
const writeJSON = (file, value) => writeFile(file, `${JSON.stringify(value, null, 2)}\n`, { flag: 'wx' });

async function ownedFile(root, relative) {
  const file = path.join(root, relative);
  const resolved = await realpath(file);
  if (!resolved.startsWith(root + path.sep) || !(await lstat(file)).isFile()) {
    throw new Error(`Expected an ordinary file within its source directory: ${relative}`);
  }
  return file;
}

// Re-read every asset before packing so the embedded hashes describe verified bytes on disk.
export async function verifyBinaryAssets(output, manifest) {
  output = await realpath(output);
  for (const target of Object.values(manifest.targets)) {
    if (path.basename(target.asset) !== target.asset || /[\\/]/.test(target.asset)) {
      throw new Error('Invalid binary asset path.');
    }
    const compressed = await readFile(await ownedFile(output, target.asset));
    if (hash(compressed) !== target.sha256) throw new Error(`Compressed checksum mismatch: ${target.asset}`);
    const binary = gunzipSync(compressed);
    if (hash(binary) !== target.binarySha256) throw new Error(`Binary checksum mismatch: ${target.asset}`);
  }
}

async function npmCLI() {
  // Launch npm's JavaScript entry point directly; .cmd files require unsafe shell invocation.
  const roots = [path.dirname(process.execPath), ...(process.env.PATH ?? '').split(path.delimiter)];
  const candidates = process.env.npm_execpath ? [process.env.npm_execpath] : [];
  for (const root of roots) {
    candidates.push(path.join(root, 'node_modules/npm/bin/npm-cli.js'));
    candidates.push(path.resolve(root, '../lib/node_modules/npm/bin/npm-cli.js'));
    try { candidates.push(await realpath(path.join(root, 'npm'))); } catch { /* Try the next installation. */ }
  }
  for (const candidate of candidates) {
    if (path.basename(candidate) === 'npm-cli.js') {
      try { if ((await lstat(candidate)).isFile()) return candidate; } catch { /* Try the next installation. */ }
    }
  }
  throw new Error('Cannot find npm-cli.js. Install Node.js 22 or newer with npm.');
}

export async function packageNpm({ output, root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..') }) {
  if (Number(process.versions.node.split('.')[0]) < 22) throw new Error('Packaging requires Node.js 22 or newer.');
  output = await realpath(output);
  root = await realpath(root);
  const release = await readJSON(await ownedFile(output, 'release.json'));
  if (!versionPattern.test(release.version) || release.version === sourceVersion || release.tag !== `v${release.version}`) {
    throw new Error('Release version must be an exact semantic version matching its tag.');
  }
  if (release.repository !== repository || release.command !== 'agentwarmup') throw new Error('Unexpected release repository or command.');
  if (release.npmPackage !== undefined && release.npmPackage !== `agentwarmup-${release.version}.tgz`) throw new Error('npm artifact version does not match the release.');
  const source = await realpath(path.join(root, 'npm'));
  const pkg = await readJSON(await ownedFile(source, 'package.json'));
  const sourceManifest = await readJSON(await ownedFile(source, 'manifest.json'));
  if (pkg.name !== 'agentwarmup' || pkg.private !== true || pkg.version !== sourceVersion || sourceManifest.version !== pkg.version || sourceManifest.repository !== repository || Object.keys(sourceManifest.targets ?? {}).length !== 0) {
    throw new Error('npm source must remain a private development package with a matching empty manifest.');
  }
  if (['dependencies', 'optionalDependencies', 'peerDependencies', 'bundledDependencies'].some((key) => Object.keys(pkg[key] ?? {}).length) || ['preinstall', 'install', 'postinstall', 'prepare', 'prepack', 'postpack'].some((key) => pkg.scripts?.[key])) {
    throw new Error('The npm launcher must have no runtime dependencies or installation/packaging hooks.');
  }
  const manifest = { version: release.version, repository, targets: {} };
  if (!Array.isArray(release.targets) || release.targets.length !== 6) throw new Error('Release must contain all six supported targets.');
  // Validate all target names before using them in paths or writing assets.
  for (const target of release.targets) {
    if (!Object.hasOwn(platforms, target.os) || !Object.hasOwn(architectures, target.arch)) throw new Error('Unsupported release target.');
    const key = `${platforms[target.os]}-${architectures[target.arch]}`;
    if (Object.hasOwn(manifest.targets, key)) throw new Error('Duplicate release target.');
    manifest.targets[key] = {};
  }
  for (const target of release.targets) {
    const key = `${platforms[target.os]}-${architectures[target.arch]}`;
    const suffix = target.os === 'windows' ? '.exe' : '';
    const binary = await readFile(await ownedFile(output, `.work-${target.os}-${target.arch}/agentwarmup${suffix}`));
    if (binary.length === 0) throw new Error(`Empty executable for ${key}.`);
    const compressed = gzipSync(binary, { level: 9 });
    const asset = `agentwarmup_${release.version}_${target.os}_${target.arch}${suffix}.gz`;
    await writeFile(path.join(output, asset), compressed, { flag: 'wx' });
    manifest.targets[key] = { asset, sha256: hash(compressed), binarySha256: hash(binary) };
  }
  await verifyBinaryAssets(output, manifest);
  // npm parses its working path as a package spec, so keep output-path punctuation out of it.
  const temporary = await realpath(os.tmpdir());
  const stage = await mkdtemp(path.join(temporary, 'agentwarmup-npm-package-'));
  try {
    for (const relative of runtimeFiles) {
      const destination = path.join(stage, relative);
      await mkdir(path.dirname(destination), { recursive: true });
      await copyFile(await ownedFile(source, relative), destination);
    }
    await chmod(path.join(stage, 'bin/agentwarmup.js'), 0o755);
    await copyFile(await ownedFile(root, 'LICENSE'), path.join(stage, 'LICENSE'));
    const packed = { ...pkg, version: release.version, files: [...runtimeFiles, 'manifest.json', 'LICENSE'] };
    delete packed.private;
    delete packed.scripts;
    delete packed.devDependencies;
    await writeJSON(path.join(stage, 'package.json'), packed);
    await writeJSON(path.join(stage, 'manifest.json'), manifest);
    const filename = `agentwarmup-${release.version}.tgz`;
    try { await lstat(path.join(output, filename)); throw new Error('npm tarball already exists.'); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    const results = JSON.parse(execFileSync(process.execPath, [await npmCLI(), 'pack', '--ignore-scripts', '--json', '--offline', '--pack-destination', output], {
      cwd: stage,
      encoding: 'utf8',
      windowsHide: true,
      env: { ...process.env, npm_config_cache: path.join(stage, '.npm-cache') },
    }));
    if (results.length !== 1 || results[0].filename !== filename || results[0].version !== release.version) throw new Error('npm packed an unexpected package or version.');
    const expected = [...runtimeFiles, 'LICENSE', 'manifest.json', 'package.json'].sort();
    const actual = results[0].files.map((file) => file.path).sort();
    if (JSON.stringify(expected) !== JSON.stringify(actual)) throw new Error('npm tarball contains unexpected files.');
    return { filename, manifest };
  } finally {
    const resolved = await realpath(stage);
    if (path.dirname(resolved) !== temporary || (await lstat(stage)).isSymbolicLink()) throw new Error('npm staging directory escaped its temporary parent.');
    await rm(resolved, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv.length !== 3) throw new Error('Usage: node .github/scripts/package-npm.mjs <release-output>');
    const { filename } = await packageNpm({ output: process.argv[2] });
    process.stdout.write(`Packed ${filename}\n`);
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
