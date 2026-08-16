package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticatorLoginAndRequire(t *testing.T) {
	auth := newAuthenticator("test-access-token", false)
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"token":"test-access-token"}`))
	login.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	auth.login(recorder, login)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	result := recorder.Result()
	if len(result.Cookies()) != 1 {
		t.Fatalf("expected one auth cookie, got %d", len(result.Cookies()))
	}
	cookie := result.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security flags missing: %#v", cookie)
	}

	protected := auth.require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	request.AddCookie(cookie)
	protectedRecorder := httptest.NewRecorder()
	protected.ServeHTTP(protectedRecorder, request)
	if protectedRecorder.Code != http.StatusNoContent {
		t.Fatalf("protected status = %d", protectedRecorder.Code)
	}
}

func TestAuthenticatorRejectsBadTokenAndCrossOriginMutation(t *testing.T) {
	auth := newAuthenticator("test-access-token", false)
	recorder := httptest.NewRecorder()
	auth.login(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"token":"wrong"}`)))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", recorder.Code)
	}

	cookie := &http.Cookie{Name: authCookieName, Value: auth.sign(time.Now().Add(time.Hour))}
	request := httptest.NewRequest(http.MethodPost, "http://haro.test/api/v1/agents", bytes.NewBufferString(`{}`))
	request.Header.Set("Origin", "https://evil.test")
	request.AddCookie(cookie)
	protectedRecorder := httptest.NewRecorder()
	auth.require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(protectedRecorder, request)
	if protectedRecorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", protectedRecorder.Code)
	}
}
