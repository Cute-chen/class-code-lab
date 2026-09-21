package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	jsonBlockPattern = regexp.MustCompile("(?is)```json\\s*\\n?(.*?)```")
	errNoCodePatch   = errors.New("response does not contain a code patch")
)

type codePatch struct {
	Type         string            `json:"type"`
	Summary      string            `json:"summary"`
	Replacements []codeReplacement `json:"replacements"`
}

type codeReplacement struct {
	Old string `json:"old"`
	New string `json:"new"`
}

func applyCodePatchResponse(content, currentCode string) (string, string, error) {
	matches := jsonBlockPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if patch, ok := decodeCodePatch(match[1]); ok {
			return applyCodePatch(currentCode, patch)
		}
	}
	for offset := 0; offset < len(content); {
		relative := strings.IndexByte(content[offset:], '{')
		if relative < 0 {
			break
		}
		start := offset + relative
		decoder := json.NewDecoder(strings.NewReader(content[start:]))
		var patch codePatch
		if err := decoder.Decode(&patch); err == nil && strings.EqualFold(patch.Type, "patch") {
			return applyCodePatch(currentCode, patch)
		}
		offset = start + 1
	}
	return "", "", errNoCodePatch
}

func decodeCodePatch(value string) (codePatch, bool) {
	var patch codePatch
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &patch); err != nil || !strings.EqualFold(patch.Type, "patch") {
		return codePatch{}, false
	}
	return patch, true
}

func applyCodePatch(currentCode string, patch codePatch) (string, string, error) {
	if strings.TrimSpace(currentCode) == "" {
		return "", "", errors.New("当前没有可修改的作品代码")
	}
	if len(patch.Replacements) == 0 || len(patch.Replacements) > 20 {
		return "", "", errors.New("代码补丁必须包含 1-20 个精确替换")
	}
	updated := currentCode
	for index, replacement := range patch.Replacements {
		if replacement.Old == "" {
			return "", "", fmt.Errorf("第 %d 个替换的 old 不能为空", index+1)
		}
		count := strings.Count(updated, replacement.Old)
		if count != 1 {
			return "", "", fmt.Errorf("第 %d 个替换在当前代码中匹配到 %d 处，需要唯一精确匹配", index+1, count)
		}
		updated = strings.Replace(updated, replacement.Old, replacement.New, 1)
	}
	if updated == currentCode {
		return "", "", errors.New("代码补丁没有产生变化")
	}
	if len([]byte(updated)) > MaxCodeBytes {
		return "", "", fmt.Errorf("补丁应用后代码超过 %dKB", MaxCodeBytes/1024)
	}
	summary := strings.TrimSpace(patch.Summary)
	if summary == "" {
		summary = "AI 返回了可安全应用的增量修改。"
	}
	return updated, truncateRunes(summary, 500), nil
}

func resolveAIProposal(content, currentCode string) (code, summary, format, warning string) {
	if fullCode := extractLargestHTMLBlock(content); fullCode != "" {
		return fullCode, "AI 返回了一份完整代码，应用前请先查看安全检查结果。", "full_html", ""
	}
	patchedCode, patchSummary, err := applyCodePatchResponse(content, currentCode)
	if err == nil {
		return patchedCode, patchSummary, "patch", ""
	}
	if errors.Is(err, errNoCodePatch) {
		return "", "", "", ""
	}
	return "", "", "", "AI 返回了增量修改，但无法与当前代码唯一匹配，因此未生成可应用提案。请重试或要求 AI 返回完整 HTML。"
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
