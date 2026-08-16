package sandbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RuntimeTarget struct {
	BaseURL       string
	ServerName    string
	CAPEM         string
	ClientCertPEM string
	ClientKeyPEM  string
	Token         string
}

type Runtime interface {
	Exec(context.Context, RuntimeTarget, ExecRequest) (Process, error)
	ListProcesses(context.Context, RuntimeTarget, *int64) ([]Process, error)
	GetProcess(context.Context, RuntimeTarget, string) (Process, error)
	WriteStdin(context.Context, RuntimeTarget, string, StdinRequest) (Process, error)
	Signal(context.Context, RuntimeTarget, string, string) (Process, error)
}

type HTTPRuntime struct {
	RequestTimeout time.Duration
}

func NewHTTPRuntime() *HTTPRuntime { return &HTTPRuntime{RequestTimeout: 30 * time.Second} }

func (r *HTTPRuntime) Exec(ctx context.Context, target RuntimeTarget, input ExecRequest) (Process, error) {
	var output Process
	err := r.request(ctx, target, http.MethodPost, "/v1/processes", input, &output, execHTTPTimeout(input))
	return output, err
}

func (r *HTTPRuntime) ListProcesses(ctx context.Context, target RuntimeTarget, sessionID *int64) ([]Process, error) {
	path := "/v1/processes"
	if sessionID != nil {
		path += "?session_id=" + strconv.FormatInt(*sessionID, 10)
	}
	var output struct {
		Processes []Process `json:"processes"`
	}
	err := r.request(ctx, target, http.MethodGet, path, nil, &output, r.timeout())
	return output.Processes, err
}

func (r *HTTPRuntime) GetProcess(ctx context.Context, target RuntimeTarget, id string) (Process, error) {
	var output Process
	err := r.request(ctx, target, http.MethodGet, "/v1/processes/"+url.PathEscape(id), nil, &output, r.timeout())
	return output, err
}

func (r *HTTPRuntime) WriteStdin(ctx context.Context, target RuntimeTarget, id string, input StdinRequest) (Process, error) {
	var output Process
	err := r.request(ctx, target, http.MethodPost, "/v1/processes/"+url.PathEscape(id)+"/stdin", input, &output, execHTTPTimeout(ExecRequest{YieldTimeMS: input.YieldTimeMS}))
	return output, err
}

func (r *HTTPRuntime) Signal(ctx context.Context, target RuntimeTarget, id string, signal string) (Process, error) {
	var output Process
	err := r.request(ctx, target, http.MethodPost, "/v1/processes/"+url.PathEscape(id)+"/signal", SignalRequest{Signal: signal}, &output, r.timeout())
	return output, err
}

func (r *HTTPRuntime) timeout() time.Duration {
	if r == nil || r.RequestTimeout <= 0 {
		return 30 * time.Second
	}
	return r.RequestTimeout
}

func execHTTPTimeout(input ExecRequest) time.Duration {
	yield := time.Duration(input.YieldTimeMS) * time.Millisecond
	if yield <= 0 {
		yield = 10 * time.Second
	}
	return yield + 30*time.Second
}

func (r *HTTPRuntime) request(ctx context.Context, target RuntimeTarget, method, path string, input any, output any, timeout time.Duration) error {
	client, err := runtimeHTTPClient(target, timeout)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(target.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+target.Token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox runtime request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &detail)
		if detail.Error == "" {
			detail.Error = strings.TrimSpace(string(data))
		}
		if detail.Error == "" {
			detail.Error = resp.Status
		}
		return fmt.Errorf("sandbox runtime: %s", detail.Error)
	}
	if output == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode sandbox runtime response: %w", err)
	}
	return nil
}

func runtimeHTTPClient(target RuntimeTarget, timeout time.Duration) (*http.Client, error) {
	if strings.TrimSpace(target.CAPEM) == "" || strings.TrimSpace(target.ClientCertPEM) == "" || strings.TrimSpace(target.ClientKeyPEM) == "" {
		return nil, errors.New("sandbox runtime TLS credentials are incomplete")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(target.CAPEM)) {
		return nil, errors.New("sandbox runtime CA certificate is invalid")
	}
	cert, err := tls.X509KeyPair([]byte(target.ClientCertPEM), []byte(target.ClientKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("sandbox runtime client certificate: %w", err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: target.ServerName,
	}}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
