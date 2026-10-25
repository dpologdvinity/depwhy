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
