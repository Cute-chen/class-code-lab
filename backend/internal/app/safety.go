package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

const MaxCodeBytes = 512 * 1024

type SafetyResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

var (
	dangerousJS = []struct {
		pattern *regexp.Regexp
		label   string
	}{
		{regexp.MustCompile(`(?i)\bfetch\s*\(`), "不能使用 fetch 发起网络请求"},
		{regexp.MustCompile(`(?i)\bXMLHttpRequest\b`), "不能使用 XMLHttpRequest 发起网络请求"},
		{regexp.MustCompile(`(?i)\bWebSocket\b|\bEventSource\b|sendBeacon\s*\(`), "不能建立后台通信连接"},
		{regexp.MustCompile(`(?i)\bwindow\.open\s*\(|\bshowModalDialog\s*\(`), "不能打开新窗口"},
		{regexp.MustCompile(`(?i)\beval\s*\(|\bnew\s+Function\s*\(`), "不能动态执行字符串代码"},
		{regexp.MustCompile(`(?i)\b(importScripts|Worker|SharedWorker)\s*\(`), "不能启动 Worker"},
		{regexp.MustCompile(`(?i)\b(location\.(href|replace|assign)|window\.location|top\.location)\s*=`), "不能跳转当前页面"},
		{regexp.MustCompile(`(?i)\bwhile\s*\(\s*true\s*\)|\bfor\s*\(\s*;\s*;\s*\)`), "检测到明显的无限循环"},
	}
	versionSegment = regexp.MustCompile(`(?i)(?:@|/)(?:v?\d+)(?:\.\d+){0,3}(?:[-+][a-z0-9.-]+)?(?:/|$)`)
)

func CheckHTMLSafety(code string) SafetyResult {
	issues := make(map[string]struct{})
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		issues["代码不能为空"] = struct{}{}
	}
	if len([]byte(code)) > MaxCodeBytes {
		issues[fmt.Sprintf("代码不能超过 %dKB", MaxCodeBytes/1024)] = struct{}{}
	}
	for _, check := range dangerousJS {
		if check.pattern.MatchString(code) {
			issues[check.label] = struct{}{}
		}
	}

	doc, err := html.Parse(strings.NewReader(code))
	if err != nil {
		issues["HTML 结构无法解析，请让 AI 返回完整的单文件 HTML"] = struct{}{}
	} else {
		walkHTML(doc, issues)
	}
	if err := validateHTMLStructure(code); err != nil {
		issues[err.Error()] = struct{}{}
	}

	result := SafetyResult{OK: len(issues) == 0, Issues: make([]string, 0, len(issues))}
	for issue := range issues {
		result.Issues = append(result.Issues, issue)
	}
	sort.Strings(result.Issues)
	return result
}

var voidElements = map[string]struct{}{
	"area": {}, "base": {}, "br": {}, "col": {}, "embed": {}, "hr": {},
	"img": {}, "input": {}, "link": {}, "meta": {}, "param": {}, "source": {},
	"track": {}, "wbr": {},
}

var optionalEndElements = map[string]struct{}{
	"body": {}, "colgroup": {}, "dd": {}, "dt": {}, "head": {}, "html": {},
	"li": {}, "optgroup": {}, "option": {}, "p": {}, "rp": {}, "rt": {},
	"tbody": {}, "td": {}, "tfoot": {}, "th": {}, "thead": {}, "tr": {},
}

// html.Parse intentionally repairs incomplete markup. The safety gate needs a
// stricter signal so a visibly broken document cannot be presented as ready to
// preview or publish.
func validateHTMLStructure(code string) error {
	tokenizer := html.NewTokenizer(strings.NewReader(code))
	stack := make([]string, 0, 16)
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			if tokenizer.Err() != io.EOF {
				return fmt.Errorf("HTML 结构无法解析，请修复标签后再试")
			}
			for _, tag := range stack {
				if _, optional := optionalEndElements[tag]; !optional {
					return fmt.Errorf("HTML 结构不完整或标签不匹配，请修复后再预览或发布")
				}
			}
			return nil
		case html.StartTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if _, void := voidElements[tag]; !void {
				for len(stack) > 0 && implicitlyClosedBy(tag, stack[len(stack)-1]) {
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, tag)
			}
		case html.SelfClosingTagToken:
			// A self-closing token never needs a matching end tag.
		case html.EndTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if _, void := voidElements[tag]; void {
				continue
			}
			index := len(stack) - 1
			for index >= 0 && stack[index] != tag {
				if _, optional := optionalEndElements[stack[index]]; !optional {
					return fmt.Errorf("HTML 结构不完整或标签不匹配，请修复后再预览或发布")
				}
				index--
			}
			if index < 0 {
				return fmt.Errorf("HTML 结构不完整或标签不匹配，请修复后再预览或发布")
			}
			stack = stack[:index]
		}
	}
}

func implicitlyClosedBy(next, open string) bool {
	switch next {
	case "li":
		return open == "li"
	case "dt", "dd":
		return open == "dt" || open == "dd"
	case "p":
		return open == "p"
	case "option":
		return open == "option"
	case "optgroup":
		return open == "option" || open == "optgroup"
	case "tr":
		return open == "tr"
	case "td", "th":
		return open == "td" || open == "th"
	case "thead", "tbody", "tfoot":
		return open == "thead" || open == "tbody" || open == "tfoot"
	case "colgroup":
		return open == "colgroup"
	default:
		return false
	}
}

func walkHTML(node *html.Node, issues map[string]struct{}) {
	if node.Type == html.ElementNode {
		tag := strings.ToLower(node.Data)
		switch tag {
		case "iframe", "frame", "frameset", "object", "embed", "applet":
			issues["不能嵌入其他网页或外部对象"] = struct{}{}
		case "form":
			issues["不能提交表单"] = struct{}{}
		case "base":
			issues["不能修改页面基础地址"] = struct{}{}
		case "input":
			if attrValue(node, "type") == "password" || attrValue(node, "type") == "file" {
				issues["不能创建密码或文件上传输入框"] = struct{}{}
			}
		case "meta":
			if strings.EqualFold(attrValue(node, "http-equiv"), "refresh") {
				issues["不能自动刷新或跳转页面"] = struct{}{}
			}
		}

		for _, attr := range node.Attr {
			name := strings.ToLower(attr.Key)
			value := strings.TrimSpace(attr.Val)
			if strings.HasPrefix(name, "on") && strings.Contains(strings.ToLower(value), "window.open") {
				issues["不能打开新窗口"] = struct{}{}
			}
			if (name == "href" || name == "src" || name == "action" || name == "poster") && strings.HasPrefix(strings.ToLower(value), "javascript:") {
				issues["不能使用 javascript: 地址"] = struct{}{}
			}
			if tag == "a" && name == "href" && value != "" && !strings.HasPrefix(value, "#") {
				issues["不能跳转到其他网页"] = struct{}{}
			}
		}

		switch tag {
		case "script":
			if src := attrValue(node, "src"); src != "" && !allowedCDNURL(src) {
				issues["脚本只能使用带明确版本号的 jsDelivr 或 cdnjs 地址"] = struct{}{}
			}
		case "link":
			if href := attrValue(node, "href"); href != "" && !allowedCDNURL(href) {
				issues["样式只能使用带明确版本号的 jsDelivr 或 cdnjs 地址"] = struct{}{}
			}
		case "img", "audio", "video", "source":
			if src := attrValue(node, "src"); src != "" && !allowedMediaURL(src) {
				issues["图片和音频只能使用 data、blob 或带版本号的白名单 CDN 地址"] = struct{}{}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, issues)
	}
}

func attrValue(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func allowedMediaURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") || allowedCDNURL(raw)
}

func allowedCDNURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "cdn.jsdelivr.net" && host != "cdnjs.cloudflare.com" {
		return false
	}
	path := parsed.EscapedPath()
	return versionSegment.MatchString(path) && !strings.Contains(strings.ToLower(path), "latest")
}

func injectRunnerBridge(code string, captureIDs ...string) string {
	captureID := ""
	if len(captureIDs) > 0 {
		captureID = captureIDs[0]
	}
	captureLibrary := ""
	captureScript := ""
	if captureID != "" {
		encodedCaptureID, _ := json.Marshal(captureID)
		captureLibrary = `<script src="/runner-assets/html2canvas.min.js"></script>`
		captureScript = fmt.Sprintf(`
var captureId=%s;
var captureRenderer=window.html2canvas;
window.addEventListener('load',function(){
  var fontsReady=document.fonts&&document.fonts.ready?document.fonts.ready:Promise.resolve();
  Promise.resolve(fontsReady).catch(function(){}).then(function(){
    setTimeout(function(){
      if(typeof captureRenderer!=='function'){send('thumbnail-error','封面截图组件加载失败',captureId);return}
      captureRenderer(document.documentElement,{backgroundColor:'#ffffff',logging:false,useCORS:true,allowTaint:false,scale:0.5,width:window.innerWidth,height:window.innerHeight,windowWidth:window.innerWidth,windowHeight:window.innerHeight,scrollX:0,scrollY:0}).then(function(canvas){
        send('thumbnail-captured',canvas.toDataURL('image/jpeg',0.82),captureId);
      }).catch(function(error){send('thumbnail-error',error&&error.message?error.message:'封面截取失败',captureId)});
    },450);
  });
});`, encodedCaptureID)
	}
	bridge := captureLibrary + `<script>(function(){
var send=function(type,value,captureId){try{parent.postMessage({source:'class-code-lab-runner',type:type,value:String(value||''),capture_id:captureId||''},'*')}catch(_){}};
window.addEventListener('error',function(e){send('runtime-error',e.message+' @ '+(e.filename||'作品代码')+':'+(e.lineno||0),typeof captureId==='string'?captureId:'')});
window.addEventListener('unhandledrejection',function(e){send('runtime-error',e.reason&&e.reason.message?e.reason.message:e.reason,typeof captureId==='string'?captureId:'')});
send('runner-ready','作品已开始运行');
` + captureScript + `
})();</script>`
	lower := strings.ToLower(code)
	if index := strings.Index(lower, "<head"); index >= 0 {
		if end := strings.Index(lower[index:], ">"); end >= 0 {
			at := index + end + 1
			return code[:at] + bridge + code[at:]
		}
	}
	return bridge + code
}
