'use strict';
// Downloads, verifies and unpacks the native depwhy binary that matches this
// machine and this package's version. Used by the postinstall script and, as a
// fallback, by the launcher (for installs that skipped scripts).

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { target, assetName, releaseUrl, parseChecksums } = require('./platform');

// httpDownload fetches a URL into a Buffer. Only https is accepted, including
// after redirects (GitHub redirects release downloads to a CDN).
async function httpDownload(url) {
  const res = await fetch(url, { redirect: 'follow' });
  if (!res.ok) throw new Error(`download failed: ${url} (HTTP ${res.status})`);
  if (!res.url.startsWith('https://')) throw new Error(`refusing non-https download: ${res.url}`);
  return Buffer.from(await res.arrayBuffer());
}

// extract unpacks an archive into dir with the system tar, which handles .tar.gz
// everywhere and .zip on Windows 10 and later (bsdtar).
function extract(archive, dir) {
  const r = spawnSync('tar', ['-xf', archive, '-C', dir], { encoding: 'utf8' });
  if (r.error || r.status !== 0) {
    throw new Error(`could not unpack ${path.basename(archive)}: ${r.error ? r.error.message : r.stderr}`);
  }
}

// ensureBinary makes sure the binary for `version` exists in `dir` and returns its
// path. Nothing is installed unless the archive's SHA-256 matches the release's
// checksums.txt. The options besides version are injectable for tests.
async function ensureBinary({
  version,
  platform = process.platform,
  arch = process.arch,
  dir,
  download = httpDownload,
}) {
  const t = target(platform, arch);
  const bin = path.join(dir, t.exe);
  if (fs.existsSync(bin)) return bin;

  const name = assetName(version, t);
  const sums = parseChecksums((await download(releaseUrl(version, 'checksums.txt'))).toString('utf8'));
  const want = sums.get(name);
  if (!want) throw new Error(`no checksum listed for ${name} in release v${version}`);

  const archive = await download(releaseUrl(version, name));
  const got = crypto.createHash('sha256').update(archive).digest('hex');
  if (got !== want) throw new Error(`checksum mismatch for ${name}: expected ${want}, got ${got}`);

  fs.mkdirSync(dir, { recursive: true });
  const file = path.join(dir, name);
  fs.writeFileSync(file, archive);
  try {
    extract(file, dir);
  } finally {
    fs.rmSync(file, { force: true });
  }
  if (!fs.existsSync(bin)) throw new Error(`${name} did not contain ${t.exe}`);
  fs.chmodSync(bin, 0o755);
  return bin;
}

module.exports = { ensureBinary };
