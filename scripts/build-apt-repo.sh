#!/usr/bin/env bash
# Builds a signed, flat apt repository (plus a small landing page) from .deb files.
#
# Usage: scripts/build-apt-repo.sh <debs-dir> <site-dir> <gpg-key-id>
#
#   <debs-dir>    directory holding every .deb to publish (all versions, all architectures)
#   <site-dir>    output directory; publish its contents as the web root
#   <gpg-key-id>  signing key, which must already be in the current GnuPG keyring
#                 (set GNUPGHOME to use another keyring)
#
# Output layout:
#   <site-dir>/index.html   install instructions
#   <site-dir>/key.gpg      public key, binary (for apt's signed-by=)
#   <site-dir>/key.asc      public key, ASCII-armored
#   <site-dir>/apt/         *.deb, Packages(.gz), Release, Release.gpg, InRelease
#
# Users add it as a flat repository: deb [signed-by=...] https://<host>/apt ./
# Needs: dpkg-scanpackages (dpkg-dev), apt-ftparchive (apt-utils), gpg, gzip.
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <debs-dir> <site-dir> <gpg-key-id>" >&2
  exit 2
fi
debs=$1
site=$2
key=$3
base_url=${APT_BASE_URL:-https://dpologdvinity.github.io/depwhy}

shopt -s nullglob
packages=("$debs"/*.deb)
if [ "${#packages[@]}" -eq 0 ]; then
  echo "no .deb files in $debs" >&2
  exit 1
fi

rm -rf "$site"
mkdir -p "$site/apt"
cp "${packages[@]}" "$site/apt/"

(
  cd "$site/apt"
  # --multiversion keeps every version so users can pin an older release.
  dpkg-scanpackages --multiversion . >Packages
  gzip -9 -k -f Packages
  archs=$(awk '/^Architecture:/ {print $2}' Packages | sort -u | tr '\n' ' ')
  apt-ftparchive \
    -o "APT::FTPArchive::Release::Origin=depwhy" \
    -o "APT::FTPArchive::Release::Label=depwhy" \
    -o "APT::FTPArchive::Release::Suite=stable" \
    -o "APT::FTPArchive::Release::Codename=stable" \
    -o "APT::FTPArchive::Release::Architectures=${archs% }" \
    release . >Release
  # Release.gpg is the detached signature; InRelease is the same file clear-signed.
  gpg --batch --yes --pinentry-mode loopback --local-user "$key" --armor --detach-sign -o Release.gpg Release
  gpg --batch --yes --pinentry-mode loopback --local-user "$key" --clearsign -o InRelease Release
)

gpg --batch --yes --armor --export "$key" >"$site/key.asc"
gpg --batch --yes --export "$key" >"$site/key.gpg"

cat >"$site/index.html" <<HTML
<!doctype html>
<meta charset="utf-8">
<title>depwhy apt repository</title>
<h1>depwhy apt repository</h1>
<p>Explains why a package is in your dependency tree.
<a href="https://github.com/dpologdvinity/depwhy">Project page</a></p>
<h2>Install (Debian, Ubuntu and derivatives; amd64 and arm64)</h2>
<pre>
sudo install -d -m 0755 /usr/share/keyrings
curl -fsSL ${base_url}/key.gpg | sudo tee /usr/share/keyrings/depwhy.gpg &gt;/dev/null
echo "deb [signed-by=/usr/share/keyrings/depwhy.gpg] ${base_url}/apt ./" | sudo tee /etc/apt/sources.list.d/depwhy.list
sudo apt update
sudo apt install depwhy
</pre>
HTML

echo "built apt repository in $site with ${#packages[@]} package file(s)"
