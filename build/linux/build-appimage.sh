#!/usr/bin/env bash
# AppImage 组装（10 文档 §4 定案：linuxdeploy；上游 build/linux/build-appimage.sh 同构复用，
# 构建源从 wails build 换为 go build 单二进制）。
# 用法：go build 后执行
#   ./build/linux/build-appimage.sh <版本号>          # 如 1.0.0
# 产物：build/bin/chinese-chess-ultra-gio-<版本号>-linux-amd64.AppImage
# 依赖：linuxdeploy（脚本自动下载到 tmp/；无 FUSE 时自动走 --appimage-extract-and-run）。
set -euo pipefail
cd "$(dirname "$0")/../.."

VER="${1:?用法: build-appimage.sh <版本号>}"
BIN="build/bin/chinese-chess-ultra-gio"
OUT="build/bin/chinese-chess-ultra-gio-${VER}-linux-amd64.AppImage"
TOOLS_DIR="tmp/appimage-tools"
[ -x "$BIN" ] || { echo "未找到 $BIN（先执行 go build -o build/bin/chinese-chess-ultra-gio .）" >&2; exit 1; }

mkdir -p "$TOOLS_DIR" build/bin/AppDir/usr/bin
download() {
  local url="$1" dest="$2"
  if [ ! -x "$dest" ]; then
    echo "下载 $url"
    curl -fsSL -o "$dest" "$url"
    chmod +x "$dest"
  fi
}
LINUXDEPLOY="$TOOLS_DIR/linuxdeploy-x86_64.AppImage"
download "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage" "$LINUXDEPLOY"

export APPIMAGE_EXTRACT_AND_RUN=1   # linuxdeploy 无 FUSE 时的官方开关
export ARCH=x86_64
export UPDATE_DESKTOP_DATABASE=""

APPDIR="build/bin/AppDir"
cp "$BIN" "$APPDIR/usr/bin/chinese-chess-ultra-gio"
cp build/linux/chinese-chess-ultra-gio.desktop "$APPDIR/"
# linuxdeploy 仅接受标准分辨率图标（appicon.png 为 1024）：预置 512 副本随仓库走。
cp build/linux/chinese-chess-ultra-gio-512.png "$APPDIR/chinese-chess-ultra-gio.png"

"$LINUXDEPLOY" --appdir "$APPDIR" \
  --desktop-file "$APPDIR/chinese-chess-ultra-gio.desktop" \
  --icon-file "$APPDIR/chinese-chess-ultra-gio.png" \
  --output appimage

# linuxdeploy 产物写在 CWD（名字取自 desktop Name 字段），统一改名为发布名。
built=$(ls -t ./*.AppImage build/bin/*.AppImage 2>/dev/null | head -1 || true)
if [ -n "$built" ] && [ "$built" != "$OUT" ]; then
  mv "$built" "$OUT"
fi
[ -f "$OUT" ] || { echo "AppImage 产物未生成" >&2; exit 1; }
echo "AppImage 已生成：$OUT"
