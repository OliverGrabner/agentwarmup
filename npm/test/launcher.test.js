import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { PassThrough } from 'node:stream';
import { gzipSync } from 'node:zlib';
import { childArguments, selectTarget, downloadAsset, verifiedBinary, runLauncher } from '../lib/launcher.js';

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const binary = Buffer.from('fake executable; never invokes a provider');
const compressed = gzipSync(binary);
function manifest(platform = 'linux', arch = 'x64') {
  const os = { win32: 'windows', linux: 'linux', darwin: 'darwin' }[platform];
  return {
    version: '1.2.3-preview.1', repository: 'OliverGrabner/agentwarmup',
    targets: { [`${platform}-${arch}`]: {
      asset: `agentwarmup_1.2.3-preview.1_${os}_${arch === 'x64' ? 'amd64' : 'arm64'}${platform === 'win32' ? '.exe' : ''}.gz`,
      sha256: hash(compressed), binarySha256: hash(binary),
    } },
  };
}

test('no arguments installs while explicit public commands stay management commands', () => {
  assert.deepEqual(childArguments([]), ['install']);
  assert.deepEqual(childArguments(['--home', 'space & Unicode ö']), ['install', '--home', 'space & Unicode ö']);
  for (const command of ['status', 'setup', 'pause', 'resume', 'uninstall', 'help', 'version', '--help', '-h', '--version', '-v']) {
    assert.deepEqual(childArguments([command, '--home', 'isolated']), [command, '--home', 'isolated']);
  }
  for (const args of [['check'], ['warmup'], ['install'], ['run-now'], ['--home'], ['--home', ''], ['--home', '--version'], ['status', 'pause'], ['--home', 'a', '--home', 'b']]) {
    assert.throws(() => childArguments(args));
  }
});

test('all six targets select an exact versioned GitHub asset', () => {
  for (const platform of ['linux', 'darwin', 'win32']) {
    for (const arch of ['x64', 'arm64']) {
      const target = selectTarget(manifest(platform, arch), platform, arch);
      assert.equal(target.url, `https://github.com/OliverGrabner/agentwarmup/releases/download/v1.2.3-preview.1/${target.asset}`);
    }
  }
  assert.throws(() => selectTarget(manifest(), 'linux', 'ia32'), /unsupported platform/);
  assert.throws(() => selectTarget(manifest(), 'freebsd', 'x64'), /unsupported platform/);
  assert.throws(() => selectTarget({ ...manifest(), targets: {} }, 'linux', 'x64'), /development preview/);
  for (const mutation of [m => m.repository = 'attacker/repo', m => m.version = '../other', m => m.targets['linux-x64'].asset = '../program.gz', m => m.targets['linux-x64'].sha256 = '0']) {
    const m = manifest(); mutation(m);
    assert.throws(() => selectTarget(m, 'linux', 'x64'), /invalid embedded/);
  }
});

test('gzip and decompressed executable are both verified', () => {
  const target = selectTarget(manifest(), 'linux', 'x64');
  assert.deepEqual(verifiedBinary(compressed, target), binary);
  assert.throws(() => verifiedBinary(Buffer.from('tampered'), target), /download checksum mismatch/);
  assert.throws(() => verifiedBinary(compressed, { ...target, binarySha256: '0'.repeat(64) }), /executable checksum mismatch/);
  const invalid = Buffer.from('not gzip');
  assert.throws(() => verifiedBinary(invalid, { ...target, sha256: hash(invalid) }), /invalid or oversized gzip/);
});

function fakeRequest(replies, seen = []) {
  return (url, options, callback) => {
    seen.push({ url: String(url), options });
    const req = new EventEmitter();
    const response = new PassThrough();
    const onAbort = () => { response.destroy(); req.emit('error', options.signal.reason); };
    options.signal.addEventListener('abort', onAbort, { once: true });
    response.on('close', () => options.signal.removeEventListener('abort', onAbort));
    queueMicrotask(() => {
      const reply = replies.shift();
      if (reply.error) { req.emit('error', new Error(reply.error)); response.destroy(); return; }
      response.statusCode = reply.status ?? 200;
      response.headers = reply.headers ?? {};
      callback(response);
      if (reply.body !== undefined) response.end(reply.body);
    });
    return req;
  };
}

const url = 'https://github.com/OliverGrabner/agentwarmup/releases/download/v1.2.3/example.gz';
test('HTTPS downloader follows bounded GitHub redirects without accepting other destinations', async () => {
  const seen = [];
  assert.deepEqual(await downloadAsset(url, { request: fakeRequest([
    { status: 302, headers: { location: 'https://release-assets.githubusercontent.com/file?token=fixture' } },
    { body: compressed },
  ], seen) }), compressed);
  assert.equal(seen.length, 2);
  for (const location of ['http://github.com/file', 'https://example.org/file', 'https://github.com.evil.org/file', 'https://user:pass@github.com/file', 'https://github.com:444/file']) {
    await assert.rejects(downloadAsset(url, { request: fakeRequest([{ status: 302, headers: { location } }]) }), /outside GitHub HTTPS/);
  }
  await assert.rejects(downloadAsset(url, { maxRedirects: 0, request: fakeRequest([{ status: 302 }]) }), /too many/);
  await assert.rejects(downloadAsset(url, { request: fakeRequest([{ status: 302 }]) }), /no location/);
});

test('download failures, declared and streamed limits, and timeouts fail closed', async () => {
  await assert.rejects(downloadAsset(url, { request: fakeRequest([{ status: 404 }]) }), /HTTP 404/);
  await assert.rejects(downloadAsset(url, { request: fakeRequest([{ error: 'network unavailable' }]) }), /network unavailable/);
  await assert.rejects(downloadAsset(url, { maxBytes: 2, request: fakeRequest([{ headers: { 'content-length': '3' } }]) }), /size limit/);
  await assert.rejects(downloadAsset(url, { maxBytes: 2, request: fakeRequest([{ body: Buffer.from('123') }]) }), /size limit/);
  await assert.rejects(downloadAsset(url, { timeoutMs: 10, request: fakeRequest([{}]) }), /timed out/);
  const controller = new AbortController();
  controller.abort(new Error('cancel fixture'));
  await assert.rejects(downloadAsset(url, { signal: controller.signal, request: () => assert.fail('must not request') }), /cancel fixture/);
});

test('help and version work in private source package without platform/download checks', async () => {
  const output = [];
  const opts = { platform: 'unsupported', download: () => assert.fail('no download'), output: line => output.push(line), progress: () => assert.fail('no preparation') };
  const source = { version: '0.0.0-development', targets: {} };
  assert.equal(await runLauncher(source, ['--version'], opts), 0);
  assert.equal(output[0], 'agentwarmup 0.0.0-development');
  assert.equal(await runLauncher(source, ['--help'], opts), 0);
  assert.match(output[1], /install or update/);
});

async function temporary(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'agentwarmup-npm-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(path.join(root, 'unrelated'), 'keep');
  return root;
}

test('verified temporary executable forwards arguments, terminal and exit code, then cleans only owned files', async t => {
  const tempRoot = await temporary(t);
  for (const args of [[], ['status'], ['setup', '--home', path.join(tempRoot, 'isolated & ö')], ['uninstall']]) {
    const signals = new EventEmitter();
    const progress = [];
    const result = await runLauncher(manifest(), args, {
      platform: 'linux', arch: 'x64', tempRoot, signals, progress: line => progress.push(line),
      download: async () => { assert.deepEqual(progress, ['Preparing AgentWarmup 1.2.3-preview.1...']); return compressed; },
      spawnProcess: (file, forwarded, options) => {
        assert.equal(path.dirname(path.dirname(file)), tempRoot);
        assert.deepEqual(forwarded, childArguments(args));
        assert.deepEqual(options, { stdio: 'inherit', shell: false, windowsHide: false });
        const child = new EventEmitter();
        readFile(file).then(bytes => { assert.deepEqual(bytes, binary); child.emit('close', 7, null); });
        return child;
      },
    });
    assert.equal(result, 7);
    assert.deepEqual(await readdir(tempRoot), ['unrelated']);
    assert.equal(signals.listenerCount('SIGINT'), 0);
    assert.equal(signals.listenerCount('SIGTERM'), 0);
  }
});

test('checksum, download, and spawn errors never leave an executable directory', async t => {
  const tempRoot = await temporary(t);
  const opts = { platform: 'linux', arch: 'x64', tempRoot, signals: new EventEmitter(), spawnProcess: () => assert.fail('must not spawn') };
  await assert.rejects(runLauncher(manifest(), [], { ...opts, download: async () => Buffer.from('tampered') }), /checksum/);
  await assert.rejects(runLauncher(manifest(), [], { ...opts, download: async () => { throw new Error('offline'); } }), /offline/);
  await assert.rejects(runLauncher(manifest(), [], {
    ...opts, download: async () => compressed,
    spawnProcess: () => { const child = new EventEmitter(); queueMicrotask(() => { child.emit('error', new Error('spawn failed')); child.emit('close', -2); }); return child; },
  }), /spawn failed/);
  assert.deepEqual(await readdir(tempRoot), ['unrelated']);
});

test('a real harmless child exit is propagated and cleaned', async t => {
  const tempRoot = await temporary(t);
  const code = await runLauncher(manifest(), ['status'], {
    platform: 'linux', arch: 'x64', tempRoot, download: async () => compressed,
    spawnProcess: (_file, _args, options) => spawn(process.execPath, ['-e', 'process.exit(19)'], options),
  });
  assert.equal(code, 19);
  assert.deepEqual(await readdir(tempRoot), ['unrelated']);
});

test('child termination reports the conventional signal exit code', async t => {
  const tempRoot = await temporary(t);
  assert.equal(await runLauncher(manifest(), ['status'], {
    platform: 'linux', arch: 'x64', tempRoot, download: async () => compressed,
    spawnProcess: () => {
      const child = new EventEmitter();
      queueMicrotask(() => child.emit('close', null, 'SIGKILL'));
      return child;
    },
  }), 137);
  assert.deepEqual(await readdir(tempRoot), ['unrelated']);
});

test('cancellation forwards the signal, escalates a stuck child, awaits close, and cleans', async t => {
  const tempRoot = await temporary(t);
  for (const signal of ['SIGINT', 'SIGTERM']) {
    const signals = new EventEmitter();
    const killed = [];
    const keepAlive = setTimeout(() => {}, 1000);
    try {
      assert.equal(await runLauncher(manifest(), [], {
        platform: 'linux', arch: 'x64', tempRoot, signals, cancellationGraceMs: 5, download: async () => compressed,
        spawnProcess: () => {
          const child = new EventEmitter();
          child.kill = value => {
            killed.push(value);
            if (value === 'SIGKILL') queueMicrotask(() => child.emit('close', null, value));
          };
          queueMicrotask(() => signals.emit(signal));
          return child;
        },
      }), signal === 'SIGINT' ? 130 : 143);
    } finally { clearTimeout(keepAlive); }
    assert.deepEqual(killed, [signal, 'SIGKILL']);
    assert.deepEqual(await readdir(tempRoot), ['unrelated']);
  }
});

test('cancelling download does not create or execute a file', async t => {
  const tempRoot = await temporary(t);
  const signals = new EventEmitter();
  assert.equal(await runLauncher(manifest(), [], {
    platform: 'linux', arch: 'x64', tempRoot, signals,
    download: async (_url, { signal }) => { signals.emit('SIGINT'); signal.throwIfAborted(); },
    spawnProcess: () => assert.fail('must not spawn'),
  }), 130);
  assert.deepEqual(await readdir(tempRoot), ['unrelated']);
});

test('Windows console interruption allows Go to restore its terminal before forced termination', async t => {
  const tempRoot = await temporary(t);
  for (const cooperative of [true, false]) {
    const signals = new EventEmitter();
    const killed = [];
    const keepAlive = setTimeout(() => {}, 1000);
    try {
      assert.equal(await runLauncher(manifest('win32'), [], {
        platform: 'win32', arch: 'x64', tempRoot, signals, cancellationGraceMs: 5, download: async () => compressed,
        spawnProcess: () => {
          const child = new EventEmitter();
          child.kill = value => { killed.push(value); queueMicrotask(() => child.emit('close', null, value)); };
          queueMicrotask(() => {
            signals.emit('SIGINT');
            assert.deepEqual(killed, []);
            if (cooperative) child.emit('close', 1, null);
          });
          return child;
        },
      }), 130);
    } finally { clearTimeout(keepAlive); }
    assert.deepEqual(killed, cooperative ? [] : ['SIGKILL']);
    assert.equal(signals.listenerCount('SIGINT'), 0);
    assert.deepEqual(await readdir(tempRoot), ['unrelated']);
  }
});
