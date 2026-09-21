package app

import (
	"strings"
	"testing"
)

func TestCheckHTMLSafetyAllowsOrdinaryFunctionsAndPinnedCDN(t *testing.T) {
	code := `<!doctype html><html><head><script src="https://cdn.jsdelivr.net/npm/p5@1.9.0/lib/p5.min.js"></script></head><body><button onclick="play()">开始</button><script>function play(){ document.body.dataset.started = 'yes' }</script></body></html>`
	result := CheckHTMLSafety(code)
	if !result.OK {
		t.Fatalf("expected safe HTML, got issues: %v", result.Issues)
	}
}

func TestCheckHTMLSafetyRejectsNetworkNavigationAndDynamicCode(t *testing.T) {
	code := `<!doctype html><html><body><a href="https://example.com">离开</a><script>fetch('https://example.com'); eval('alert(1)')</script></body></html>`
	result := CheckHTMLSafety(code)
	if result.OK {
		t.Fatal("expected unsafe HTML")
	}
	joined := strings.Join(result.Issues, "|")
	for _, expected := range []string{"fetch", "动态执行", "跳转"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected issue containing %q, got %v", expected, result.Issues)
		}
	}
}

func TestCheckHTMLSafetyRejectsFloatingCDNVersion(t *testing.T) {
	code := `<!doctype html><html><head><script src="https://cdn.jsdelivr.net/npm/p5@latest/lib/p5.min.js"></script></head><body></body></html>`
	result := CheckHTMLSafety(code)
	if result.OK {
		t.Fatal("expected latest CDN URL to be rejected")
	}
}

func TestCheckHTMLSafetyRejectsBrokenHTMLStructure(t *testing.T) {
	for _, code := range []string{
		`<!doctype html><html><head><title>测试</title></head><body><main><h1>缺少结束标签</h1></body></html>`,
		`<!doctype html><html><body><div><span>标签顺序错误</div></span></body></html>`,
	} {
		result := CheckHTMLSafety(code)
		if result.OK || !strings.Contains(strings.Join(result.Issues, "|"), "结构") {
			t.Fatalf("expected broken HTML to be rejected, got: %+v", result)
		}
	}
}

func TestCheckHTMLSafetyAllowsOptionalEndTags(t *testing.T) {
	result := CheckHTMLSafety(`<!doctype html><html><body><ul><li>一<li>二</ul><p>一段<p>另一段</body></html>`)
	if !result.OK {
		t.Fatalf("expected optional HTML end tags to be accepted, got issues: %v", result.Issues)
	}
}

func TestInjectRunnerBridge(t *testing.T) {
	result := injectRunnerBridge("<!doctype html><html><head><title>x</title></head><body></body></html>")
	if !strings.Contains(result, "class-code-lab-runner") || strings.Index(result, "class-code-lab-runner") > strings.Index(result, "<title>") {
		t.Fatalf("bridge was not injected at the beginning of head: %s", result)
	}
}
