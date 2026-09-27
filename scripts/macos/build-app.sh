#!/usr/bin/env bash
# 构建 PrismCat.app（菜单栏应用，Intel + Apple 芯片通用二进制）并打包成 zip。
#
#   scripts/macos/build-app.sh                  # 版本号取自 git tag，产物在 dist/macos/
#   VERSION=1.16.0 scripts/macos/build-app.sh
#   SKIP_WEB=1 scripts/macos/build-app.sh       # 复用已同步到 internal/server/ui 的前端
#
# 签名：默认 ad-hoc 签名（codesign -s -），用户首次打开需右键 →「打开」。
# 设置 CODESIGN_IDENTITY="Developer ID Application: ..." 则用正式证书并开启
# hardened runtime，之后还需 notarytool 公证（尚未接入）。
#
# 只能在 macOS 上运行：菜单栏依赖 Cocoa，需要 CGO。
set -euo pipefail

cd "$(dirname "$0")/../.."

if [[ "$(uname -s)" != "Darwin" ]]; then
	echo "build-app.sh 只能在 macOS 上运行" >&2
	exit 1
fi

VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
CODESIGN_IDENTITY="${CODESIGN_IDENTITY:--}"
OUT_DIR="dist/macos"
APP="$OUT_DIR/PrismCat.app"
ZIP="$OUT_DIR/prismcat-v$VERSION-macos-universal.zip"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> PrismCat $VERSION"

if [[ -z "${SKIP_WEB:-}" ]]; then
	echo "==> 构建前端"
	(cd web && npm ci && npm run build)
	git clean -fdXq -- internal/server/ui
	cp -R web/dist/. internal/server/ui/
fi
if [[ ! -f internal/server/ui/index.html ]]; then
	echo "internal/server/ui 里没有前端文件，去掉 SKIP_WEB 重新构建" >&2
	exit 1
fi

# 最低 macOS 版本，同时传给 clang 编译 CGO 部分；须与 Info.plist 的
# LSMinimumSystemVersion 一致。新版 Go（1.27 起）编出的代码要求 macOS 13。
export MACOSX_DEPLOYMENT_TARGET=13.0
for arch in arm64 amd64; do
	echo "==> 编译 darwin/$arch"
	CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" go build -trimpath \
		-ldflags="-s -w -X github.com/paopaoandlingyia/PrismCat/internal/config.Version=$VERSION" \
		-o "$WORK/prismcat-$arch" ./cmd/prismcat
done

echo "==> 组装 $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/prismcat" "$WORK/prismcat-arm64" "$WORK/prismcat-amd64"
sed "s/__VERSION__/$VERSION/g" scripts/macos/Info.plist >"$APP/Contents/Info.plist"

ICONSET="$WORK/AppIcon.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" scripts/macos/AppIcon.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
	sips -z $((size * 2)) $((size * 2)) scripts/macos/AppIcon.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns -o "$APP/Contents/Resources/AppIcon.icns" "$ICONSET"

echo "==> 签名 ($CODESIGN_IDENTITY)"
if [[ "$CODESIGN_IDENTITY" == "-" ]]; then
	codesign --force --sign - "$APP"
else
	codesign --force --options runtime --timestamp --sign "$CODESIGN_IDENTITY" "$APP"
fi
codesign --verify --strict "$APP"

echo "==> 打包 $ZIP"
rm -f "$ZIP"
# 用 ditto 而不是 zip，保留签名所需的扩展属性。
ditto -c -k --keepParent "$APP" "$ZIP"

echo "完成: $ZIP"
