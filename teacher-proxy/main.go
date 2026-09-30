// teacher-proxy: 教师机反向代理程序 (双端口版)
//
// 专用于代理 class-code-lab 这类"主服务 + runner"双端口服务。
// 主服务监听 :PORT, 转发到内网主服务 (如 127.0.0.1:18080);
// runner 监听 :PORT+1, 转发到内网 runner (如 127.0.0.1:18081)。
// 前端使用当前页面的主机名及主端口 +1 访问 runner。
//
//	学生访问 http://<教师机IP>:8088/login       -> 内网主服务
//	学生访问 http://<教师机IP>:8089/run/<token> -> 内网 runner
//
// 双击 exe (不传任何参数) 会进入交互式引导;
// 也可直接用命令行参数:
//
//	teacher-proxy.exe -listen :8080 -target http://127.0.0.1:18080
//
// 仅使用 Go 标准库,无外部依赖,便于交叉编译为 Windows exe。
package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	listenAddr = flag.String("listen", ":8088", "本机主服务监听地址 (runner 自动使用下一端口)")
	targetURL  = flag.String("target", "http://127.0.0.1:18080", "内网主服务地址 (runner 转发端口自动 +1)")
	allowCIDR  = flag.String("allow", "", "可选,允许访问的客户端 CIDR,如 192.0.2.0/24;留空表示允许所有")
	skipVerify = flag.Bool("insecure", false, "后端为 HTTPS 且证书无效时使用(谨慎,仅限内网自签名)")
)

func main() {
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime)

	// 双击启动(未传任何 -flag)时进入交互式引导
	if !anyFlagSet() {
		runInteractive()
	}

	// 解析主服务目标
	mainTarget, err := url.Parse(*targetURL)
	if err != nil {
		log.Fatalf("目标地址无效 %q: %v", *targetURL, err)
	}
	if mainTarget.Scheme == "" || mainTarget.Host == "" {
		log.Fatalf("目标地址必须形如 http://host:port 或 https://host:port,当前: %q", *targetURL)
	}

	// 推导 runner 转发目标 (主 target 端口 + 1)
	runnerTarget := bumpPort(mainTarget, +1)
	listenMain := normalizeListen(*listenAddr)
	listenRunner, err := nextListenAddr(listenMain)
	if err != nil {
		log.Fatalf("本机主服务监听地址无效 %q: %v", listenMain, err)
	}

	// 访问控制中间件 (两个端口共用)
	var allowNet *net.IPNet
	if *allowCIDR != "" {
		_, n, err := net.ParseCIDR(*allowCIDR)
		if err != nil {
			if ip := net.ParseIP(*allowCIDR); ip != nil {
				n = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
			} else {
				log.Fatalf("无效的 -allow CIDR: %q", *allowCIDR)
			}
		}
		allowNet = n
	}

	mainSrv := newProxyServer(listenMain, mainTarget, allowNet, *skipVerify, "main")
	runnerSrv := newProxyServer(listenRunner, runnerTarget, allowNet, *skipVerify, "runner")

	// 启动信息
	log.Printf("=== 教师机反向代理 (双端口) ===")
	log.Printf("[主服务]  %-22s -> %s", listenMain, mainTarget.String())
	log.Printf("[runner]  %-22s -> %s", listenRunner, runnerTarget.String())
	if allowNet != nil {
		log.Printf("白名单:   %s", *allowCIDR)
	} else {
		log.Printf("白名单:   (允许所有)")
	}
	log.Printf("本机 IPv4 地址:")
	ips := localIPv4s()
	if len(ips) == 0 {
		log.Printf("  (未找到 IPv4 地址)")
	}
	for _, ip := range ips {
		log.Printf("  %s -> 主服务: http://%s%s  runner: http://%s%s",
			ip, ip, displayPort(listenMain), ip, displayPort(listenRunner))
	}
	log.Printf("按 Ctrl+C 停止")
	fmt.Println()

	// 主服务阻塞运行; runner 用 goroutine
	go func() {
		if err := runnerSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[runner] 启动失败: %v (端口 %s 是否被占用?)", err, listenRunner)
		}
	}()
	if err := mainSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[主服务] 启动失败: %v (端口 %s 是否被占用?)", err, listenMain)
	}
}

// newProxyServer 构造一个监听 listenAddr 的反向代理 server
func newProxyServer(listenAddr string, target *url.URL, allowNet *net.IPNet, insecure bool, tag string) *http.Server {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[%-6s ERR] %s %s -> %v", tag, r.Method, r.URL.Path, err)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "代理无法连接后端服务 %s: %s\n", target.String(), err)
		},
	}
	// Go 标准库 ReverseProxy 内置 WebSocket Upgrade 支持
	if insecure {
		proxy.Transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			ResponseHeaderTimeout: 30 * time.Second,
		}
	}
	handler := accessControl(logMiddleware(proxy, tag), allowNet)
	return &http.Server{
		Addr:         listenAddr,
		Handler:      handler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// accessControl 限制客户端来源
func accessControl(next http.Handler, network *net.IPNet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if network != nil {
			ipStr, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ipStr = r.RemoteAddr
			}
			ip := net.ParseIP(ipStr)
			if ip == nil || !network.Contains(ip) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, "403 当前 IP %s 不在允许范围内\n", ipStr)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// logMiddleware 记录每条访问日志
func logMiddleware(next http.Handler, tag string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isWS := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		t := "HTTP"
		if isWS {
			t = "WS"
		}
		log.Printf("[%-6s %2s] %s %s %s", tag, t, clientIP(r), r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// localIPv4s 返回非回环的 IPv4 地址
func localIPv4s() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if v4 := ipnet.IP.To4(); v4 != nil {
				out = append(out, v4.String())
			}
		}
	}
	return out
}

// displayPort 把 ":8088" / "0.0.0.0:8088" 统一显示为 ":8088"
func displayPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return addr
}

// normalizeListen 把 "8080" 之类的纯数字补成 ":8080"
func normalizeListen(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, ":") {
		return ":" + s
	}
	return s
}

// nextListenAddr 保持监听网卡不变，并使用主服务端口的下一端口。
func nextListenAddr(addr string) (string, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port >= 65535 {
		return "", fmt.Errorf("端口须在 1 到 65534 之间")
	}
	return net.JoinHostPort(host, strconv.Itoa(port+1)), nil
}

// bumpPort 把 URL 的端口加 n
func bumpPort(u *url.URL, n int) *url.URL {
	out := *u
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		// 没有显式端口,按 scheme 默认 +n
		defPort := 80
		if u.Scheme == "https" {
			defPort = 443
		}
		out.Host = net.JoinHostPort(u.Host, strconv.Itoa(defPort+n))
		return &out
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return &out
	}
	out.Host = net.JoinHostPort(host, strconv.Itoa(p+n))
	return &out
}

// anyFlagSet 检测是否传了任意 -flag
func anyFlagSet() bool {
	set := false
	flag.Visit(func(f *flag.Flag) { set = true })
	return set
}

// runInteractive 双击 exe 时进入的交互式配置向导
func runInteractive() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("==========================================")
	fmt.Println("  教师机反向代理 - 配置向导 (双端口)")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("作用: 让学生机通过教师机访问学校内网服务")
	fmt.Println("      自动代理主服务和 runner 两个端口")
	fmt.Println()
	fmt.Println("网络拓扑:")
	fmt.Println("  学生机 --→ 教师机:下层端口    --→ 学校内网主服务")
	fmt.Println("  学生机 --→ 教师机:下层端口+1  --→ 学校内网 runner")
	fmt.Println("                (本程序)")
	fmt.Println()

	// 上一层 - 内网主服务地址
	fmt.Println("[上一层] 学校内网主服务地址")
	fmt.Println("  例: http://127.0.0.1:18080")
	fmt.Printf("  直接回车使用默认 [%s]: ", *targetURL)
	if s := readLine(reader); s != "" {
		if !strings.Contains(s, "://") {
			s = "http://" + s
		}
		*targetURL = s
	}
	fmt.Println()

	// 下一层 - 监听端口
	fmt.Println("[下一层] 本机主服务监听端口")
	fmt.Println("  runner 自动使用主服务端口 +1")
	fmt.Println("  学生访问主服务: http://<教师机IP>:<本端口>")
	fmt.Println("  学生访问 runner: http://<教师机IP>:<本端口+1>")
	fmt.Printf("  直接回车使用默认 [%s]: ", strings.TrimPrefix(displayPort(*listenAddr), ":"))
	if s := readLine(reader); s != "" {
		if !strings.HasPrefix(s, ":") {
			s = ":" + s
		}
		*listenAddr = s
	}
	fmt.Println()

	// 白名单
	fmt.Println("[白名单] 允许访问的客户端 IP 范围 (可选, 推荐)")
	fmt.Println("  例: 192.0.2.0/24 表示只允许机房局域网访问")
	fmt.Println("  直接回车 = 允许所有 IP 访问")
	fmt.Print("  请输入: ")
	if s := readLine(reader); s != "" {
		*allowCIDR = s
	}
	fmt.Println()

	// 解析推导后的 runner 地址 (主 target 端口 +1)
	mainTarget, err := url.Parse(*targetURL)
	if err != nil || mainTarget.Scheme == "" || mainTarget.Host == "" {
		fmt.Printf("!! 目标地址无效,请重新运行 %q\n", *targetURL)
		os.Exit(1)
	}
	runnerTarget := bumpPort(mainTarget, +1)
	listenMain := normalizeListen(*listenAddr)
	listenRunner, err := nextListenAddr(listenMain)
	if err != nil {
		fmt.Printf("!! 本机主服务监听地址无效: %v\n", err)
		os.Exit(1)
	}

	// 确认
	fmt.Println("------------------------------------------")
	fmt.Println("配置确认:")
	fmt.Printf("  [主服务]  %-10s -> %s\n", listenMain, mainTarget.String())
	fmt.Printf("  [runner]  %-10s -> %s\n", listenRunner, runnerTarget.String())
	if *allowCIDR != "" {
		fmt.Printf("  白名单:   %s\n", *allowCIDR)
	} else {
		fmt.Printf("  白名单:   (允许所有)\n")
	}
	fmt.Println()
	fmt.Println("本机 IPv4 地址 (学生访问这些地址):")
	ips := localIPv4s()
	if len(ips) == 0 {
		fmt.Println("  (未找到,请教师机执行 ipconfig 查看)")
	}
	for _, ip := range ips {
		fmt.Printf("  %s -> 主服务: http://%s%s  runner: http://%s%s\n",
			ip, ip, displayPort(listenMain), ip, displayPort(listenRunner))
	}
	fmt.Println()
	fmt.Print("按回车开始服务 (或 Ctrl+C 退出): ")
	_ = readLine(reader)
	fmt.Println("==========================================")
	fmt.Println()
}

// readLine 从 reader 读取一行并去空白
func readLine(reader *bufio.Reader) string {
	s, err := reader.ReadString('\n')
	if err != nil && s == "" {
		return ""
	}
	return strings.TrimSpace(s)
}
