#!/usr/bin/env bash
# Build a Windows x64 portable zip of teacher-proxy (教师机反向代理, 双端口版).
# Output: dist/teacher-proxy-portable-windows-x64.zip
#
# Run on macOS (or Linux). Requires only the Go toolchain, no CGO/mingw.
# 仅使用 Go 标准库,无需下载任何外部依赖。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="$ROOT/teacher-proxy"
DIST_DIR="$ROOT/dist/teacher-proxy"
ZIP_PATH="$ROOT/dist/teacher-proxy-portable-windows-x64.zip"

echo "==> [1/3] Tidying Go modules"
cd "$SRC_DIR"
if ! go mod tidy 2>&1; then
    echo "  (go mod tidy had issues, retrying with goproxy.cn fallback)"
    GOPROXY="https://goproxy.cn,direct" go mod tidy 2>&1 || \
        echo "  (tidy still failing — proceeding, build does not require it)"
fi

echo "==> [2/3] Cross-compiling Windows x64 exe (CGO disabled)"
mkdir -p "$DIST_DIR"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath \
    -o "$DIST_DIR/teacher-proxy.exe" \
    .

echo "==> [3/3] Writing firewall helper and docs"
cp "$ROOT/LICENSE" "$DIST_DIR/LICENSE"
# firewall-open.bat: 交互式输入主端口号, 一次性放行 N 和 N+1 两个端口
# (exe 自身无法以管理员权限修改防火墙, 故单独提供)
cat > "$DIST_DIR/firewall-open.bat" <<'BAT'
@echo off
chcp 65001 >nul
echo ============================================================
echo  教师机反向代理 - 防火墙放行工具 (双端口)
echo ============================================================
echo.
echo  必须以管理员身份运行此脚本!
echo  (右键此文件 -^> 以管理员身份运行)
echo.
echo  程序同时监听两个端口: 主服务端口 + runner 端口
echo  默认主服务 8088, runner 自动使用 8089
echo  本工具会一次性放行这两个端口
echo.
set /p PORT=请输入主端口号(回车默认 8088):
if "%PORT%"=="" set PORT=8088
set /a RUNNER_PORT=PORT+1
echo.
echo 正在放行 主端口 %PORT% 和 runner 端口 %RUNNER_PORT% 入站...
netsh advfirewall firewall add rule name="teacher-proxy-main-%PORT%" dir=in action=allow protocol=TCP localport=%PORT%
netsh advfirewall firewall add rule name="teacher-proxy-runner-%RUNNER_PORT%" dir=in action=allow protocol=TCP localport=%RUNNER_PORT%
echo.
echo 完成! 端口 %PORT% 和 %RUNNER_PORT% 已允许入站访问。
echo.
pause
BAT

cat > "$DIST_DIR/README.txt" <<'README'
教师机反向代理 (teacher-proxy)  Windows x64 便携版  双端口版
================================================================

【用途】
  专门为 class-code-lab (主服务 + runner 双端口) 设计。
  机房学生机被限制访问机房外的网络,
  但教师机既能访问机房局域网, 又能访问学校内网。
  本程序在教师机上跑一个反向代理,
  学生通过教师机 IP 即可访问学校内网的 class-code-lab 服务,
  包括主服务和学生代码 runner。

【网络拓扑示意】
  学生机(192.0.2.x) -- 教师机(192.0.2.x) -- 学校内网(198.51.100.x)
                          ↑ 跑 teacher-proxy.exe
                          ↓ 转发到 http://127.0.0.1:18080 (主服务)
                                       http://127.0.0.1:18081 (runner)

【为什么需要双端口】
  class-code-lab 的前端按主页面端口 +1 生成 runner URL:
    http://<当前hostname>:<主页面端口+1>/run/<token>
  所以代理必须同时监听两个相邻端口:
    主服务:  8088  -> 内网主服务 18080
    runner:  8089  -> 内网 runner  18081 (自动 +1)
  默认: 主服务监听 8088, runner 监听 8089

【启动方式】
  双击 teacher-proxy.exe
  会进入交互式配置向导,依次问:
    [上一层] 学校内网主服务地址 (默认 http://127.0.0.1:18080)
    [下一层] 本机主服务监听端口(默认 8088, runner 自动 +1)
    [白名单] 允许访问的客户端 IP 范围 (可选, 推荐 192.0.2.0/24)
  确认后按回车启动服务。

【学生访问】
  学生浏览器输入: http://<教师机IP>:8088/login
  教师机 IP 在程序启动后控制台日志中会自动打印,
  也可在教师机命令行执行: ipconfig 查看以太网 IPv4 地址。
  runner 端口(8089) 由前端自动使用, 学生无需关心。

【教师机防火墙放行(重要! 首次必做)】
  右键 firewall-open.bat -^> 以管理员身份运行,
  输入主端口号(默认 8088), 回车,
  会自动添加这两个端口的 Windows 入站规则。
  不放行的话学生机访问会被防火墙拦下。

【关闭服务】
  直接关闭黑色控制台窗口, 或在窗口内按 Ctrl+C。

【验证服务】
  教师机浏览器访问: http://127.0.0.1:8088/login
  能打开登录页说明主服务代理正常。
  在学生机浏览器访问 http://<教师机IP>:8089/health
  返回 {"status":"ok"} 说明 runner 代理链路正常。
  再登录学生账号，点击“运行预览”验证实际作品。

【系统要求】
  Windows 10/11 64 位
  教师机需能访问后端服务目标地址
  学生机能 ping 通教师机 IP
  教师机 8088/8089 端口未被其他程序占用

【提示】
  - 上一层地址可只填 IP:端口,程序会自动补 http:// 前缀
  - 白名单填 192.0.2.0/24 表示只允许指定局域网访问
    不填表示允许所有 IP(不推荐,但更省事)
  - 更改主端口后，runner 端口自动随之变为主端口 +1
  - 关闭控制台窗口即停止代理,不影响学生机其他网络设置

【常见问题】
  Q: 启动时提示 "端口 8089 被占用"?
  A: 教师机可能已有其他服务在 8089 端口。
     1) 排查占用: 命令行执行 netstat -ano | findstr :8089
     2) 同时调整主服务和 runner 端口, 例如:
        teacher-proxy.exe -listen :9088 (runner 自动使用 9089)
  Q: 启动时提示 "端口 8088 被占用"?
  A: 8088 被其他程序占用。换一个主端口, 例如 9088:
     teacher-proxy.exe -listen :9088
  Q: 学生访问主服务正常但代码沙盒加载失败?
  A: 1) 检查防火墙是否也放行了 runner 端口(主端口 +1)
     2) 在学生机浏览器 F12 看 Network, /run/<token> 是否 200
README

echo "==> Done."
echo "  Output dir: $DIST_DIR"
echo "  Files:"
ls -la "$DIST_DIR"

cd "$ROOT/dist"
rm -f "$ZIP_PATH"
zip -q -r -X "$ZIP_PATH" teacher-proxy/ -x "*.DS_Store" -x "*__MACOSX*"
echo "  Package: $ZIP_PATH"
