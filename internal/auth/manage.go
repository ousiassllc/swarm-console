package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// manageMu は Add / Revoke の読み → 書きをプロセス内で直列化する。
var manageMu sync.Mutex

// Add は新しいトークンを発行して tokens.toml に追記し、平文を返す。
// 平文は保存しない（返すだけ）。name は一意で、失効済みの名前も再利用できない。
func Add(path, name string, scope Scope, now time.Time) (string, error) {
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("名前は %s に合う必要があります", namePattern)
	}
	if !validScope(scope) {
		return "", fmt.Errorf("scope は %s か %s にしてください", ScopeReport, ScopeOps)
	}
	manageMu.Lock()
	defer manageMu.Unlock()
	recs, err := readRecords(path)
	if err != nil {
		return "", err
	}
	for _, r := range recs {
		if r.Name == name {
			return "", fmt.Errorf("名前 %s は既に使われています（失効済みの名前も再利用できません）", name)
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("乱数を取得できません: %w", err)
	}
	plaintext := "sc_" + base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(plaintext))
	recs = append(recs, record{
		Name:      name,
		Scope:     string(scope),
		Hash:      "sha256:" + hex.EncodeToString(sum[:]),
		CreatedAt: now.Format(time.RFC3339),
	})
	if err := writeRecords(path, recs); err != nil {
		return "", err
	}
	return plaintext, nil
}

// Revoke は name のトークンに revoked_at を付ける。
// 存在しない名前・既に失効済みの名前はエラー。
func Revoke(path, name string, now time.Time) error {
	manageMu.Lock()
	defer manageMu.Unlock()
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
