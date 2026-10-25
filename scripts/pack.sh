#!/bin/sh
# Builds the release archives of the plugin, one per architecture: dist/<id>-linux-<arch>.tar.gz
# holds plugin.json, web/index.js (the page module, built by the SDK's preset), i18n/ and the
# backend (bin/<name> of plugin.json, a static Go program). The version of the tag (v1.2.3) must
# be the one of plugin.json: the IDE checks it too.
set -eu
cd "$(dirname "$0")/.."
field() { sed -n "s/^  \"$1\": \"\(.*\)\",\$/\1/p" plugin.json; }
id=$(field id)
version=$(field version)
bin=$(field backend)
if [ -n "${TAG:-}" ] && [ "$TAG" != "v$version" ]; then
  echo "the tag $TAG is not the version $version of plugin.json" >&2
  exit 1
fi
rm -rf dist
npm run build
for arch in amd64 arm64; do
  dir=dist/linux-$arch
  mkdir -p "$dir/web"
  if [ -n "$bin" ]; then
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$dir/$bin" ./backend
  fi
  cp -r plugin.json i18n "$dir/"
  cp web/index.js "$dir/web/"
  tar -czf "dist/$id-linux-$arch.tar.gz" -C "$dir" .
done
ls -l dist/*.tar.gz
