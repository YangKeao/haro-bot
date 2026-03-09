package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Codex OAuth constants (from OpenAI Codex CLI)
const (
	CodexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	CodexTokenURL     = "https://auth.openai.com/oauth/token"
	CodexRedirectURI  = "http://localhost:1455/auth/callback"
	CodexScope        = "openid profile email offline_access"
	CodexOAuthProvider = "codex"
)

// CodexToken represents stored OAuth tokens
type CodexToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Email        string    `json:"email,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
}

// PKCEPair holds PKCE challenge and verifier
type PKCEPair struct {
	Challenge string
	Verifier  string
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	Enabled     bool `toml:"enabled"`
	AutoRefresh bool `toml:"auto_refresh"`
}

// CodexOAuthManager manages OAuth authentication for Codex
type CodexOAuthManager struct {
	config     OAuthConfig
	db         *gorm.DB
	token      *CodexToken
	mu         sync.RWMutex
	httpClient *http.Client
}

// NewCodexOAuthManager creates a new OAuth manager
func NewCodexOAuthManager(cfg OAuthConfig, database *gorm.DB) *CodexOAuthManager {
	mgr := &CodexOAuthManager{
		config:     cfg,
		db:         database,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	// Load existing token if available
	if database != nil {
		if err := mgr.loadToken(); err != nil {
			logging.L().Named("codex_oauth").Debug("no existing token loaded", zap.Error(err))
		}
	}

	return mgr
}

// generatePKCE creates a PKCE challenge/verifier pair
func generatePKCE() (*PKCEPair, error) {
	verifier := make([]byte, 32)
	if _, err := rand.Read(verifier); err != nil {
		return nil, fmt.Errorf("failed to generate verifier: %w", err)
	}

	verifierStr := base64.RawURLEncoding.EncodeToString(verifier)

	h := sha256.New()
	h.Write([]byte(verifierStr))
	challenge := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return &PKCEPair{
		Challenge: challenge,
		Verifier:  verifierStr,
	}, nil
}

// generateState creates a random state value
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GetAuthorizationURL generates the OAuth authorization URL
func (m *CodexOAuthManager) GetAuthorizationURL() (string, *PKCEPair, string, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return "", nil, "", err
	}

	state, err := generateState()
	if err != nil {
		return "", nil, "", err
	}

	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {CodexClientID},
		"redirect_uri":               {CodexRedirectURI},
		"scope":                      {CodexScope},
		"code_challenge":             {pkce.Challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"codex_cli_rs"},
	}

	authURL := CodexAuthorizeURL + "?" + params.Encode()
	return authURL, pkce, state, nil
}

// ExchangeCode exchanges authorization code for tokens
func (m *CodexOAuthManager) ExchangeCode(ctx context.Context, code string, verifier string) error {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {CodexClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {CodexRedirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange returned status %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	if result.AccessToken == "" || result.RefreshToken == "" {
		return fmt.Errorf("token response missing required fields")
	}

	token := &CodexToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}

	// Extract account info from JWT if present
	if result.IDToken != "" {
		m.extractAccountInfo(token, result.IDToken)
	}

	m.mu.Lock()
	m.token = token
	m.mu.Unlock()

	if err := m.saveToken(); err != nil {
		logging.L().Named("codex_oauth").Warn("failed to save token", zap.Error(err))
	}

	logging.L().Named("codex_oauth").Info("OAuth login successful",
		zap.String("email", token.Email),
		zap.Time("expires", token.ExpiresAt),
	)

	return nil
}

// RefreshToken refreshes the access token
func (m *CodexOAuthManager) RefreshToken(ctx context.Context) error {
	m.mu.RLock()
	token := m.token
	m.mu.RUnlock()

	if token == nil || token.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token.RefreshToken},
		"client_id":     {CodexClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh returned status %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	if result.AccessToken == "" {
		return fmt.Errorf("refresh response missing access token")
	}

	// Update token, keep old refresh token if new one not provided
	newRefresh := result.RefreshToken
	if newRefresh == "" {
		newRefresh = token.RefreshToken
	}

	m.mu.Lock()
	m.token = &CodexToken{
		AccessToken:  result.AccessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		Email:        token.Email,
		AccountID:    token.AccountID,
	}
	m.mu.Unlock()

	if err := m.saveToken(); err != nil {
		logging.L().Named("codex_oauth").Warn("failed to save refreshed token", zap.Error(err))
	}

	logging.L().Named("codex_oauth").Debug("token refreshed successfully",
		zap.Time("expires", m.token.ExpiresAt),
	)

	return nil
}

// GetAccessToken returns a valid access token, refreshing if necessary
func (m *CodexOAuthManager) GetAccessToken(ctx context.Context) (string, error) {
	m.mu.RLock()
	token := m.token
	m.mu.RUnlock()

	if token == nil {
		return "", fmt.Errorf("not authenticated - please run OAuth login first")
	}

	// Refresh if expired or will expire within 5 minutes
	if time.Until(token.ExpiresAt) < 5*time.Minute {
		if !m.config.AutoRefresh {
			return "", fmt.Errorf("token expired - auto refresh disabled")
		}
		if err := m.RefreshToken(ctx); err != nil {
			return "", fmt.Errorf("token refresh failed: %w", err)
		}
		m.mu.RLock()
		token = m.token
		m.mu.RUnlock()
	}

	return token.AccessToken, nil
}

// IsAuthenticated returns true if we have a valid token
func (m *CodexOAuthManager) IsAuthenticated() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.token != nil && m.token.AccessToken != ""
}

// loadToken loads token from database
func (m *CodexOAuthManager) loadToken() error {
	if m.db == nil {
		return fmt.Errorf("database not configured")
	}

	var oauthToken db.OAuthToken
	if err := m.db.Where("provider = ?", CodexOAuthProvider).First(&oauthToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("no token found for provider %s", CodexOAuthProvider)
		}
		return fmt.Errorf("failed to load token: %w", err)
	}

	token := &CodexToken{
		AccessToken:  oauthToken.AccessToken,
		RefreshToken: oauthToken.RefreshToken,
		Email:        oauthToken.Email,
		AccountID:    oauthToken.AccountID,
	}
	if oauthToken.ExpiresAt != nil {
		token.ExpiresAt = *oauthToken.ExpiresAt
	}

	m.mu.Lock()
	m.token = token
	m.mu.Unlock()

	return nil
}

// saveToken saves token to database
func (m *CodexOAuthManager) saveToken() error {
	if m.db == nil {
		return fmt.Errorf("database not configured")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.token == nil {
		return nil
	}

	oauthToken := db.OAuthToken{
		Provider:     CodexOAuthProvider,
		AccessToken:  m.token.AccessToken,
		RefreshToken: m.token.RefreshToken,
		Email:        m.token.Email,
		AccountID:    m.token.AccountID,
	}
	if !m.token.ExpiresAt.IsZero() {
		oauthToken.ExpiresAt = &m.token.ExpiresAt
	}

	// Use upsert (ON DUPLICATE KEY UPDATE)
	result := m.db.Where("provider = ?", CodexOAuthProvider).
		Assign(oauthToken).
		FirstOrCreate(&oauthToken)
	if result.Error != nil {
		return fmt.Errorf("failed to save token: %w", result.Error)
	}

	// If FirstOrCreate didn't update (created new), we need to update existing
	if oauthToken.ID == 0 || (result.RowsAffected == 0) {
		// Try explicit update
		if err := m.db.Model(&db.OAuthToken{}).
			Where("provider = ?", CodexOAuthProvider).
			Updates(map[string]interface{}{
				"access_token":  oauthToken.AccessToken,
				"refresh_token": oauthToken.RefreshToken,
				"expires_at":    oauthToken.ExpiresAt,
				"email":         oauthToken.Email,
				"account_id":    oauthToken.AccountID,
			}).Error; err != nil {
			return fmt.Errorf("failed to update token: %w", err)
		}
	}

	return nil
}

// extractAccountInfo extracts account info from JWT ID token
func (m *CodexOAuthManager) extractAccountInfo(token *CodexToken, idToken string) {
	// JWT format: header.payload.signature
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return
	}

	// Decode payload (base64url)
	payload := parts[1]
	// Add padding if needed
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try raw encoding
		decoded, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return
		}
	}

	var claims struct {
		Email string `json:"email"`
		Auth  struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}

	if err := json.Unmarshal(decoded, &claims); err != nil {
		return
	}

	token.Email = claims.Email
	token.AccountID = claims.Auth.ChatGPTAccountID
}

// DeleteToken removes the stored token from database
func (m *CodexOAuthManager) DeleteToken() error {
	if m.db == nil {
		return fmt.Errorf("database not configured")
	}

	m.mu.Lock()
	m.token = nil
	m.mu.Unlock()

	return m.db.Where("provider = ?", CodexOAuthProvider).Delete(&db.OAuthToken{}).Error
}
