package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const authCookieName = "haro_web_session"

type authenticator struct {
	token        string
	cookieSecure bool
	loginLimiter *rate.Limiter
}

func newAuthenticator(token string, secure bool) *authenticator {
	return &authenticator{token: token, cookieSecure: secure, loginLimiter: rate.NewLimiter(rate.Every(12*time.Second), 5)}
}

func (a *authenticator) login(w http.ResponseWriter, r *http.Request) {
	if !a.loginLimiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(input.Token), []byte(a.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid access token")
		return
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name: authCookieName, Value: a.sign(expires), Path: "/", Expires: expires,
		MaxAge: int((7 * 24 * time.Hour).Seconds()), HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (a *authenticator) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (a *authenticator) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookieName)
		if err != nil || !a.valid(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "origin_rejected", "request origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *authenticator) sign(expires time.Time) string {
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	payload := strconv.FormatInt(expires.Unix(), 10) + "." + hex.EncodeToString(nonce)
	mac := hmac.New(sha256.New, []byte(a.token))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "." + hex.EncodeToString(mac.Sum(nil))))
}

func (a *authenticator) valid(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	parts := strings.Split(string(decoded), ".")
	if len(parts) != 3 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() >= expiry {
		return false
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(a.token))
	_, _ = mac.Write([]byte(payload))
	expected, err := hex.DecodeString(parts[2])
	return err == nil && hmac.Equal(expected, mac.Sum(nil))
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}
