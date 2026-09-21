package app

import (
	"strings"
	"testing"
)

func TestApplyCodePatchResponseAppliesUniqueReplacements(t *testing.T) {
	current := `<!doctype html><html><style>body { color: red; }</style><body><button>开始</button></body></html>`
	response := "已修改颜色和按钮。\n```json\n" +
		`{"type":"patch","summary":"换成蓝色并修改按钮文字","replacements":[{"old":"color: red","new":"color: blue"},{"old":"<button>开始</button>","new":"<button>开始游戏</button>"}]}` +
		"\n```"
	updated, summary, err := applyCodePatchResponse(response, current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "color: blue") || !strings.Contains(updated, "开始游戏") || summary != "换成蓝色并修改按钮文字" {
		t.Fatalf("unexpected patch result: code=%q summary=%q", updated, summary)
	}
}

func TestApplyCodePatchRejectsAmbiguousReplacement(t *testing.T) {
	current := `<html><body><span>x</span><span>x</span></body></html>`
	response := "```json\n" +
		`{"type":"patch","summary":"change","replacements":[{"old":"<span>x</span>","new":"<span>y</span>"}]}` +
		"\n```"
	if _, _, err := applyCodePatchResponse(response, current); err == nil || !strings.Contains(err.Error(), "2 处") {
		t.Fatalf("expected ambiguous replacement rejection, got %v", err)
	}
}

func TestApplyCodePatchResponseAcceptsBareJSON(t *testing.T) {
	current := `<html><body class="day">房间</body></html>`
	response := `{"type":"patch","summary":"切换为夜晚","replacements":[{"old":"class=\"day\"","new":"class=\"night\""}]}`
	updated, summary, err := applyCodePatchResponse(response, current)
	if err != nil || !strings.Contains(updated, `class="night"`) || summary != "切换为夜晚" {
		t.Fatalf("bare JSON patch failed: code=%q summary=%q err=%v", updated, summary, err)
	}
}

func TestApplyCodePatchResponseAcceptsJSONAfterExplanation(t *testing.T) {
	current := `<html><body>原文</body></html>`
	response := "下面是增量修改：\n" + `{"type":"patch","summary":"换文字","replacements":[{"old":"原文","new":"新文"}]}`
	updated, _, err := applyCodePatchResponse(response, current)
	if err != nil || !strings.Contains(updated, "新文") {
		t.Fatalf("embedded JSON patch failed: code=%q err=%v", updated, err)
	}
}

func TestResolveAIProposalFallsBackToFullHTML(t *testing.T) {
	content := "重写完成\n```html\n<!doctype html><html><body>new</body></html>\n```"
	code, summary, format, warning := resolveAIProposal(content, "<html><body>old</body></html>")
	if !strings.Contains(code, "new") || summary == "" || format != "full_html" || warning != "" {
		t.Fatalf("unexpected full HTML proposal: code=%q summary=%q format=%q warning=%q", code, summary, format, warning)
	}
}

func TestResolveAIProposalReturnsWarningForInvalidPatch(t *testing.T) {
	content := "```json\n" +
		`{"type":"patch","summary":"change","replacements":[{"old":"missing","new":"value"}]}` +
		"\n```"
	code, _, _, warning := resolveAIProposal(content, "<html><body>old</body></html>")
	if code != "" || warning == "" {
		t.Fatalf("expected invalid patch warning, code=%q warning=%q", code, warning)
	}
}
