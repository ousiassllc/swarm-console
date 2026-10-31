package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

// routes は API 仕様 §2 の順に判定する: 未知のパス（404）→ 認証（401）→ 窓口とスコープ（403）→ メソッド（405）→ 本文上限（413）。
func (s *Server) routes() http.Handler {
	endpoints := map[string]http.Handler{"/report": s.o.Report, "/ops": s.o.Ops}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		endpoint := r.URL.Path
		mcp, ok := endpoints[endpoint]
		if !ok {
			s.reject(w, http.StatusNotFound, apperr.NotFound, "", "", start)
			return
		}
		client, err := s.o.Auth.Authenticate(r.Header.Get("Authorization"))
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.reject(w, http.StatusUnauthorized, apperr.Unauthorized, endpoint, "", start)
			return
		}
		if err := auth.Allowed(client, endpoint); err != nil {
			s.reject(w, http.StatusForbidden, apperr.ForbiddenScope, endpoint, client.Name, start)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			s.reject(w, http.StatusMethodNotAllowed, apperr.MethodNotAllowed, endpoint, client.Name, start)
			return
		}
		body, err := readBody(w, r)
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge) || errors.Is(err, errTooLarge):
			s.reject(w, http.StatusRequestEntityTooLarge, apperr.PayloadTooLarge, endpoint, client.Name, start)
			return
		case err != nil:
			s.reject(w, http.StatusBadRequest, apperr.InvalidArgs, endpoint, client.Name, start)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		mcp.ServeHTTP(w, r.WithContext(auth.WithClient(r.Context(), client)))
	})
}

// readBody は本文を上限つきで読む。Content-Length が上限を超えていれば読まずに拒否し、
// 分からない（chunked）場合は MaxBytesReader で上限を超えた時点で拒否する。
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.ContentLength > MaxBodyBytes {
		return nil, errTooLarge
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
}

var errTooLarge = errors.New("server: 本文が上限を超えています")

// reject は API 仕様 §3.1 の固定形 {"error":{"code","message"}} で返す。details・ヒントは出さない。
func (s *Server) reject(w http.ResponseWriter, status int, code apperr.Code, endpoint, client string, start time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + string(code) + `","message":"` + apperr.Message(code) + `"}}`))
	s.auditHTTP(endpoint, client, status, code, time.Since(start))
}
