#!/usr/bin/env bash
# Build a Windows x64 portable zip of class-code-lab.
# Output: dist/class-code-lab-portable-windows-x64.zip
#
# Run on macOS (or Linux). Requires: Node.js for the frontend build,
# Go toolchain for cross-compilation. No CGO, no mingw needed.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

FRONTEND_DIR="$ROOT/frontend"
BACKEND_DIR="$ROOT/backend"
CMD_DIR="$BACKEND_DIR/cmd/class-code-lab"
EMBED_DIR="$CMD_DIR/frontend-dist"
DIST_DIR="$ROOT/dist"
PKG_DIR="$DIST_DIR/class-code-lab"
ZIP_PATH="$DIST_DIR/class-code-lab-portable-windows-x64.zip"

echo "==> [1/6] Building frontend (Vite)"
cd "$FRONTEND_DIR"
if [ ! -d node_modules ]; then
    npm ci
fi
npm run build

echo "==> [2/6] Staging frontend-dist for go:embed"
rm -rf "$EMBED_DIR"
mkdir -p "$EMBED_DIR"
cp -R "$FRONTEND_DIR/dist/." "$EMBED_DIR/"
# Keep a .gitkeep so the dir survives cleanup in gitignored state
touch "$EMBED_DIR/.gitkeep"

echo "==> [3/6] Tidying Go modules"
cd "$BACKEND_DIR"
# tidy is best-effort: pulling test-only deps (e.g. pprof) can fail on
# restricted networks but does not block the production build, since the
# main module graph is already complete from `go get`.
if ! go mod tidy 2>&1; then
    echo "  (go mod tidy had issues, retrying with goproxy.cn fallback)"
    GOPROXY="https://goproxy.cn,direct" go mod tidy 2>&1 || \
        echo "  (tidy still failing — proceeding, build does not require it)"
fi

echo "==> [4/6] Cross-compiling Windows x64 exe (CGO disabled)"
mkdir -p "$PKG_DIR"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath \
    -o "$PKG_DIR/class-code-lab.exe" \
    ./cmd/class-code-lab

echo "==> [5/6] Writing launcher and docs"
cat > "$PKG_DIR/start.bat" <<'BAT'
@echo off
chcp 65001 >nul
cd /d "%~dp0"
set PORT=%1
if "%PORT%"=="" set PORT=8080
set /a RUNNER_PORT=%PORT%+1
set APP_ADDRESS=0.0.0.0:%PORT%
set RUNNER_ADDRESS=0.0.0.0:%RUNNER_PORT%
echo starting class-code-lab on port %PORT% (runner %RUNNER_PORT%)...
echo teacher UI:  http://127.0.0.1:%PORT%
echo students use: http://<this-pc-ip>:%PORT%
echo close this window to stop the service.
start "" "class-code-lab.exe"
timeout /t 2 >nul
start "" "http://127.0.0.1:%PORT%"
BAT

cat > "$PKG_DIR/.env.example" <<'ENV'
# class-code-lab portable configuration
# Copy this file to ".env" and edit as needed. The start.bat launcher
# already handles port selection via its first argument.
# Configure AI services after logging in to the teacher dashboard.

# --- Override ports here instead of using start.bat argument ---
# APP_ADDRESS=:8080
# RUNNER_ADDRESS=:8081

# --- Admin account (only seeds on first run) ---
# ADMIN_NAME=任课教师
# ADMIN_LOGIN=teacher
# ADMIN_PASSWORD=123456
ENV

cat > "$PKG_DIR/README.txt" <<'README'
class-code-lab 便携版 (Windows x64)
====================================

【启动方式】
  双击 start.bat
  浏览器会自动打开 http://127.0.0.1:8080

【改端口】(默认 8080 被占用时)
  在命令行执行:start.bat 8090
  或编辑 .env.example 复制为 .env 后修改 APP_ADDRESS

【教师访问】
  本机浏览器:  http://127.0.0.1:8080
  默认账号:    teacher / 123456 (首次登录强制改密)

【学生访问】
  学生在自己设备浏览器输入:http://<教师机IP>:8080
  教师可在命令行执行 ipconfig 查看本机 IP
  然后在教师端后台创建班级并添加学生账号

【数据存储】
  数据库文件 class-code-lab.db 与 exe 同目录
  备份/迁移:复制整个文件夹即可
  关闭服务:直接关闭黑色控制台窗口

【AI 功能】(可选)
  教师登录后进入“AI 对话与用量”,新增并测试 AI 服务
  可配置多组服务,系统按空闲容量分配学生请求
  不配置时 AI 入口自动禁用,不影响其他功能

【系统要求】
  Windows 10/11 64 位
  现代浏览器(Chrome/Edge/Firefox)
README

echo "==> [6/6] Packaging zip"
cd "$DIST_DIR"
rm -f "$ZIP_PATH"
zip -r -X "$ZIP_PATH" class-code-lab/ -x "*.DS_Store" -x "*__MACOSX*"

echo ""
echo "Done."
echo "  Package: $ZIP_PATH"
echo "  Size:    $(du -h "$ZIP_PATH" | cut -f1)"

# Cleanup staged frontend-dist so it does not pollute the source tree.
# The .gitkeep remains so go:embed always has a valid target.
cd "$EMBED_DIR"
find . ! -name '.gitkeep' -delete
echo "  (cleaned staged frontend-dist)"
