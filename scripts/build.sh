#!/usr/bin/env bash
# build.sh — cross-compile ops-panel + build frontend, package tarballs ready for GitHub release.
#
# Outputs:
#   dist/ops-panel-<version>-linux-amd64.tar.gz
#   dist/ops-panel-<version>-linux-arm64.tar.gz
#   dist/SHA256SUMS
#
# Each tarball contains:
#   ops-panel           — static Go binary (CGO_ENABLED=0, stripped)
#   frontend/           — vite build output (static SPA)
#   scripts/install.sh
#   scripts/ops-panel.service
#   VERSION
#
# Usage:
#   ./scripts/build.sh                 # version auto from git tag, else dev-<shorthash>
#   VERSION=v0.2.0 ./scripts/build.sh  # override

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  if git rev-parse --git-dir >/dev/null 2>&1; then
    VERSION=$(git describe --tags --exact-match 2>/dev/null \
      || echo "dev-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)")
  else
    VERSION="dev-local"
  fi
fi

echo "==> Version: $VERSION"

rm -rf dist
mkdir -p dist

echo "==> Building frontend..."
pushd frontend >/dev/null
  if [ ! -d node_modules ]; then
    pnpm install --frozen-lockfile
  fi
  pnpm build
popd >/dev/null

for ARCH in amd64 arm64; do
  STAGE="dist/stage-linux-$ARCH"
  mkdir -p "$STAGE/scripts"

  echo "==> Building backend (linux/$ARCH)..."
  pushd backend >/dev/null
    CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH \
      go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
      -o "$ROOT/$STAGE/ops-panel" ./cmd/panel
  popd >/dev/null

  cp -r frontend/dist "$STAGE/frontend"
  cp scripts/install.sh "$STAGE/scripts/install.sh"
  cp scripts/opsctl "$STAGE/scripts/opsctl"
  cp scripts/ops-panel.service "$STAGE/scripts/ops-panel.service"
  cp scripts/generate-cert.sh "$STAGE/scripts/generate-cert.sh" 2>/dev/null || true
  cp scripts/ssh-harden.sh "$STAGE/scripts/ssh-harden.sh" 2>/dev/null || true
  echo "$VERSION" > "$STAGE/VERSION"
  cp README.md "$STAGE/README.md"

  chmod +x "$STAGE/ops-panel" "$STAGE/scripts/opsctl" "$STAGE/scripts/"*.sh

  TARBALL="dist/ops-panel-$VERSION-linux-$ARCH.tar.gz"
  tar -czf "$TARBALL" -C "dist" "stage-linux-$ARCH" \
    --transform "s|stage-linux-$ARCH|ops-panel-$VERSION-linux-$ARCH|"

  # When building on Windows (MinGW/git-bash), NTFS doesn't preserve the
  # Unix execute bit on files without a .sh/.exe extension, so `chmod +x`
  # on the Go binary doesn't make it into the tar. Post-process the tar
  # with Python to force 0755 on executables.
  python -c "
import tarfile, os, sys
src = sys.argv[1]
tmp = src + '.tmp'
execs = ('/ops-panel',)
execs_endswith = ('.sh', '/opsctl')
with tarfile.open(src, 'r:gz') as tin, tarfile.open(tmp, 'w:gz') as tout:
    for m in tin.getmembers():
        name = m.name
        if name.endswith(execs) or any(name.endswith(s) for s in execs_endswith):
            m.mode = 0o755
        elif m.isdir():
            m.mode = 0o755
        else:
            m.mode = 0o644
        f = tin.extractfile(m) if m.isfile() else None
        tout.addfile(m, f)
os.replace(tmp, src)
" "$TARBALL" 2>/dev/null || {
    echo "    !! python post-process failed; binary may lack +x on extract. install.sh handles this defensively." >&2
  }

  rm -rf "$STAGE"
  echo "    -> $TARBALL"
done

echo "==> SHA256SUMS"
pushd dist >/dev/null
  sha256sum *.tar.gz > SHA256SUMS
  cat SHA256SUMS
popd >/dev/null

echo ""
echo "Done. Upload dist/*.tar.gz and dist/SHA256SUMS to GitHub release $VERSION."
