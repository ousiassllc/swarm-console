# swarm-console レポート

- 時刻: {{.At}}
- サーバー負荷: load1 {{.Load1}} / CPU {{.CPU}} / メモリ空き {{.MemFree}} GB / スワップ {{.Swap}}
- ディスク: {{.Disk}}
- 更新: {{.Updates}}
- ログイン: {{.Login}}

## ペイン

| 位置 | エージェント | 状態 | 経過 | 依頼元 | リポジトリ / Issue | いまやっていること | 次にやること |
|------|------------|------|------|--------|------------------|------------------|------------|
{{range .Panes}}| {{.Pos}} | {{.Agent}} | {{.State}} | {{.Elapsed}} | {{.From}} | {{.Repo}} | {{.Activity}} | {{.Next}} |
{{end}}
## 要対応

{{range .Attention}}- {{.}}
{{else}}- なし
{{end}}
## 使用状況

{{with .Usage}}{{if .Line}}- {{.Line}}
{{else}}{{range $i, $s := .Sections}}{{if $i}}
{{end}}### {{$s.Name}}

{{$s.Body}}
{{end}}{{if .Stale}}
- {{.Stale}}
{{end}}{{end}}{{end}}
{{- if .Warnings}}
## 警告

{{range .Warnings}}- {{.}}
{{end}}
{{- end}}
{{- with .Detail}}
## 詳細: {{.Pos}}

- ペイン ID: {{.ID}}
- タイトル: {{.Title}}
- 作業フォルダ: {{.Cwd}}
- 経過: {{.Elapsed}}
- 最後の変化: {{.ChangedAgo}}
- 最終報告: {{.ReportLine}}

### メモ

{{with .Note}}{{range .Fields}}- {{.Label}}: {{.Value}}
{{end}}{{if .Log}}- ログ:
{{range .Log}}  - {{.}}
{{end}}{{end}}{{else}}- なし
{{end}}
{{- with .Card}}
### 質問カード

- 質問: {{.Question}}
{{range .Options}}  {{.N}}. {{.Label}}{{if .Recommended}}（推奨）{{end}}
{{end}}- カーソル: {{.Cursor}}
- 自由入力: {{.AllowsCustom}}
{{end}}
### 画面の最後の {{.TailCount}} 行

{{.Tail}}
{{- end}}
