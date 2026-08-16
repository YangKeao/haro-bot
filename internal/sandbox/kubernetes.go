package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
)

type ControlPlane interface {
	Apply(context.Context, Profile, RuntimeCredentials) error
	SetOperatingMode(context.Context, Profile, string) error
	Delete(context.Context, Profile) error
	ResetWorkspace(context.Context, Profile) error
	Status(context.Context, Profile) (string, *string, error)
}

type KubernetesControlPlane struct {
	config config.SandboxConfig
	base   string
	token  string
	client *http.Client
}

func NewKubernetesControlPlane(cfg config.SandboxConfig) (*KubernetesControlPlane, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.KubeAPIURL), "/")
	if base == "" {
		host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
		port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS"))
		if port == "" {
			port = "443"
		}
		if host == "" {
			return nil, errors.New("sandbox Kubernetes API URL is not configured and in-cluster discovery is unavailable")
		}
		base = "https://" + host + ":" + port
	}
	tokenBytes, err := os.ReadFile(cfg.KubeTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes service account token: %w", err)
	}
	roots := x509.NewCertPool()
	caBytes, err := os.ReadFile(cfg.KubeCAFile)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes CA: %w", err)
	}
	if !roots.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("Kubernetes CA file contains no certificates")
	}
	return &KubernetesControlPlane{
		config: cfg, base: base, token: strings.TrimSpace(string(tokenBytes)),
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}},
	}, nil
}

func (k *KubernetesControlPlane) Apply(ctx context.Context, profile Profile, credentials RuntimeCredentials) error {
	if len(credentials.ServerCertPEM) > 0 {
		if err := k.ensureRuntimeSecret(ctx, profile, credentials); err != nil {
			return err
		}
	}
	manifest := k.sandboxManifest(profile, credentials)
	path := k.sandboxesPath()
	var existing map[string]any
	status, err := k.request(ctx, http.MethodGet, path+"/"+url.PathEscape(profile.KubernetesName), nil, &existing)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		status, err = k.request(ctx, http.MethodPost, path, manifest, nil)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("create Sandbox returned HTTP %d", status)
		}
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("get Sandbox returned HTTP %d", status)
	}
	patch := map[string]any{"spec": manifest["spec"]}
	status, err = k.mergePatch(ctx, path+"/"+url.PathEscape(profile.KubernetesName), patch)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("update Sandbox returned HTTP %d", status)
	}
	return nil
}

func (k *KubernetesControlPlane) SetOperatingMode(ctx context.Context, profile Profile, state string) error {
	status, err := k.mergePatch(ctx, k.sandboxesPath()+"/"+url.PathEscape(profile.KubernetesName), map[string]any{"spec": map[string]any{"operatingMode": state}})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("update Sandbox operating mode returned HTTP %d", status)
	}
	return nil
}

func (k *KubernetesControlPlane) Delete(ctx context.Context, profile Profile) error {
	path := k.sandboxesPath() + "/" + url.PathEscape(profile.KubernetesName)
	status, err := k.request(ctx, http.MethodDelete, path, map[string]any{"propagationPolicy": "Foreground"}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNotFound && (status < 200 || status >= 300) {
		return fmt.Errorf("delete Sandbox returned HTTP %d", status)
	}
	secretPath := k.corePath("secrets") + "/" + url.PathEscape(runtimeSecretName(profile.ID))
	_, _ = k.request(ctx, http.MethodDelete, secretPath, map[string]any{}, nil)
	return nil
}

func (k *KubernetesControlPlane) ResetWorkspace(ctx context.Context, profile Profile) error {
	if profile.DesiredState != StateSuspended {
		return errors.New("sandbox must be suspended before resetting its workspace")
	}
	pvcName := "workspace-" + profile.KubernetesName
	status, err := k.request(ctx, http.MethodDelete, k.corePath("persistentvolumeclaims")+"/"+url.PathEscape(pvcName), map[string]any{}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNotFound && (status < 200 || status >= 300) {
		return fmt.Errorf("delete workspace PVC returned HTTP %d", status)
	}
	return nil
}

func (k *KubernetesControlPlane) Status(ctx context.Context, profile Profile) (string, *string, error) {
	var object struct {
		Status struct {
			Conditions []struct {
				Type, Status, Reason, Message string
			} `json:"conditions"`
		} `json:"status"`
	}
	status, err := k.request(ctx, http.MethodGet, k.sandboxesPath()+"/"+url.PathEscape(profile.KubernetesName), nil, &object)
	if err != nil {
		return "", nil, err
	}
	if status == http.StatusNotFound {
		return "Not provisioned", nil, nil
	}
	if status < 200 || status >= 300 {
		return "Error", stringPtr(fmt.Sprintf("Kubernetes API returned HTTP %d", status)), nil
	}
	state := "Starting"
	var last *string
	for _, condition := range object.Status.Conditions {
		switch condition.Type {
		case "Ready":
			if condition.Status == "True" {
				state = "Ready"
			} else if condition.Message != "" {
				last = stringPtr(condition.Message)
			}
		case "Suspended":
			if condition.Status == "True" {
				state = "Suspended"
			}
		case "Finished":
			if condition.Status == "True" {
				state = condition.Reason
				last = stringPtr(condition.Message)
			}
		}
	}
	return state, last, nil
}

func (k *KubernetesControlPlane) ensureRuntimeSecret(ctx context.Context, profile Profile, credentials RuntimeCredentials) error {
	name := runtimeSecretName(profile.ID)
	path := k.corePath("secrets")
	var current map[string]any
	status, err := k.request(ctx, http.MethodGet, path+"/"+url.PathEscape(name), nil, &current)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		if len(credentials.ServerCertPEM) == 0 || len(credentials.ServerKeyPEM) == 0 || credentials.Token == "" {
			return nil
		}
		encode := func(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
		patch := map[string]any{"data": map[string]string{
			"ca.crt": encode(credentials.CAPEM), "tls.crt": encode(credentials.ServerCertPEM), "tls.key": encode(credentials.ServerKeyPEM), "token": encode([]byte(credentials.Token)),
		}}
		status, err = k.mergePatch(ctx, path+"/"+url.PathEscape(name), patch)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("update runtime Secret returned HTTP %d", status)
		}
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("get runtime Secret returned HTTP %d", status)
	}
	encode := func(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
	secret := map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": name, "labels": map[string]string{"app.kubernetes.io/managed-by": "haro-bot", "haro.yangkeao.io/sandbox-id": strconv.FormatInt(profile.ID, 10)}},
		"type":     "Opaque", "data": map[string]string{
			"ca.crt": encode(credentials.CAPEM), "tls.crt": encode(credentials.ServerCertPEM), "tls.key": encode(credentials.ServerKeyPEM), "token": encode([]byte(credentials.Token)),
		},
	}
	status, err = k.request(ctx, http.MethodPost, path, secret, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create runtime Secret returned HTTP %d", status)
	}
	return nil
}

func (k *KubernetesControlPlane) sandboxManifest(profile Profile, credentials RuntimeCredentials) map[string]any {
	workspaceClaim := map[string]any{
		"metadata": map[string]any{"name": "workspace"},
		"spec": map[string]any{
			"accessModes": []string{"ReadWriteOnce"},
			"resources":   map[string]any{"requests": map[string]string{"storage": fmt.Sprintf("%dMi", profile.WorkspaceStorageMiB)}},
		},
	}
	if k.config.StorageClass != "" {
		workspaceClaim["spec"].(map[string]any)["storageClassName"] = k.config.StorageClass
	}
	cpuRequest := profile.CPULimitMillis / 4
	if cpuRequest < 100 {
		cpuRequest = 100
	}
	memoryRequest := profile.MemoryLimitMiB / 2
	if memoryRequest < 128 {
		memoryRequest = 128
	}
	labels := map[string]string{"app.kubernetes.io/name": "haro-sandbox", "app.kubernetes.io/managed-by": "haro-bot", "haro.yangkeao.io/sandbox-id": strconv.FormatInt(profile.ID, 10)}
	runtimeDigest := sha256.Sum256(credentials.ServerCertPEM)
	falseValue := false
	return map[string]any{
		"apiVersion": "agents.x-k8s.io/v1beta1", "kind": "Sandbox",
		"metadata": map[string]any{"name": profile.KubernetesName, "labels": labels},
		"spec": map[string]any{
			"operatingMode": profile.DesiredState, "service": true,
			"volumeClaimTemplates": []any{workspaceClaim},
			"podTemplate": map[string]any{
				"metadata": map[string]any{
					"labels":      labels,
					"annotations": map[string]string{"haro.yangkeao.io/runtime-generation": hex.EncodeToString(runtimeDigest[:8])},
				},
				"spec": map[string]any{
					"runtimeClassName": k.config.RuntimeClass, "automountServiceAccountToken": falseValue, "terminationGracePeriodSeconds": 10,
					"initContainers": []any{map[string]any{
						"name": "runtime-helper", "image": k.config.HelperImage, "imagePullPolicy": "IfNotPresent",
						"command":         []string{"/bin/sh", "-c", "cp /usr/local/bin/haro-sandboxd /runtime/haro-sandboxd && chmod 0555 /runtime/haro-sandboxd"},
						"securityContext": map[string]any{"runAsUser": 0, "runAsNonRoot": false, "allowPrivilegeEscalation": false, "capabilities": map[string]any{"drop": []string{"ALL"}}},
						"volumeMounts":    []any{map[string]any{"name": "runtime-bin", "mountPath": "/runtime"}},
					}},
					"containers": []any{map[string]any{
						"name": "sandbox", "image": profile.Image, "imagePullPolicy": "IfNotPresent", "workingDir": "/workspace",
						"command": []string{"/runtime/haro-sandboxd"},
						"args":    []string{"-listen", fmt.Sprintf(":%d", k.config.RuntimePort), "-workspace", "/workspace", "-tls-cert", "/runtime-tls/tls.crt", "-tls-key", "/runtime-tls/tls.key", "-client-ca", "/runtime-tls/ca.crt", "-token-file", "/runtime-tls/token"},
						"env":     []any{map[string]any{"name": "HOME", "value": "/workspace/home"}, map[string]any{"name": "LANG", "value": "C.UTF-8"}},
						"ports":   []any{map[string]any{"name": "runtime", "containerPort": k.config.RuntimePort, "protocol": "TCP"}},
						"resources": map[string]any{
							"requests": map[string]string{"cpu": fmt.Sprintf("%dm", cpuRequest), "memory": fmt.Sprintf("%dMi", memoryRequest)},
							"limits":   map[string]string{"cpu": fmt.Sprintf("%dm", profile.CPULimitMillis), "memory": fmt.Sprintf("%dMi", profile.MemoryLimitMiB), "ephemeral-storage": fmt.Sprintf("%dMi", profile.EphemeralStorageMiB)},
						},
						"securityContext": map[string]any{
							"runAsUser": 0, "runAsNonRoot": false, "allowPrivilegeEscalation": false,
							"seccompProfile": map[string]string{"type": "RuntimeDefault"},
							"capabilities":   map[string]any{"drop": []string{"ALL"}, "add": []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "SETGID", "SETUID", "SETFCAP"}},
						},
						"readinessProbe": map[string]any{"tcpSocket": map[string]any{"port": "runtime"}, "initialDelaySeconds": 2, "periodSeconds": 5},
						"livenessProbe":  map[string]any{"tcpSocket": map[string]any{"port": "runtime"}, "initialDelaySeconds": 10, "periodSeconds": 10},
						"volumeMounts": []any{
							map[string]any{"name": "workspace", "mountPath": "/workspace"},
							map[string]any{"name": "runtime-bin", "mountPath": "/runtime", "readOnly": true},
							map[string]any{"name": "runtime-tls", "mountPath": "/runtime-tls", "readOnly": true},
						},
					}},
					"volumes": []any{
						map[string]any{"name": "runtime-bin", "emptyDir": map[string]any{}},
						map[string]any{"name": "runtime-tls", "secret": map[string]any{"secretName": runtimeSecretName(profile.ID), "defaultMode": 0400}},
					},
				},
			},
		},
	}
}

func (k *KubernetesControlPlane) sandboxesPath() string {
	return "/apis/agents.x-k8s.io/v1beta1/namespaces/" + url.PathEscape(k.config.Namespace) + "/sandboxes"
}

func (k *KubernetesControlPlane) corePath(resource string) string {
	return "/api/v1/namespaces/" + url.PathEscape(k.config.Namespace) + "/" + resource
}

func (k *KubernetesControlPlane) mergePatch(ctx context.Context, path string, patch any) (int, error) {
	return k.requestWithContentType(ctx, http.MethodPatch, path, patch, nil, "application/merge-patch+json")
}

func (k *KubernetesControlPlane) request(ctx context.Context, method, path string, input any, output any) (int, error) {
	return k.requestWithContentType(ctx, method, path, input, output, "application/json")
}

func (k *KubernetesControlPlane) requestWithContentType(ctx context.Context, method, path string, input any, output any, contentType string) (int, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.base+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.token)
	if input != nil {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Kubernetes API request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return resp.StatusCode, err
		}
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		var statusObject struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &statusObject)
		if statusObject.Message == "" {
			statusObject.Message = strings.TrimSpace(string(data))
		}
		return resp.StatusCode, fmt.Errorf("Kubernetes API %s %s: %s", method, path, statusObject.Message)
	}
	return resp.StatusCode, nil
}

func runtimeSecretName(id int64) string { return "haro-runtime-" + strconv.FormatInt(id, 10) }

func stringPtr(value string) *string { return &value }
