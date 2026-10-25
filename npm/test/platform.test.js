'use strict';
// Tests for the pure helpers in lib/platform.js. Run with: npm test

const test = require('node:test');
const assert = require('node:assert/strict');
const { target, assetName, releaseUrl, parseChecksums } = require('../lib/platform');

test('target maps Node platform and arch names to release names', () => {
  assert.deepEqual(target('linux', 'x64'), { os: 'linux', arch: 'amd64', ext: 'tar.gz', exe: 'depwhy' });
  assert.deepEqual(target('darwin', 'arm64'), { os: 'darwin', arch: 'arm64', ext: 'tar.gz', exe: 'depwhy' });
  assert.deepEqual(target('win32', 'x64'), { os: 'windows', arch: 'amd64', ext: 'zip', exe: 'depwhy.exe' });
});

test('target rejects platforms without a release build', () => {
  assert.throws(() => target('freebsd', 'x64'), /unsupported platform freebsd\/x64/);
  assert.throws(() => target('linux', 'ia32'), /unsupported platform linux\/ia32/);
});

test('assetName and releaseUrl match the GoReleaser file names', () => {
  const t = target('linux', 'arm64');
  assert.equal(assetName('0.2.1', t), 'depwhy_0.2.1_linux_arm64.tar.gz');
  assert.equal(
    releaseUrl('0.2.1', 'checksums.txt'),
    'https://github.com/dpologdvinity/depwhy/releases/download/v0.2.1/checksums.txt',
  );
});

test('parseChecksums reads "<sha256>  <file>" lines and ignores blanks', () => {
  const sums = parseChecksums('abc123  depwhy_0.2.1_linux_amd64.tar.gz\n\ndef456  checksums-other.txt\n');
  assert.equal(sums.get('depwhy_0.2.1_linux_amd64.tar.gz'), 'abc123');
  assert.equal(sums.get('checksums-other.txt'), 'def456');
  assert.equal(sums.size, 2);
});
