# swarm-console report

- Time: {{.At}}
- Server load: load1 {{.Load1}} / Mem free {{.MemFree}} GB

## Panes

| Pos | Agent | State | From | Repo / Issue | Doing now | Next |
|-----|-------|-------|------|--------------|-----------|------|
{{range .Panes}}| {{.Pos}} | {{.Agent}} | {{.State}} | {{.From}} | {{.Repo}} | {{.Activity}} | {{.Next}} |
{{end}}
## Needs attention

{{range .Attention}}- {{.}}
{{else}}- None
{{end}}
{{- if .Warnings}}
## Warnings

{{range .Warnings}}- {{.}}
{{end}}
{{- end}}
{{- with .Detail}}
## Detail: {{.Pos}}

- Pane ID: {{.ID}}
- Title: {{.Title}}
- Working directory: {{.Cwd}}
- Elapsed: {{.Elapsed}}
- Last change: {{.ChangedAgo}}
- Final report: {{.ReportLine}}

### Note

{{with .Note}}{{range .Fields}}- {{.Label}}: {{.Value}}
{{end}}{{if .Log}}- Log:
{{range .Log}}  - {{.}}
{{end}}{{end}}{{else}}- None
{{end}}
{{- with .Card}}
### Question card

- Question: {{.Question}}
{{range .Options}}  {{.N}}. {{.Label}}{{if .Recommended}} (recommended){{end}}
{{end}}- Cursor: {{.Cursor}}
- Free text: {{.AllowsCustom}}
{{end}}
### Last {{.TailCount}} screen lines

{{.Tail}}
{{- end}}
