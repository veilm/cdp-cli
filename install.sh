#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")" && pwd)
BIN_PATH="$ROOT/cdp"
INSTALL_PATH="/usr/local/bin/cdp"

printf 'Building cdp CLI...\n'
GO111MODULE=on go build -buildvcs=false -o "$BIN_PATH" "$ROOT"

printf 'Copying binary to %s...\n' "$INSTALL_PATH"
tmp_install="${INSTALL_PATH}.new.$$"
sudo install -m 0755 "$BIN_PATH" "$tmp_install"
sudo mv -f "$tmp_install" "$INSTALL_PATH"

printf 'Installed cdp to %s\n' "$INSTALL_PATH"
