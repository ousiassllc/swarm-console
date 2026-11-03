package notes

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// 上限（ER §2.2・§2.3、API §7）。文字数は rune 数。
const (
	maxString   = 2000
	maxMemo     = 4000
	maxArray    = 50
	maxLogText  = 500
	reasonType  = "type"
	reasonLong  = "too_long"
	reasonMany  = "too_many"
	reasonBad   = "invalid"
	reasonUnk   = "unknown_key"
	reasonRO    = "read_only"
	reasonEmpty = "empty"
)

// change は検証済みの 1 項目の変更。strs は秘密検査の対象になる文字列値（キー名を含まない）。
type change struct {
	key   string
	apply func(*Note)
	strs  []string
}

func invalid(field, reason string) *apperr.Error {
	return apperr.New(apperr.InvalidArgs, "field", field, "reason", reason)
}

func stringField(key string) (func(*Note) *string, int) {
	limit := maxString
	if key == "memo" {
		limit = maxMemo
	}
	switch key {
	case "from":
		return func(n *Note) *string { return &n.From }, limit
	case "started_by":
		return func(n *Note) *string { return &n.StartedBy }, limit
	case "repo":
		return func(n *Note) *string { return &n.Repo }, limit
	case "worktree":
		return func(n *Note) *string { return &n.Worktree }, limit
	case "branch":
		return func(n *Note) *string { return &n.Branch }, limit
	case "instruction":
		return func(n *Note) *string { return &n.Instruction }, limit
	case "status":
		return func(n *Note) *string { return &n.Status }, limit
	case "next":
		return func(n *Note) *string { return &n.Next }, limit
	case "resume_id":
		return func(n *Note) *string { return &n.ResumeID }, limit
	case "memo":
		return func(n *Note) *string { return &n.Memo }, limit
	}
	return nil, 0
}

func arrayField(key string) func(*Note) *[]string {
	switch key {
	case "items":
		return func(n *Note) *[]string { return &n.Items }
	case "rules":
		return func(n *Note) *[]string { return &n.Rules }
	}
	return nil
}

// parseChanges は set を検証し、キー名の昇順で変更の列を返す（どのエラーが先に出るかを決定的にする）。
func parseChanges(set map[string]json.RawMessage) ([]change, error) {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]change, 0, len(keys))
	for _, k := range keys {
		c, err := parseChange(k, set[k])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func parseChange(key string, raw json.RawMessage) (change, error) {
	if key == "updated_at" || key == "log" {
		return change{}, invalid(key, reasonRO)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return change{}, invalid(key, reasonType)
	}
	if sf, limit := stringField(key); sf != nil {
		return parseString(key, raw, sf, limit)
	}
	if af := arrayField(key); af != nil {
		return parseArray(key, raw, af)
	}
	switch key {
	case "origin":
		return parseOrigin(raw)
	case "report_to":
		return parseReportTo(raw)
	case "approval":
		return parseApproval(raw)
	}
	return change{}, invalid(key, reasonUnk)
}

func parseString(key string, raw json.RawMessage, f func(*Note) *string, limit int) (change, error) {
	if isNull(raw) {
		return change{key: key, apply: func(n *Note) { *f(n) = "" }}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return change{}, invalid(key, reasonType)
	}
	if utf8.RuneCountInString(s) > limit {
		return change{}, invalid(key, reasonLong)
	}
	return change{key: key, apply: func(n *Note) { *f(n) = s }, strs: []string{s}}, nil
}

// parseOrigin は origin（user | agent）を検証する。null と空文字は消す（ほかの文字列項目と同じ。FR-NOTE-4）。ほかの値は invalid_args。
func parseOrigin(raw json.RawMessage) (change, error) {
	const key = "origin"
	if isNull(raw) {
		return change{key: key, apply: func(n *Note) { n.Origin = "" }}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return change{}, invalid(key, reasonType)
	}
	if s == "" {
		return change{key: key, apply: func(n *Note) { n.Origin = "" }}, nil
	}
	if s != OriginUser && s != OriginAgent {
		return change{}, invalid(key, reasonBad)
	}
	return change{key: key, apply: func(n *Note) { n.Origin = s }, strs: []string{s}}, nil
}

func parseArray(key string, raw json.RawMessage, f func(*Note) *[]string) (change, error) {
	if isNull(raw) {
		return change{key: key, apply: func(n *Note) { *f(n) = nil }}, nil
	}
	var elems []*string
	if err := json.Unmarshal(raw, &elems); err != nil {
		return change{}, invalid(key, reasonType)
	}
	if len(elems) > maxArray {
		return change{}, invalid(key, reasonMany)
	}
	vals := make([]string, 0, len(elems))
	for _, e := range elems {
		if e == nil {
			return change{}, invalid(key, reasonType)
		}
		if utf8.RuneCountInString(*e) > maxString {
			return change{}, invalid(key, reasonLong)
		}
		vals = append(vals, *e)
	}
	apply := func(n *Note) { *f(n) = cloneStrings(vals) }
	if len(vals) == 0 {
		apply = func(n *Note) { *f(n) = nil }
	}
	return change{key: key, apply: apply, strs: vals}, nil
}

// strictDecode は未知キー・型違いを拒否して v へ読む。
func strictDecode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func tooLong(ss ...string) bool {
	for _, s := range ss {
		if utf8.RuneCountInString(s) > maxString {
			return true
		}
	}
	return false
}

func parseReportTo(raw json.RawMessage) (change, error) {
	const key = "report_to"
	if isNull(raw) {
		return change{key: key, apply: func(n *Note) { n.ReportTo = nil }}, nil
	}
	var r ReportTo
	if err := strictDecode(raw, &r); err != nil {
		return change{}, invalid(key, reasonType)
	}
	switch r.Kind {
	case "agent":
		if r.Name == "" {
			return change{}, invalid(key, reasonBad)
		}
	case "email":
		if !strings.Contains(r.To, "@") {
			return change{}, invalid(key, reasonBad)
		}
	default:
		return change{}, invalid(key, reasonBad)
	}
	if tooLong(r.Name, r.To, r.Subject, r.ReplyToID) {
		return change{}, invalid(key, reasonLong)
	}
	strs := []string{r.Kind, r.Name, r.To, r.Subject, r.ReplyToID}
	return change{key: key, apply: func(n *Note) { c := r; n.ReportTo = &c }, strs: strs}, nil
}

func parseApproval(raw json.RawMessage) (change, error) {
	const key = "approval"
	if isNull(raw) {
		return change{key: key, apply: func(n *Note) { n.Approval = nil }}, nil
	}
	var a Approval
	if err := strictDecode(raw, &a); err != nil {
		return change{}, invalid(key, reasonType)
	}
	if a.Text == "" {
		return change{}, invalid(key, reasonEmpty)
	}
	if tooLong(a.Text, a.Via, a.At) {
		return change{}, invalid(key, reasonLong)
	}
	if a.At != "" {
		if _, err := time.Parse(time.RFC3339, a.At); err != nil {
			return change{}, invalid(key, reasonBad)
		}
	}
	strs := []string{a.Text, a.Via, a.At}
	return change{key: key, apply: func(n *Note) { c := a; n.Approval = &c }, strs: strs}, nil
}

// checkLogText は note_log の 1 行を検証する（空・改行・500 字超は invalid_args）。
func checkLogText(text string) error {
	if text == "" {
		return invalid("text", reasonEmpty)
	}
	if strings.ContainsAny(text, "\r\n") {
		return invalid("text", reasonBad)
	}
	if utf8.RuneCountInString(text) > maxLogText {
		return invalid("text", reasonLong)
	}
	return nil
}
