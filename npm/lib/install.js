'use strict';
// Downloads, verifies and unpacks the native depwhy binary that matches this
// machine and this package's version. Used by the postinstall script and, as a
// fallback, by the launcher (for installs that skipped scripts).

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { target, assetName, releaseUrl, parseChecksums } = require('./platform');

// downloadTimeoutMs bounds each download, so an offline machine fails in a minute
// instead of hanging.
const downloadTimeoutMs = 60_000;

// httpDownload fetches a URL into a Buffer. Only https is accepted, including
// after redirects (GitHub redirects release downloads to a CDN). Node's fetch does
// not read HTTPS_PROXY; the error says so, because that is the usual cause.
async function httpDownload(url) {
  let res;
  try {
    res = await fetch(url, { redirect: 'follow', signal: AbortSignal.timeout(downloadTimeoutMs) });
  } catch (err) {
    const cause = err.cause && err.cause.message ? `: ${err.cause.message}` : '';
    throw new Error(`could not reach ${url} (${err.message}${cause}); this installer does not use proxy settings, so download the build manually if you are behind a proxy`);
  }
  if (!res.ok) throw new Error(`download failed: ${url} (HTTP ${res.status})`);
  if (!res.url.startsWith('https://')) throw new Error(`refusing non-https download: ${res.url}`);
  return Buffer.from(await res.arrayBuffer());
}

// extract unpacks an archive into dir with the system tar. On Windows the tar that
// ships with the OS (bsdtar) is used explicitly, because a GNU tar from Git for
// Windows can neither read .zip files nor cope with drive letters in paths.
function extract(archive, dir) {
  const tar = process.platform === 'win32'
    ? path.join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'tar.exe')
    : 'tar';
  const r = spawnSync(tar, ['-xf', archive, '-C', dir], { encoding: 'utf8' });
  if (r.error || r.status !== 0) {
    throw new Error(`could not unpack ${path.basename(archive)}: ${r.error ? r.error.message : r.stderr}`);
  }
}

// bundledChecksumsPath is the checksums.txt shipped inside the published package
// (the release workflow adds it). Because it travels in the provenance-attested
// tarball, it can vouch for the download even if a release asset is later replaced.
const bundledChecksumsPath = path.join(__dirname, '..', 'checksums.txt');

// ensureBinary makes sure the binary for `version` exists in `dir` and returns its
// path. Nothing is installed unless the archive's SHA-256 matches the expected
// checksum: the one bundled in the package when present, else the release's
// checksums.txt. The install is atomic: everything happens in a private temporary
// directory and the finished binary is renamed into place, so an interrupted run
// cannot leave a truncated binary and two concurrent runs cannot corrupt each
// other. The options besides version are injectable for tests.
async function ensureBinary({
  version,
  platform = process.platform,
  arch = process.arch,
  dir,
  download = httpDownload,
  bundledChecksums = bundledChecksumsPath,
}) {
  const t = target(platform, arch);
  const bin = path.join(dir, t.exe);
  if (fs.existsSync(bin)) return bin;

  const name = assetName(version, t);
  const sumsText = fs.existsSync(bundledChecksums)
    ? fs.readFileSync(bundledChecksums, 'utf8')
    : (await download(releaseUrl(version, 'checksums.txt'))).toString('utf8');
  const want = parseChecksums(sumsText).get(name);
  if (!want) throw new Error(`no checksum listed for ${name} in release v${version}`);

  const archive = await download(releaseUrl(version, name));
  const got = crypto.createHash('sha256').update(archive).digest('hex');
  if (got !== want) throw new Error(`checksum mismatch for ${name}: expected ${want}, got ${got}`);

  let work;
  try {
    fs.mkdirSync(dir, { recursive: true });
    work = fs.mkdtempSync(path.join(dir, '.tmp-'));
  } catch (err) {
    throw new Error(`cannot write to ${dir} (${err.code || err.message}); if depwhy was installed with sudo, reinstall it or run it once as that user`);
  }
  try {
    const file = path.join(work, name);
    fs.writeFileSync(file, archive);
    extract(file, work);
    const unpacked = path.join(work, t.exe);
    if (!fs.existsSync(unpacked)) throw new Error(`${name} did not contain ${t.exe}`);
    fs.chmodSync(unpacked, 0o755);
    try {
      fs.renameSync(unpacked, bin); // atomic; a concurrent installer just wins with the same file
    } catch (err) {
      if (!fs.existsSync(bin)) throw err;
    }
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
  return bin;
}

module.exports = { ensureBinary };
