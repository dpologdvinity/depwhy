'use strict';
// Pure helpers that map the running machine to a depwhy release asset. They have
// no side effects, so they are easy to test.

const REPO = 'dpologdvinity/depwhy';
const OS = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
const ARCH = { x64: 'amd64', arm64: 'arm64' };

// target translates Node's process.platform / process.arch into the names used by
// the release files. It throws for combinations that have no release build.
function target(platform, arch) {
  const os = OS[platform];
  const cpu = ARCH[arch];
  if (!os || !cpu) {
    throw new Error(`unsupported platform ${platform}/${arch}; download a build from https://github.com/${REPO}/releases`);
  }
  const windows = os === 'windows';
  return { os, arch: cpu, ext: windows ? 'zip' : 'tar.gz', exe: windows ? 'depwhy.exe' : 'depwhy' };
}

// assetName is the archive file name GoReleaser gives a release build.
function assetName(version, t) {
  return `depwhy_${version}_${t.os}_${t.arch}.${t.ext}`;
}

// releaseUrl is the download URL of a file attached to the release for `version`.
function releaseUrl(version, file) {
  return `https://github.com/${REPO}/releases/download/v${version}/${file}`;
}

// parseChecksums reads GoReleaser's checksums.txt ("<sha256>  <file>" per line)
// into a Map from file name to lowercase hex digest.
function parseChecksums(text) {
  const sums = new Map();
  for (const line of text.split('\n')) {
    const m = /^([0-9a-fA-F]+)\s+(\S+)\s*$/.exec(line);
    if (m) sums.set(m[2], m[1].toLowerCase());
  }
  return sums;
}

module.exports = { REPO, target, assetName, releaseUrl, parseChecksums };
