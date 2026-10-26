'use strict';
// Tests for lib/install.js using a real tar.gz built on the fly and an injected
// downloader, so nothing touches the network. Run with: npm test

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { ensureBinary } = require('../lib/install');
const { assetName, target } = require('../lib/platform');

const VERSION = '9.9.9';

// makeArchive builds a tar.gz holding a fake "depwhy" executable.
function makeArchive() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'depwhy-archive-'));
  fs.writeFileSync(path.join(dir, 'depwhy'), '#!/bin/sh\necho fake\n', { mode: 0o755 });
  const archive = path.join(dir, 'a.tar.gz');
  const r = spawnSync('tar', ['-czf', archive, '-C', dir, 'depwhy']);
  assert.equal(r.status, 0, String(r.stderr));
  return fs.readFileSync(archive);
}

// fakeDownload serves a checksums file and the archive; `sum` can be forged.
function fakeDownload(archive, sum) {
  const t = target('linux', 'x64');
  const name = assetName(VERSION, t);
  const calls = [];
  const fn = async (url) => {
    calls.push(url);
    if (url.endsWith('/checksums.txt')) return Buffer.from(`${sum}  ${name}\n`);
    if (url.endsWith(`/${name}`)) return archive;
    throw new Error(`unexpected URL ${url}`);
  };
  fn.calls = calls;
  return fn;
}

// assertRunnable checks the unpacked fake binary is complete. On Windows a #!/bin/sh
// script cannot be executed, so there the contents are compared instead.
function assertRunnable(bin) {
  if (process.platform === 'win32') {
    assert.match(fs.readFileSync(bin, 'utf8'), /echo fake/);
  } else {
    assert.equal(spawnSync(bin).stdout.toString().trim(), 'fake', 'the binary must be complete and runnable');
  }
}

const sha = (b) => crypto.createHash('sha256').update(b).digest('hex');
const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), 'depwhy-vendor-'));

test('ensureBinary downloads, verifies and unpacks the binary', async () => {
  const archive = makeArchive();
  const dir = tmp();
  const download = fakeDownload(archive, sha(archive));
  const bin = await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download });
  assert.equal(bin, path.join(dir, 'depwhy'));
  assert.ok(fs.existsSync(bin));
  if (process.platform !== 'win32') {
    // Windows has no execute bit; there the .exe extension is what matters.
    assert.ok(fs.statSync(bin).mode & 0o111, 'binary must be executable');
  }
  assert.equal(fs.readdirSync(dir).length, 1, 'the archive must be cleaned up');
});

test('ensureBinary does nothing when the binary is already there', async () => {
  const archive = makeArchive();
  const dir = tmp();
  await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download: fakeDownload(archive, sha(archive)) });
  const again = fakeDownload(archive, sha(archive));
  await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download: again });
  assert.equal(again.calls.length, 0);
});

test('ensureBinary rejects an archive whose checksum does not match', async () => {
  const archive = makeArchive();
  const dir = tmp();
  const download = fakeDownload(archive, 'f'.repeat(64));
  await assert.rejects(
    ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download }),
    /checksum mismatch/,
  );
  assert.equal(fs.existsSync(path.join(dir, 'depwhy')), false, 'nothing may be installed');
});

test('ensureBinary rejects a release that lists no checksum for the asset', async () => {
  const dir = tmp();
  const download = async (url) => (url.endsWith('checksums.txt') ? Buffer.from('') : Buffer.from('x'));
  await assert.rejects(
    ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download }),
    /no checksum listed/,
  );
});

test('ensureBinary survives two installs at once and leaves no temp files', async () => {
  const archive = makeArchive();
  const dir = tmp();
  const opts = () => ({ version: VERSION, platform: 'linux', arch: 'x64', dir, download: fakeDownload(archive, sha(archive)) });
  const [a, b] = await Promise.all([ensureBinary(opts()), ensureBinary(opts())]);
  assert.equal(a, b);
  assert.deepEqual(fs.readdirSync(dir), ['depwhy'], 'only the binary may remain');
  assertRunnable(a);
});

test('ensureBinary ignores a stale temp directory from an interrupted install', async () => {
  const archive = makeArchive();
  const dir = tmp();
  fs.mkdirSync(path.join(dir, '.tmp-stale'));
  fs.writeFileSync(path.join(dir, '.tmp-stale', 'depwhy'), 'truncated');
  const bin = await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download: fakeDownload(archive, sha(archive)) });
  assertRunnable(bin);
});

test('ensureBinary handles rename collision if binary already exists', async () => {
  const archive = makeArchive();
  const dir = tmp();
  const download = fakeDownload(archive, sha(archive));
  const origRename = fs.renameSync;
  let renameCalls = 0;
  fs.renameSync = (from, to) => {
    renameCalls++;
    origRename(from, to);
    const err = new Error('EPERM: operation not permitted');
    err.code = 'EPERM';
    throw err;
  };
  try {
    const bin = await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download });
    assert.ok(fs.existsSync(bin));
    assert.equal(renameCalls, 1);
  } finally {
    fs.renameSync = origRename;
  }
});

test('ensureBinary trusts the checksums bundled in the package over a download', async () => {
  const archive = makeArchive();
  const dir = tmp();
  const name = assetName(VERSION, target('linux', 'x64'));
  const bundled = path.join(dir, '..', `bundled-${path.basename(dir)}.txt`);
  fs.writeFileSync(bundled, `${sha(archive)}  ${name}\n`);
  // The download offers a forged checksum; the bundled file must win.
  const download = fakeDownload(archive, 'f'.repeat(64));
  const bin = await ensureBinary({ version: VERSION, platform: 'linux', arch: 'x64', dir, download, bundledChecksums: bundled });
  assert.ok(fs.existsSync(bin));
  assert.ok(!download.calls.some((u) => u.endsWith('checksums.txt')), 'checksums.txt must not be downloaded');
});

test('ensureBinary unpacks a Windows .zip', { skip: process.platform !== 'win32' }, async () => {
  const src = fs.mkdtempSync(path.join(os.tmpdir(), 'depwhy-zip-'));
  fs.writeFileSync(path.join(src, 'depwhy.exe'), 'MZ fake');
  const zip = path.join(src, 'a.zip');
  const r = spawnSync(path.join(process.env.SystemRoot, 'System32', 'tar.exe'), ['-a', '-cf', zip, '-C', src, 'depwhy.exe']);
  assert.equal(r.status, 0, String(r.stderr));
  const archive = fs.readFileSync(zip);
  const name = assetName(VERSION, target('win32', 'x64'));
  const download = async (url) => (url.endsWith('checksums.txt') ? Buffer.from(`${sha(archive)}  ${name}\n`) : archive);
  const dir = tmp();
  const bin = await ensureBinary({ version: VERSION, platform: 'win32', arch: 'x64', dir, download });
  assert.equal(path.basename(bin), 'depwhy.exe');
  assert.ok(fs.existsSync(bin));
});
