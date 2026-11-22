package auth

import "slices"

// Info は tokens.toml のトークン 1 件の一覧用の情報。ハッシュ・平文は含まない。
type Info struct {
	Name  string
	Scope Scope
	// Sessions は許可セッション。nil は config の sessions すべて。
	Sessions  []string
	CreatedAt string
	// RevokedAt は失効日時。空なら有効。
	RevokedAt string
}

// List は path の tokens.toml にあるトークンを、書かれた順（発行順）で返す。ファイルが無ければ空。
// 検証は Open と同じ（権限が 600 でない・不正な内容はエラー）。ハッシュは返さない。
// 読むだけなので tokens.toml.lock は開かない・作らない（書き手は一時ファイル → rename なので、
// 読み手は常に旧内容か新内容の全体を見る。書けない置き場・別ユーザー（sudo）でも読める）。
// プロセス内の書き手とは manageMu で順に並ぶ。
func List(path string) ([]Info, error) {
	manageMu.Lock()
	defer manageMu.Unlock()
	recs, err := readRecords(path)
	if err != nil {
		return nil, err
	}
	out := make([]Info, len(recs))
	for i, r := range recs {
		out[i] = Info{
			Name: r.Name, Scope: Scope(r.Scope), Sessions: slices.Clone(r.AllowedSessions),
			CreatedAt: r.CreatedAt, RevokedAt: r.RevokedAt,
		}
	}
	return out, nil
}
