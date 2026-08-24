package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func (m *Manager) ListServers(ctx context.Context, includeDisabled bool) ([]Server, error) {
	query := m.db.WithContext(ctx).Model(&dbmodel.MCPServer{})
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	var rows []dbmodel.MCPServer
	if err := query.Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(rows))
	for _, row := range rows {
		out = append(out, serverFromRow(row))
	}
	return out, nil
}

func (m *Manager) GetServer(ctx context.Context, id int64) (Server, error) {
	var row dbmodel.MCPServer
	if err := m.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return Server{}, err
	}
	return serverFromRow(row), nil
}

func (m *Manager) CreateServer(ctx context.Context, input ServerWrite) (Server, error) {
	secret := input.OAuthClientSecret
	if secret != nil && strings.TrimSpace(*secret) != "" && m.box == nil {
		return Server{}, errors.New("secret encryption is not configured")
	}
	input.OAuthClientSecret = nil
	row, err := m.serverRow(0, input)
	if err != nil {
		return Server{}, err
	}
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if secret == nil || strings.TrimSpace(*secret) == "" {
			return nil
		}
		ciphertext, err := m.box.Encrypt(*secret, mcpServerSecretAAD(row.ID, row.Name))
		if err != nil {
			return err
		}
		row.OAuthClientSecretCiphertext = ciphertext
		return tx.Model(&row).Update("oauth_client_secret_ciphertext", ciphertext).Error
	})
	if err != nil {
		return Server{}, err
	}
	return serverFromRow(row), nil
}

func (m *Manager) UpdateServer(ctx context.Context, id int64, input ServerWrite) (Server, error) {
	var existing dbmodel.MCPServer
	if err := m.db.WithContext(ctx).First(&existing, id).Error; err != nil {
		return Server{}, err
	}
	row, err := m.serverRow(id, input)
	if err != nil {
		return Server{}, err
	}
	if input.OAuthClientSecret == nil {
		row.OAuthClientSecretCiphertext = existing.OAuthClientSecretCiphertext
	}
	if row.URL == existing.URL {
		row.OAuthIssuer = existing.OAuthIssuer
		row.OAuthResource = existing.OAuthResource
	}
	updates := map[string]any{
		"name": row.Name, "description": row.Description, "transport": row.Transport, "command": row.Command,
		"args_json": row.ArgsJSON, "url": row.URL, "oauth_enabled": row.OAuthEnabled,
		"oauth_authorization_endpoint": row.OAuthAuthorizationEndpoint, "oauth_token_endpoint": row.OAuthTokenEndpoint,
		"oauth_registration_endpoint": row.OAuthRegistrationEndpoint, "oauth_client_id": row.OAuthClientID,
		"oauth_issuer": row.OAuthIssuer, "oauth_resource": row.OAuthResource,
		"oauth_client_secret_ciphertext": row.OAuthClientSecretCiphertext, "oauth_scopes": row.OAuthScopes,
		"enabled": row.Enabled, "last_error": nil, "updated_at": time.Now(),
	}
	if err := m.db.WithContext(ctx).Model(&dbmodel.MCPServer{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return Server{}, err
	}
	m.closeServerSessions(id)
	return m.GetServer(ctx, id)
}

func (m *Manager) DeleteServer(ctx context.Context, id int64) error {
	result := m.db.WithContext(ctx).Delete(&dbmodel.MCPServer{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	m.closeServerSessions(id)
	return nil
}

func (m *Manager) serverRow(id int64, input ServerWrite) (dbmodel.MCPServer, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Transport = strings.ToLower(strings.TrimSpace(input.Transport))
	input.Command = strings.TrimSpace(input.Command)
	input.URL = strings.TrimSpace(input.URL)
	if input.Name == "" {
		return dbmodel.MCPServer{}, errors.New("MCP server name is required")
	}
	if input.Transport != TransportStdio && input.Transport != TransportHTTP {
		return dbmodel.MCPServer{}, errors.New("transport must be stdio or http")
	}
	if input.Transport == TransportStdio && input.Command == "" {
		return dbmodel.MCPServer{}, errors.New("stdio MCP server command is required")
	}
	if input.Transport == TransportHTTP {
		parsed, err := url.Parse(input.URL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return dbmodel.MCPServer{}, errors.New("HTTP MCP server URL must be an absolute http(s) URL")
		}
	}
	if input.OAuthEnabled && input.Transport != TransportHTTP {
		return dbmodel.MCPServer{}, errors.New("OAuth is supported only for HTTP MCP servers")
	}
	for label, raw := range map[string]string{
		"authorization endpoint": input.OAuthAuthorizationEndpoint,
		"token endpoint":         input.OAuthTokenEndpoint,
		"registration endpoint":  input.OAuthRegistrationEndpoint,
	} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return dbmodel.MCPServer{}, errors.New("OAuth " + label + " must be an absolute http(s) URL")
		}
	}
	args, _ := json.Marshal(input.Args)
	row := dbmodel.MCPServer{
		ID: id, Name: input.Name, Description: strings.TrimSpace(input.Description), Transport: input.Transport,
		Command: input.Command, ArgsJSON: datatypes.JSON(args), URL: input.URL, OAuthEnabled: input.OAuthEnabled,
		OAuthAuthorizationEndpoint: strings.TrimSpace(input.OAuthAuthorizationEndpoint), OAuthTokenEndpoint: strings.TrimSpace(input.OAuthTokenEndpoint),
		OAuthRegistrationEndpoint: strings.TrimSpace(input.OAuthRegistrationEndpoint), OAuthClientID: strings.TrimSpace(input.OAuthClientID),
		OAuthScopes: strings.TrimSpace(input.OAuthScopes), Enabled: input.Enabled,
	}
	if input.OAuthClientSecret != nil && strings.TrimSpace(*input.OAuthClientSecret) != "" {
		if m.box == nil {
			return dbmodel.MCPServer{}, errors.New("secret encryption is not configured")
		}
		ciphertext, err := m.box.Encrypt(*input.OAuthClientSecret, mcpServerSecretAAD(id, input.Name))
		if err != nil {
			return dbmodel.MCPServer{}, err
		}
		row.OAuthClientSecretCiphertext = ciphertext
	}
	return row, nil
}

func serverFromRow(row dbmodel.MCPServer) Server {
	var args []string
	_ = json.Unmarshal(row.ArgsJSON, &args)
	return Server{
		ID: row.ID, Name: row.Name, Description: row.Description, Transport: row.Transport, Command: row.Command, Args: args, URL: row.URL,
		OAuthEnabled: row.OAuthEnabled, OAuthAuthorizationEndpoint: row.OAuthAuthorizationEndpoint, OAuthTokenEndpoint: row.OAuthTokenEndpoint,
		OAuthRegistrationEndpoint: row.OAuthRegistrationEndpoint, OAuthClientID: row.OAuthClientID,
		OAuthClientSecretSet: row.OAuthClientSecretCiphertext != "", OAuthScopes: row.OAuthScopes, Enabled: row.Enabled,
		LastError: row.LastError, LastRefreshAt: row.LastRefreshAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (m *Manager) SetAgentCredential(ctx context.Context, agentID, serverID int64, input AgentCredentialWrite) (AgentConnection, error) {
	if m.box == nil {
		return AgentConnection{}, errors.New("secret encryption is not configured")
	}
	if _, err := m.GetServer(ctx, serverID); err != nil {
		return AgentConnection{}, err
	}
	environment, _ := json.Marshal(input.Environment)
	headers, _ := json.Marshal(input.Headers)
	envCipher, err := m.box.Encrypt(string(environment), credentialAAD(agentID, serverID, "environment"))
	if err != nil {
		return AgentConnection{}, err
	}
	headerCipher, err := m.box.Encrypt(string(headers), credentialAAD(agentID, serverID, "headers"))
	if err != nil {
		return AgentConnection{}, err
	}
	row := dbmodel.AgentMCPCredential{AgentID: agentID, ServerID: serverID, EnvironmentCiphertext: envCipher, HeadersCiphertext: headerCipher}
	err = m.db.WithContext(ctx).Where("agent_id = ? AND server_id = ?", agentID, serverID).
		Assign(map[string]any{"environment_ciphertext": envCipher, "headers_ciphertext": headerCipher, "updated_at": time.Now()}).FirstOrCreate(&row).Error
	if err != nil {
		return AgentConnection{}, err
	}
	m.closeAgentServerSessions(agentID, serverID)
	return m.AgentConnection(ctx, agentID, serverID)
}

func (m *Manager) AgentConnection(ctx context.Context, agentID, serverID int64) (AgentConnection, error) {
	var row dbmodel.AgentMCPCredential
	err := m.db.WithContext(ctx).Where("agent_id = ? AND server_id = ?", agentID, serverID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentConnection{ServerID: serverID}, nil
	}
	if err != nil {
		return AgentConnection{}, err
	}
	return AgentConnection{ServerID: serverID, CredentialSet: row.EnvironmentCiphertext != "" || row.HeadersCiphertext != "", OAuthConnected: row.AccessTokenCiphertext != "", ExpiresAt: row.ExpiresAt}, nil
}

func (m *Manager) credentials(ctx context.Context, agentID, serverID int64) (map[string]string, map[string]string, *dbmodel.AgentMCPCredential, error) {
	var row dbmodel.AgentMCPCredential
	err := m.db.WithContext(ctx).Where("agent_id = ? AND server_id = ?", agentID, serverID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return map[string]string{}, map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if m.box == nil {
		return nil, nil, nil, errors.New("secret encryption is not configured")
	}
	decode := func(value, kind string) (map[string]string, error) {
		out := map[string]string{}
		if value == "" {
			return out, nil
		}
		plain, err := m.box.Decrypt(value, credentialAAD(agentID, serverID, kind))
		if err != nil {
			return nil, err
		}
		return out, json.Unmarshal([]byte(plain), &out)
	}
	env, err := decode(row.EnvironmentCiphertext, "environment")
	if err != nil {
		return nil, nil, nil, err
	}
	headers, err := decode(row.HeadersCiphertext, "headers")
	return env, headers, &row, err
}

func credentialAAD(agentID, serverID int64, kind string) string {
	return "mcp:agent:" + jsonNumber(agentID) + ":server:" + jsonNumber(serverID) + ":" + kind
}

func mcpServerSecretAAD(id int64, name string) string {
	if id > 0 {
		return "mcp:server:" + jsonNumber(id) + ":client-secret"
	}
	return "mcp:server:new:" + strings.ToLower(strings.TrimSpace(name)) + ":client-secret"
}

func jsonNumber(value int64) string { return strconv.FormatInt(value, 10) }
