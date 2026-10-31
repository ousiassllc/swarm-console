package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// prompt の文面はサーバー側で固定（クライアントによらず同じ。API 仕様 §6.1）。
const (
	promptReportText         = "report ツールを呼び、返った markdown を手を加えずそのまま表示する。"
	promptReportLangText     = "report ツールを lang=%LANG% で呼び、返った markdown を手を加えずそのまま表示する。"
	promptDispatchTextPrefix = "この引数のまま dispatch ツールを呼び、返った sent と結果を報告する。\n引数:\n"
)

func (b *builder) addPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name: "report", Description: "全ペインの状況を report ツールで取得して、そのまま表示する",
		Arguments: []*mcp.PromptArgument{{Name: "lang", Description: "言語（ja・en）。省略は設定の既定"}},
	}, b.reportPrompt)
	if b.endpoint != tools.EndpointOps {
		return
	}
	s.AddPrompt(&mcp.Prompt{
		Name: "dispatch", Description: "空きペインへ指示を送る dispatch ツールの呼び出しを組み立てる",
		Arguments: []*mcp.PromptArgument{
			{Name: "pane", Description: "送り先のペイン"}, {Name: "agent", Description: "omp・claude・codex"},
			{Name: "cwd", Description: "作業フォルダ（shell のとき）"}, {Name: "title", Description: "タイトル（shell のとき）"},
			{Name: "command", Description: "1 行目のスラッシュコマンド"},
			{Name: "task", Description: "やること", Required: true},
			{Name: "profile", Description: "profiles.toml のプロファイル名", Required: true},
			{Name: "extra_rules", Description: "追加の条件（1 行に 1 つ）"},
		},
	}, b.dispatchPrompt)
}

func userMessage(text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}
}

func invalidParams(msg string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: msg}
}

func (b *builder) reportPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	lang := req.Params.Arguments["lang"]
	switch lang {
	case "":
		return userMessage(promptReportText), nil
	case render.LangJA, render.LangEN:
		return userMessage(strings.Replace(promptReportLangText, "%LANG%", lang, 1)), nil
	}
	return nil, invalidParams("lang は ja か en にしてください")
}

// dispatchArgs は dispatch ツールの引数のキー順（API 仕様 §5.6 の表の順）。prompt に渡された分だけを並べる。
type dispatchPromptArgs struct {
	Pane       *string  `json:"pane,omitempty"`
	Agent      *string  `json:"agent,omitempty"`
	Cwd        *string  `json:"cwd,omitempty"`
	Title      *string  `json:"title,omitempty"`
	Command    *string  `json:"command,omitempty"`
	Task       string   `json:"task"`
	Profile    string   `json:"profile"`
	ExtraRules []string `json:"extra_rules,omitempty"`
}

func (b *builder) dispatchPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	a := req.Params.Arguments
	if a["task"] == "" {
		return nil, invalidParams("task が必要です")
	}
	if a["profile"] == "" {
		return nil, invalidParams("profile が必要です")
	}
	opt := func(k string) *string {
		if v, ok := a[k]; ok && v != "" {
			return &v
		}
		return nil
	}
	args := dispatchPromptArgs{
		Pane: opt("pane"), Agent: opt("agent"), Cwd: opt("cwd"), Title: opt("title"), Command: opt("command"),
		Task: a["task"], Profile: a["profile"],
	}
	if v := a["extra_rules"]; v != "" {
		args.ExtraRules = strings.Split(v, "\n")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(args); err != nil {
		return nil, err
	}
	return userMessage(promptDispatchTextPrefix + strings.TrimRight(buf.String(), "\n")), nil
}
