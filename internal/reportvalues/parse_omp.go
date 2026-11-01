package reportvalues

import (
	"regexp"
	"strings"
)

var (
	// ansiRE は CSI 制御列（色・カーソル移動など）。出力を読む前に取り除く。
	ansiRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	// versionRE は出す版の形（先頭ゼロなしの X.Y.Z。数字だけ。FR-UPD-5 と同じ考え方）。
	versionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

const (
	markCurrent   = "Current version:"
	markAvailable = "New version available:"
	markUpToDate  = "Already up to date"
)

// ParseOmp は `omp update --check` の出力を読む（P-64）。
// 読めたら Omp（Status・Current・Latest）を、どちらの目印も無い・新しい版の形が合わないなら ok=false を返す。
// 出力の文面は結果に持たない（形に合う版だけ）。
func ParseOmp(out []byte) (Omp, bool) {
	var current, available string
	var upToDate, hasAvailable bool
	for _, line := range strings.Split(ansiRE.ReplaceAllString(string(out), ""), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, markCurrent):
			current = version(strings.TrimPrefix(line, markCurrent))
		case strings.HasPrefix(line, markAvailable):
			hasAvailable = true
			available = version(strings.TrimPrefix(line, markAvailable))
		case strings.Contains(line, markUpToDate):
			upToDate = true
		}
	}
	switch {
	case hasAvailable:
		if available == "" || available == current {
			return Omp{}, false
		}
		return Omp{Status: OmpUpdateAvailable, Current: current, Latest: available}, true
	case upToDate:
		return Omp{Status: OmpUpToDate, Current: current, Latest: current}, true
	}
	return Omp{}, false
}

// version は先頭の語が X.Y.Z の形ならそれを、合わなければ "" を返す。
func version(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 || !versionRE.MatchString(f[0]) {
		return ""
	}
	return f[0]
}
