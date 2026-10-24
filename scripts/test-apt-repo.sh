#!/usr/bin/env bash
# Proves an apt repository built by build-apt-repo.sh really installs, in a clean
# Debian container: apt must accept the signature, install depwhy, and run it.
#
# Usage: scripts/test-apt-repo.sh <site-dir> [image]
#
# Only the container's own architecture is tested. Needs docker.
set -euo pipefail

if [ "$#" -lt 1 ]; then
  echo "usage: $0 <site-dir> [image]" >&2
  exit 2
fi
site=$(cd "$1" && pwd)
image=${2:-debian:stable-slim}

docker run --rm -v "$site:/site:ro" "$image" bash -euxc '
  # The repository is read from disk, but verified exactly as a remote one is.
  install -d -m 0755 /usr/share/keyrings
  cp /site/key.gpg /usr/share/keyrings/depwhy.gpg
  echo "deb [signed-by=/usr/share/keyrings/depwhy.gpg] file:///site/apt ./" >/etc/apt/sources.list.d/depwhy.list
  apt-get update
  apt-get install -y depwhy
  depwhy --version
  command -v depwhy

  # A tampered index must be rejected. apt prefers the compressed index, so
  # corrupt every variant of it and refresh.
  cp -r /site /tmp/tampered
  printf "x" >>/tmp/tampered/apt/Packages
  printf "x" >>/tmp/tampered/apt/Packages.gz
  echo "deb [signed-by=/usr/share/keyrings/depwhy.gpg] file:///tmp/tampered/apt ./" >/etc/apt/sources.list.d/depwhy.list
  if apt-get update 2>&1 | tee /tmp/update.log | grep -qiE "hash sum mismatch|is not signed|invalid"; then
    echo "tampered repository correctly rejected"
  else
    cat /tmp/update.log; echo "tampered repository was NOT rejected" >&2; exit 1
  fi
'
