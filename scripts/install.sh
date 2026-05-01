#!/bin/bash
set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="$HOME/.local/bin"
BIN_NAME="mm"
LAUNCHD_DIR="$HOME/Library/LaunchAgents"
PLIST_NAME="com.mihomo.monitor.plist"

mkdir -p "$INSTALL_DIR"
mkdir -p "$HOME/.config/mihomo"

cd "$PROJECT_DIR"
echo "Building Go binary..."
go build -o "$PROJECT_DIR/bin/$BIN_NAME" ./cmd/mm
chmod +x "$PROJECT_DIR/bin/$BIN_NAME"

ln -sf "$PROJECT_DIR/bin/$BIN_NAME" "$INSTALL_DIR/mm"
echo "Installed: $INSTALL_DIR/mm -> $PROJECT_DIR/bin/$BIN_NAME"

if [[ -d "$LAUNCHD_DIR" ]]; then
  PLIST_CONTENT=$(sed -e "s|{{PROJECT_DIR}}|$PROJECT_DIR|g" -e "s|{{CONFIG_DIR}}|$HOME/.config/mihomo|g" -e "s|{{HOME}}|$HOME|g" "$PROJECT_DIR/launchd/mihomo-monitor.plist")
  echo "$PLIST_CONTENT" > "$LAUNCHD_DIR/$PLIST_NAME"
  launchctl unload "$LAUNCHD_DIR/$PLIST_NAME" 2>/dev/null || true
  launchctl load "$LAUNCHD_DIR/$PLIST_NAME" 2>/dev/null || true
fi

echo "Done. Run: mm"
