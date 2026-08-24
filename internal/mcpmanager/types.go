package mcpmanager

import "time"

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

type Server struct {
	ID                         int64      `json:"id"`
	Name                       string     `json:"name"`
	Description                string     `json:"description"`
	Transport                  string     `json:"transport"`
	Command                    string     `json:"command,omitempty"`
	Args                       []string   `json:"args,omitempty"`
	URL                        string     `json:"url,omitempty"`
	OAuthEnabled               bool       `json:"oauth_enabled"`
	OAuthAuthorizationEndpoint string     `json:"oauth_authorization_endpoint,omitempty"`
	OAuthTokenEndpoint         string     `json:"oauth_token_endpoint,omitempty"`
	OAuthRegistrationEndpoint  string     `json:"oauth_registration_endpoint,omitempty"`
	OAuthClientID              string     `json:"oauth_client_id,omitempty"`
	OAuthClientSecretSet       bool       `json:"oauth_client_secret_set"`
	OAuthScopes                string     `json:"oauth_scopes,omitempty"`
	Enabled                    bool       `json:"enabled"`
	LastError                  *string    `json:"last_error,omitempty"`
	LastRefreshAt              *time.Time `json:"last_refresh_at,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type ServerWrite struct {
	Name                       string   `json:"name"`
	Description                string   `json:"description"`
	Transport                  string   `json:"transport"`
	Command                    string   `json:"command"`
	Args                       []string `json:"args"`
	URL                        string   `json:"url"`
	OAuthEnabled               bool     `json:"oauth_enabled"`
	OAuthAuthorizationEndpoint string   `json:"oauth_authorization_endpoint"`
	OAuthTokenEndpoint         string   `json:"oauth_token_endpoint"`
	OAuthRegistrationEndpoint  string   `json:"oauth_registration_endpoint"`
	OAuthClientID              string   `json:"oauth_client_id"`
	OAuthClientSecret          *string  `json:"oauth_client_secret,omitempty"`
	OAuthScopes                string   `json:"oauth_scopes"`
	Enabled                    bool     `json:"enabled"`
}

type AgentCredentialWrite struct {
	Environment map[string]string `json:"environment"`
	Headers     map[string]string `json:"headers"`
}

type AgentConnection struct {
	ServerID       int64      `json:"server_id"`
	CredentialSet  bool       `json:"credential_set"`
	OAuthConnected bool       `json:"oauth_connected"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

type CatalogTool struct {
	ServerID     int64          `json:"server_id"`
	ServerName   string         `json:"server"`
	RawName      string         `json:"raw_name"`
	CallableName string         `json:"callable_name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"-"`
	Builtin      bool           `json:"-"`
}
