package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ErrNameInUse は Add の name が既に使われている（失効済みを含む）ときのエラー。errors.Is で判定する。
var ErrNameInUse = errors.New("名前が既に使われています")

// manageMu は tokens.toml の読み → 書きをプロセス内で直列化する。
// 別プロセスとは直列化されない（flock は lockTokens が取る。同じプロセスの goroutine 同士は flock では直列化されないので、こちらも要る）。
var manageMu sync.Mutex

// Add は新しいトークンを発行して tokens.toml に追記し、平文を返す。
// 読み → 書きは tokens.toml.lock の flock でプロセス間も直列化する（取れなければ何も書かずにエラー）。
// 平文は保存しない（返すだけ）。name は一意で、失効済みの名前も再利用できない。
// sessions があればそのセッションだけを許可する（allowed_sessions）。無ければキーを書かず、config の sessions すべてを許可する。
func Add(path, name string, scope Scope, now time.Time, sessions ...string) (string, error) {
	if len(sessions) > 0 {
		if err := checkAllowedSessions(sessions); err != nil {
			return "", err
		}
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	if !validScope(scope) {
		return "", fmt.Errorf("scope は %s か %s にしてください", ScopeReport, ScopeOps)
	}
	unlock, err := lockTokens(path, true)
	if err != nil {
		return "", err
	}
	defer unlock()
	recs, err := readRecords(path)
	if err != nil {
		return "", err
	}
	for _, r := range recs {
		if r.Name == name {
			return "", fmt.Errorf("%w: %s（失効済みの名前も再利用できません）", ErrNameInUse, name)
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("乱数を取得できません: %w", err)
	}
	plaintext := "sc_" + base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(plaintext))
	recs = append(recs, record{
		Name:            name,
		Scope:           string(scope),
		Hash:            "sha256:" + hex.EncodeToString(sum[:]),
		CreatedAt:       now.Format(time.RFC3339),
		AllowedSessions: slices.Clone(sessions),
	})
	if err := writeRecords(path, recs); err != nil {
		return "", err
	}
	return plaintext, nil
}

// Revoke は name のトークンに revoked_at を付ける。
// 存在しない名前・既に失効済みの名前はエラー。Add と同じロックで直列化する。
func Revoke(path, name string, now time.Time) error {
	unlock, err := lockTokens(path, false)
	if err != nil {
		return err
	}
	defer unlock()
	recs, err := readRecords(path)
	if err != nil {
		return err
	}
	for i := range recs {
		if recs[i].Name != name {
			continue
		}
		if recs[i].RevokedAt != "" {
			return fmt.Errorf("名前 %s は既に失効済みです", name)
		}
		recs[i].RevokedAt = now.Format(time.RFC3339)
		return writeRecords(path, recs)
	}
	return fmt.Errorf("名前 %s のトークンはありません", name)
}

// ValidateName は token add の名前の形式を検査する（Add と同じ規則。`setup` が入力中に確かめるために使う）。
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("名前は %s に合う必要があります", namePattern)
	}
	return nil
}
