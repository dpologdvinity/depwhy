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

  # Tampering must be rejected, each time for the right reason. apt prefers the
  # compressed index, so corrupt every variant of it.
  expect_rejected() {
    local what=$1 pattern=$2
    if apt-get update >/tmp/update.log 2>&1; then
      cat /tmp/update.log; echo "$what was NOT rejected" >&2; exit 1
    fi
    if ! grep -qiE "$pattern" /tmp/update.log; then
      cat /tmp/update.log; echo "$what failed, but not for the expected reason ($pattern)" >&2; exit 1
    fi
    echo "$what correctly rejected"
  }
  use_copy() {
    rm -rf /tmp/t && cp -r /site /tmp/t
    echo "deb [signed-by=/usr/share/keyrings/depwhy.gpg] file:///tmp/t/apt ./" >/etc/apt/sources.list.d/depwhy.list
    rm -rf /var/lib/apt/lists/*
  }

  use_copy; printf x >>/tmp/t/apt/Packages; printf x >>/tmp/t/apt/Packages.gz
  expect_rejected "a modified Packages index" "hash sum mismatch|size mismatch|file size is not what was expected"

  use_copy; sed -i "s/^Origin: .*/Origin: evil/" /tmp/t/apt/InRelease
  expect_rejected "a modified InRelease" "message has been manipulated|is not signed|invalid signature|bad signature"

  use_copy; rm /tmp/t/apt/InRelease /tmp/t/apt/Release.gpg
  expect_rejected "an unsigned repository" "is not signed|no_pubkey|not signed"
'
