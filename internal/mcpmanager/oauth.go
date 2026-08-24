package mcpmanager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type OAuthStart struct {
	AuthorizationURL string `json:"authorization_url"`
}

type OAuthCallbackResult struct {
	AgentID  int64
	ServerID int64
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

var oauthHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (m *Manager) StartOAuth(ctx context.Context, agentID, serverID int64) (OAuthStart, error) {
	if m.box == nil {
		return OAuthStart{}, errors.New("secret encryption is not configured")
	}
	if m.publicURL == "" {
		return OAuthStart{}, errors.New("HARO_WEB_PUBLIC_URL is required for MCP OAuth")
	}
	var server dbmodel.MCPServer
	if err := m.db.WithContext(ctx).First(&server, serverID).Error; err != nil {
		return OAuthStart{}, err
	}
	if !server.OAuthEnabled || server.Transport != TransportHTTP {
		return OAuthStart{}, errors.New("OAuth is not enabled for this HTTP MCP server")
	}
	if err := m.ensureOAuthMetadata(ctx, &server); err != nil {
		return OAuthStart{}, err
	}
	redirectURI := m.publicURL + "/api/v1/mcp/oauth/callback"
	if server.OAuthClientID == "" {
		if err := m.registerOAuthClient(ctx, &server, redirectURI); err != nil {
			return OAuthStart{}, err
		}
	}
	state, err := randomURLToken(32)
	if err != nil {
		return OAuthStart{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return OAuthStart{}, err
	}
	ciphertext, err := m.box.Encrypt(verifier, "mcp:oauth-state:"+state)
	if err != nil {
		return OAuthStart{}, err
	}
	row := dbmodel.MCPOAuthState{State: state, AgentID: agentID, ServerID: serverID, CodeVerifierCiphertext: ciphertext, RedirectURI: redirectURI, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err := m.db.WithContext(ctx).Create(&row).Error; err != nil {
		return OAuthStart{}, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type": {"code"}, "client_id": {server.OAuthClientID}, "redirect_uri": {redirectURI}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}
	if scopes := strings.TrimSpace(server.OAuthScopes); scopes != "" {
		query.Set("scope", scopes)
	}
	if resource := strings.TrimSpace(server.OAuthResource); resource != "" {
		query.Set("resource", resource)
	}
	return OAuthStart{AuthorizationURL: server.OAuthAuthorizationEndpoint + "?" + query.Encode()}, nil
}

func (m *Manager) CompleteOAuth(ctx context.Context, state, code, callbackIssuer string) (OAuthCallbackResult, error) {
	state, code = strings.TrimSpace(state), strings.TrimSpace(code)
	if state == "" || code == "" {
		return OAuthCallbackResult{}, errors.New("OAuth callback is missing state or code")
	}
	var pending dbmodel.MCPOAuthState
	if err := m.db.WithContext(ctx).First(&pending, "state = ?", state).Error; err != nil {
		return OAuthCallbackResult{}, err
	}
	defer m.db.WithContext(context.Background()).Delete(&dbmodel.MCPOAuthState{}, "state = ?", state)
	if time.Now().After(pending.ExpiresAt) {
		return OAuthCallbackResult{}, errors.New("OAuth state expired")
	}
	verifier, err := m.box.Decrypt(pending.CodeVerifierCiphertext, "mcp:oauth-state:"+state)
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	var server dbmodel.MCPServer
	if err := m.db.WithContext(ctx).First(&server, pending.ServerID).Error; err != nil {
		return OAuthCallbackResult{}, err
	}
	if callbackIssuer = strings.TrimSpace(callbackIssuer); callbackIssuer != "" && server.OAuthIssuer != "" && callbackIssuer != server.OAuthIssuer {
		return OAuthCallbackResult{}, errors.New("OAuth callback issuer does not match the discovered authorization server")
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {pending.RedirectURI}, "client_id": {server.OAuthClientID}, "code_verifier": {verifier}}
	token, err := m.requestToken(ctx, server, values)
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	if err := m.saveToken(ctx, pending.AgentID, server.ID, token); err != nil {
		return OAuthCallbackResult{}, err
	}
	m.closeAgentServerSessions(pending.AgentID, server.ID)
	return OAuthCallbackResult{AgentID: pending.AgentID, ServerID: server.ID}, nil
}

func (m *Manager) ensureOAuthMetadata(ctx context.Context, server *dbmodel.MCPServer) error {
	resource := strings.TrimSpace(server.OAuthResource)
	issuer := strings.TrimSpace(server.OAuthIssuer)
	scopes := strings.TrimSpace(server.OAuthScopes)
	if resource == "" || issuer == "" {
		metadata, err := discoverProtectedResource(ctx, server.URL)
		if err != nil {
			return err
		}
		if resource == "" {
			resource = metadata.Resource
		}
		if issuer == "" {
			issuer = metadata.AuthorizationServers[0]
		}
		if scopes == "" && len(metadata.ScopesSupported) > 0 {
			scopes = strings.Join(metadata.ScopesSupported, " ")
		}
	}
	if server.OAuthAuthorizationEndpoint == "" || server.OAuthTokenEndpoint == "" {
		metadata, err := sdkauth.GetAuthServerMetadata(ctx, issuer, oauthHTTPClient)
		if err != nil {
			return err
		}
		if metadata == nil {
			return errors.New("OAuth authorization server metadata was not found")
		}
		server.OAuthAuthorizationEndpoint = metadata.AuthorizationEndpoint
		server.OAuthTokenEndpoint = metadata.TokenEndpoint
		if server.OAuthRegistrationEndpoint == "" {
			server.OAuthRegistrationEndpoint = metadata.RegistrationEndpoint
		}
	}
	if server.OAuthAuthorizationEndpoint == "" || server.OAuthTokenEndpoint == "" {
		return errors.New("OAuth authorization server metadata is incomplete")
	}
	server.OAuthIssuer, server.OAuthResource, server.OAuthScopes = issuer, resource, scopes
	updates := map[string]any{
		"oauth_authorization_endpoint": server.OAuthAuthorizationEndpoint,
		"oauth_token_endpoint":         server.OAuthTokenEndpoint,
		"oauth_registration_endpoint":  server.OAuthRegistrationEndpoint,
		"oauth_issuer":                 server.OAuthIssuer,
		"oauth_resource":               server.OAuthResource,
		"oauth_scopes":                 server.OAuthScopes,
	}
	return m.db.WithContext(ctx).Model(server).Updates(updates).Error
}

func discoverProtectedResource(ctx context.Context, resourceURL string) (*oauthex.ProtectedResourceMetadata, error) {
	parsed, err := url.Parse(resourceURL)
	if err != nil {
		return nil, err
	}
	metadataURL := ""
	if request, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil); err == nil {
		if response, err := oauthHTTPClient.Do(request); err == nil {
			challenges, _ := oauthex.ParseWWWAuthenticate(response.Header.Values("WWW-Authenticate"))
			response.Body.Close()
			for _, challenge := range challenges {
				if challenge.Scheme == "bearer" && challenge.Params["resource_metadata"] != "" {
					metadataURL = challenge.Params["resource_metadata"]
					break
				}
			}
		}
	}
	type candidate struct{ metadataURL, resourceURL string }
	candidates := make([]candidate, 0, 3)
	if metadataURL != "" {
		candidates = append(candidates, candidate{metadataURL, resourceURL})
	}
	pathMetadata := *parsed
	pathMetadata.RawQuery, pathMetadata.Fragment = "", ""
	pathMetadata.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(parsed.Path, "/")
	candidates = append(candidates, candidate{pathMetadata.String(), resourceURL})
	rootMetadata := *parsed
	rootResource := *parsed
	rootMetadata.RawQuery, rootMetadata.Fragment, rootMetadata.Path = "", "", "/.well-known/oauth-protected-resource"
	rootResource.RawQuery, rootResource.Fragment, rootResource.Path = "", "", ""
	candidates = append(candidates, candidate{rootMetadata.String(), rootResource.String()})
	for _, candidate := range candidates {
		metadata, err := oauthex.GetProtectedResourceMetadata(ctx, candidate.metadataURL, candidate.resourceURL, oauthHTTPClient)
		if err != nil || metadata == nil {
			continue
		}
		if len(metadata.AuthorizationServers) == 0 {
			return nil, errors.New("protected resource metadata has no authorization servers")
		}
		return metadata, nil
	}
	// Compatibility with MCP 2025-03-26: the resource server origin is also
	// the authorization server when RFC 9728 metadata is not available.
	return &oauthex.ProtectedResourceMetadata{Resource: resourceURL, AuthorizationServers: []string{rootResource.String()}}, nil
}

func (m *Manager) registerOAuthClient(ctx context.Context, server *dbmodel.MCPServer, redirectURI string) error {
	if server.OAuthRegistrationEndpoint == "" {
		return errors.New("OAuth client_id is required because the authorization server does not advertise dynamic registration")
	}
	payload := map[string]any{"client_name": "Haro Bot", "redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}
	encoded, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.OAuthRegistrationEndpoint, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OAuth dynamic registration returned %s", resp.Status)
	}
	var output struct{ ClientID, ClientSecret string }
	if err := json.Unmarshal(data, &output); err != nil {
		return err
	}
	if output.ClientID == "" {
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		output.ClientID, _ = raw["client_id"].(string)
		output.ClientSecret, _ = raw["client_secret"].(string)
	}
	if output.ClientID == "" {
		return errors.New("OAuth dynamic registration returned no client_id")
	}
	updates := map[string]any{"oauth_client_id": output.ClientID}
	server.OAuthClientID = output.ClientID
	if output.ClientSecret != "" {
		ciphertext, err := m.box.Encrypt(output.ClientSecret, mcpServerSecretAAD(server.ID, server.Name))
		if err != nil {
			return err
		}
		server.OAuthClientSecretCiphertext = ciphertext
		updates["oauth_client_secret_ciphertext"] = ciphertext
	}
	return m.db.WithContext(ctx).Model(server).Updates(updates).Error
}

func (m *Manager) requestToken(ctx context.Context, server dbmodel.MCPServer, values url.Values) (tokenResponse, error) {
	if resource := strings.TrimSpace(server.OAuthResource); resource != "" {
		values.Set("resource", resource)
	}
	if server.OAuthClientSecretCiphertext != "" {
		secret, err := m.box.Decrypt(server.OAuthClientSecretCiphertext, mcpServerSecretAAD(server.ID, server.Name))
		if err != nil {
			return tokenResponse{}, err
		}
		values.Set("client_secret", secret)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.OAuthTokenEndpoint, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, err
	}
	var token tokenResponse
	if err := json.Unmarshal(data, &token); err != nil {
		return tokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || token.AccessToken == "" {
		message := token.Description
		if message == "" {
			message = token.Error
		}
		if message == "" {
			message = resp.Status
		}
		return tokenResponse{}, errors.New("OAuth token exchange failed: " + message)
	}
	return token, nil
}

func (m *Manager) saveToken(ctx context.Context, agentID, serverID int64, token tokenResponse) error {
	access, err := m.box.Encrypt(token.AccessToken, credentialAAD(agentID, serverID, "access-token"))
	if err != nil {
		return err
	}
	refresh := ""
	if token.RefreshToken != "" {
		refresh, err = m.box.Encrypt(token.RefreshToken, credentialAAD(agentID, serverID, "refresh-token"))
		if err != nil {
			return err
		}
	}
	var expiresAt *time.Time
	if token.ExpiresIn > 0 {
		value := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		expiresAt = &value
	}
	row := dbmodel.AgentMCPCredential{AgentID: agentID, ServerID: serverID}
	return m.db.WithContext(ctx).Where("agent_id = ? AND server_id = ?", agentID, serverID).Assign(map[string]any{"access_token_ciphertext": access, "refresh_token_ciphertext": refresh, "token_type": token.TokenType, "expires_at": expiresAt, "updated_at": time.Now()}).FirstOrCreate(&row).Error
}

func (m *Manager) accessToken(ctx context.Context, server dbmodel.MCPServer, credential *dbmodel.AgentMCPCredential) (string, error) {
	if credential.ExpiresAt == nil || credential.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		return m.box.Decrypt(credential.AccessTokenCiphertext, credentialAAD(credential.AgentID, credential.ServerID, "access-token"))
	}
	if credential.RefreshTokenCiphertext == "" {
		return "", errors.New("MCP OAuth access token expired; reconnect this server")
	}
	refresh, err := m.box.Decrypt(credential.RefreshTokenCiphertext, credentialAAD(credential.AgentID, credential.ServerID, "refresh-token"))
	if err != nil {
		return "", err
	}
	token, err := m.requestToken(ctx, server, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {server.OAuthClientID}})
	if err != nil {
		return "", err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refresh
	}
	if err := m.saveToken(ctx, credential.AgentID, credential.ServerID, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
