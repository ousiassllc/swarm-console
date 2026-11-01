# swarm-console report

- Time: {{.At}}
- Server load: load1 {{.Load1}} / CPU {{.CPU}} / Mem free {{.MemFree}} GB / Swap {{.Swap}}
- Disk: {{.Disk}}
- Updates: {{.Updates}}
- Login: {{.Login}}

## Panes

| Pos | Agent | State | Elapsed | From | Repo / Issue | Doing now | Next |
|-----|-------|-------|---------|------|--------------|-----------|------|
{{range .Panes}}| {{.Pos}} | {{.Agent}} | {{.State}} | {{.Elapsed}} | {{.From}} | {{.Repo}} | {{.Activity}} | {{.Next}} |
{{end}}
## Needs attention

{{range .Attention}}- {{.}}
{{else}}- None
{{end}}
## Usage

{{with .Usage}}{{if .Line}}- {{.Line}}
{{else}}{{range $i, $s := .Sections}}{{if $i}}
{{end}}### {{$s.Name}}

{{$s.Body}}
{{end}}{{if .Stale}}
- {{.Stale}}
{{end}}{{end}}{{end}}
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
