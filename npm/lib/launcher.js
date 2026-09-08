import { createHash } from 'node:crypto';
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import https from 'node:https';
import { constants, tmpdir } from 'node:os';
import path from 'node:path';
import { gunzipSync } from 'node:zlib';

const repository = 'OliverGrabner/agentwarmup';
const platforms = { win32: 'windows', darwin: 'darwin', linux: 'linux' };
const architectures = { x64: 'amd64', arm64: 'arm64' };
const publicCommands = new Set(['status', 'setup', 'pause', 'resume', 'uninstall', 'help', 'version', '--help', '-h', '--version', '-v']);
const hashPattern = /^[a-f0-9]{64}$/;
export const limits = Object.freeze({ compressed: 64 * 1024 * 1024, binary: 128 * 1024 * 1024, timeout: 60_000, redirects: 5 });

export function childArguments(args) {
  let command;
  let home;
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--home') {
      if (home !== undefined || !args[i + 1] || args[i + 1].startsWith('--')) {
        throw new Error('--home requires one application directory');
      }
      home = args[++i];
    } else if (command === undefined && publicCommands.has(arg)) {
      command = arg;
    } else {
      throw new Error(`unsupported argument ${JSON.stringify(arg)}; use --help`);
    }
  }
  return [command ?? 'install', ...(home === undefined ? [] : ['--home', home])];
}

export function selectTarget(manifest, platform = process.platform, arch = process.arch) {
  if (!Object.hasOwn(platforms, platform) || !Object.hasOwn(architectures, arch)) {
    throw new Error(`unsupported platform ${platform}-${arch}`);
  }
  if (!manifest || manifest.repository !== repository ||
      typeof manifest.version !== 'string' || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(manifest.version)) {
    throw new Error('invalid embedded release manifest');
  }
  const key = `${platform}-${arch}`;
  const target = Object.hasOwn(manifest.targets ?? {}, key) ? manifest.targets[key] : undefined;
  if (!target) throw new Error(`this package has no release executable for ${key}; the source package is a development preview`);
  const filename = `agentwarmup_${manifest.version}_${platforms[platform]}_${architectures[arch]}${platform === 'win32' ? '.exe' : ''}.gz`;
  if (target.asset !== filename || typeof target.sha256 !== 'string' || !hashPattern.test(target.sha256) ||
      typeof target.binarySha256 !== 'string' || !hashPattern.test(target.binarySha256)) {
    throw new Error('invalid embedded executable metadata');
  }
  return { ...target, url: `https://github.com/${repository}/releases/download/v${manifest.version}/${filename}` };
}

function permittedURL(value) {
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.username || url.password || (url.port && url.port !== '443') ||
      !(url.hostname === 'github.com' || url.hostname.endsWith('.githubusercontent.com'))) {
    throw new Error('download redirected outside GitHub HTTPS assets');
  }
  return url;
}

// The deadline covers all redirects and streaming; no credentials or custom headers follow redirects.
export async function downloadAsset(url, { signal, request = https.get, maxBytes = limits.compressed, timeoutMs = limits.timeout, maxRedirects = limits.redirects } = {}) {
  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  const timeout = setTimeout(() => controller.abort(new Error('executable download timed out')), timeoutMs);
  try {
    async function fetchAsset(address, redirects) {
      controller.signal.throwIfAborted();
      const destination = permittedURL(address);
      return new Promise((resolve, reject) => {
        const req = request(destination, { signal: controller.signal, headers: { 'User-Agent': 'agentwarmup-npm', 'Accept-Encoding': 'identity' } }, response => {
          const status = response.statusCode;
          if ([301, 302, 303, 307, 308].includes(status)) {
            response.destroy();
            if (redirects >= maxRedirects) return reject(new Error('too many executable download redirects'));
            if (!response.headers.location) return reject(new Error('executable download redirect has no location'));
            try {
              resolve(fetchAsset(new URL(response.headers.location, destination), redirects + 1));
            } catch (error) { reject(error); }
            return;
          }
          if (status !== 200) {
            response.destroy();
            return reject(new Error(`executable download failed (HTTP ${status})`));
          }
          const contentLength = response.headers['content-length'];
          if (contentLength !== undefined && (!/^\d+$/.test(contentLength) || Number(contentLength) > maxBytes)) {
            response.destroy();
            return reject(new Error('executable download exceeds size limit'));
          }
          let size = 0;
          const chunks = [];
          response.on('error', reject);
          response.on('aborted', () => reject(new Error('executable download interrupted')));
          response.on('data', chunk => {
            size += chunk.length;
            if (size > maxBytes) {
              response.destroy();
              reject(new Error('executable download exceeds size limit'));
            } else chunks.push(chunk);
          });
          response.on('end', () => resolve(Buffer.concat(chunks, size)));
        });
        req.on('error', reject);
      });
    }
    return await fetchAsset(url, 0);
  } catch (error) {
    if (controller.signal.aborted) throw controller.signal.reason;
    throw error;
  } finally {
    clearTimeout(timeout);
    signal?.removeEventListener('abort', abort);
  }
}

export function verifiedBinary(compressed, target) {
  if (compressed.length > limits.compressed) throw new Error('executable download exceeds size limit');
  if (createHash('sha256').update(compressed).digest('hex') !== target.sha256) throw new Error('executable download checksum mismatch');
  let binary;
  try { binary = gunzipSync(compressed, { maxOutputLength: limits.binary }); }
  catch { throw new Error('invalid or oversized gzip executable'); }
  if (binary.length === 0 || createHash('sha256').update(binary).digest('hex') !== target.binarySha256) {
    throw new Error('executable checksum mismatch');
  }
  return binary;
}

function execute(binaryPath, args, { spawnProcess, signal, cancellationGraceMs, platform }) {
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    let timer;
    let processError;
    const child = spawnProcess(binaryPath, args, { stdio: 'inherit', shell: false, windowsHide: false });
    const abort = () => {
      const requestedSignal = signal.reason?.signal ?? 'SIGTERM';
      // Windows console Ctrl+C reaches both processes. Node's kill(SIGINT)
      // would terminate Go immediately, before it can restore the terminal.
      if (platform !== 'win32' || requestedSignal !== 'SIGINT') child.kill(requestedSignal);
      timer ??= setTimeout(() => child.kill('SIGKILL'), cancellationGraceMs);
      timer.unref?.();
    };
    signal.addEventListener('abort', abort, { once: true });
    if (signal.aborted) abort();
    child.on('error', error => { processError = error; });
    child.once('close', (code, childSignal) => {
      clearTimeout(timer);
      signal.removeEventListener('abort', abort);
      if (processError && !signal.aborted) return reject(processError);
      resolve(signal.aborted ? (signal.reason.signal === 'SIGINT' ? 130 : 143) : (code ?? (128 + (constants.signals[childSignal] ?? 15))));
    });
  });
}

export async function runLauncher(manifest, args, {
  platform = process.platform, arch = process.arch, tempRoot = tmpdir(),
  download = downloadAsset, spawnProcess = spawn, signals = process, cancellationGraceMs = 5000,
  output = console.log, progress = console.error,
} = {}) {
  const forwarded = childArguments(args);
  if (['help', '--help', '-h'].includes(forwarded[0])) {
    output('AgentWarmup npm launcher\n\n  agentwarmup            install or update, then setup on first run\n  agentwarmup status     show schedule health\n  agentwarmup setup      choose providers, reset time, and days\n  agentwarmup pause      pause future warmups\n  agentwarmup resume     resume your schedule\n  agentwarmup uninstall  remove AgentWarmup\n  agentwarmup --version  show package version\n\nThe executable runs locally using your existing provider CLI login.\nKeep this computer awake, online, and logged in for scheduled requests.');
    return 0;
  }
  if (['version', '--version', '-v'].includes(forwarded[0])) {
    output(`agentwarmup ${manifest.version}`);
    return 0;
  }
  const target = selectTarget(manifest, platform, arch);
  const controller = new AbortController();
  const interrupt = signal => controller.abort(Object.assign(new Error('cancelled'), { signal }));
  const onInterrupt = () => interrupt('SIGINT');
  const onTerminate = () => interrupt('SIGTERM');
  signals.on('SIGINT', onInterrupt);
  signals.on('SIGTERM', onTerminate);
  let directory;
  try {
    progress(`Preparing AgentWarmup ${manifest.version}...`);
    const compressed = await download(target.url, { signal: controller.signal });
    controller.signal.throwIfAborted();
    const binary = verifiedBinary(compressed, target);
    directory = await mkdtemp(path.join(tempRoot, 'agentwarmup-'));
    const binaryPath = path.join(directory, platform === 'win32' ? 'agentwarmup.exe' : 'agentwarmup');
    await writeFile(binaryPath, binary, { flag: 'wx', mode: 0o700 });
    return await execute(binaryPath, forwarded, { spawnProcess, signal: controller.signal, cancellationGraceMs, platform });
  } catch (error) {
    if (controller.signal.aborted) return controller.signal.reason.signal === 'SIGINT' ? 130 : 143;
    throw error;
  } finally {
    signals.removeListener('SIGINT', onInterrupt);
    signals.removeListener('SIGTERM', onTerminate);
    // This exact mkdtemp result is the only directory the launcher ever removes.
    if (directory) await rm(directory, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
}
