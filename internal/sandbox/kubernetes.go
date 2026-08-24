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
	SyncRuntimeHelper(context.Context, Profile) error
	Restart(context.Context, Profile) error
	SetOperatingMode(context.Context, Profile, string) error
	Delete(context.Context, Profile) error
	ResetWorkspace(context.Context, Profile) error
	Status(context.Context, Profile) (RuntimeDetails, error)
}

type KubernetesControlPlane struct {
	config config.SandboxConfig
	base   string
	token  string
	client *http.Client
}

var errSandboxNotFound = errors.New("Sandbox resource not found")

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
	path := k.sandboxesPath()
	var existing map[string]any
	status, err := k.request(ctx, http.MethodGet, path+"/"+url.PathEscape(profile.KubernetesName), nil, &existing)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		if len(credentials.ServerCertPEM) == 0 {
			return errors.New("runtime credentials are required when creating a Sandbox")
		}
		manifest := k.sandboxManifest(profile, credentials)
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
	runtimeGeneration := nestedString(existing, "spec", "podTemplate", "metadata", "annotations", "haro.yangkeao.io/runtime-generation")
	manifest := k.sandboxManifestWithRuntimeGeneration(profile, runtimeGeneration)
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

func (k *KubernetesControlPlane) SyncRuntimeHelper(ctx context.Context, profile Profile) error {
	path := k.sandboxesPath() + "/" + url.PathEscape(profile.KubernetesName)
	var existing map[string]any
	status, err := k.request(ctx, http.MethodGet, path, nil, &existing)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return errSandboxNotFound
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("get Sandbox returned HTTP %d", status)
	}
	initContainers := nestedSlice(existing, "spec", "podTemplate", "spec", "initContainers")
	found := false
	for _, value := range initContainers {
		container, _ := value.(map[string]any)
		if nestedString(container, "name") == "runtime-helper" {
			container["image"] = k.config.HelperImage
			found = true
		}
	}
	if !found {
		return errors.New("Sandbox runtime-helper init container is missing")
	}
	patch := map[string]any{"spec": map[string]any{"podTemplate": map[string]any{"spec": map[string]any{"initContainers": initContainers}}}}
	status, err = k.mergePatch(ctx, path, patch)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("update Sandbox runtime helper returned HTTP %d", status)
	}
	return nil
}

func (k *KubernetesControlPlane) Restart(ctx context.Context, profile Profile) error {
	sandboxObject, pods, err := k.sandboxPods(ctx, profile)
	if err != nil {
		return err
	}
	sandboxUID := nestedString(sandboxObject, "metadata", "uid")
	for _, pod := range pods {
		if !podOwnedBySandbox(pod, profile.KubernetesName, sandboxUID) {
			continue
		}
		name := nestedString(pod, "metadata", "name")
		if name == "" {
			continue
		}
		status, err := k.request(ctx, http.MethodDelete, k.corePath("pods")+"/"+url.PathEscape(name), map[string]any{"propagationPolicy": "Background"}, nil)
		if err != nil {
			return err
		}
		if status != http.StatusNotFound && (status < 200 || status >= 300) {
			return fmt.Errorf("delete Sandbox Pod returned HTTP %d", status)
		}
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

func (k *KubernetesControlPlane) Status(ctx context.Context, profile Profile) (RuntimeDetails, error) {
	now := time.Now().UTC()
	details := RuntimeDetails{State: "Starting", ObservedAt: now, Operation: profile.Operation, OperationStartedAt: profile.OperationStartedAt}
	sandboxObject, pods, err := k.sandboxPods(ctx, profile)
	if err != nil {
		if errors.Is(err, errSandboxNotFound) {
			details.State = "Not provisioned"
			return details, nil
		}
		return RuntimeDetails{}, err
	}
	sandboxUID := nestedString(sandboxObject, "metadata", "uid")

	var pod map[string]any
	for _, candidate := range pods {
		if podOwnedBySandbox(candidate, profile.KubernetesName, sandboxUID) {
			pod = candidate
			break
		}
	}
	if pod != nil {
		details.Pod = podRuntimeStatus(pod)
	}
	if profile.DesiredState == StateSuspended && details.Pod == nil {
		details.State = "Suspended"
	} else if details.Pod == nil {
		details.State = "Starting"
		details.Message = "Waiting for the Sandbox Pod to be created."
	} else if details.Pod.DeletionTimestamp != nil {
		details.State = "Restarting"
		if profile.Operation == OperationPause {
			details.State = "Pausing"
		}
		details.Message = "Waiting for the current Sandbox Pod to terminate."
	} else if details.Pod.WaitingReason != "" {
		details.Message = details.Pod.WaitingMessage
		if details.Message == "" {
			details.Message = details.Pod.WaitingReason
		}
		switch details.Pod.WaitingReason {
		case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError", "CreateContainerError":
			details.State = "Error"
		default:
			details.State = "Starting"
		}
	} else if details.Pod.Phase == "Failed" || details.Pod.Phase == "Succeeded" {
		details.State = "Error"
		details.Message = "Sandbox Pod stopped with phase " + details.Pod.Phase + "."
	} else if details.Pod.Ready {
		details.State = "Ready"
		details.Message = "Sandbox Pod is ready."
	} else {
		details.State = "Starting"
		details.Message = "Waiting for the Sandbox runtime to become ready."
	}
	for _, value := range nestedSlice(sandboxObject, "status", "conditions") {
		condition, _ := value.(map[string]any)
		conditionType := nestedString(condition, "type")
		conditionStatus := nestedString(condition, "status")
		message := nestedString(condition, "message")
		if conditionType == "Finished" && conditionStatus == "True" {
			details.State = "Error"
			if message != "" {
				details.Message = message
			}
		}
		if conditionType == "Ready" && conditionStatus != "True" && message != "" && details.State == "Starting" {
			details.Message = message
		}
	}

	if profile.Operation != "" {
		samePod := details.Pod != nil && profile.OperationPreviousPodUID != "" && details.Pod.UID == profile.OperationPreviousPodUID
		switch profile.Operation {
		case OperationApply:
			if samePod {
				details.State = "Applying"
				details.Message = "Applying configuration and terminating the previous Pod."
			}
		case OperationRestart:
			if samePod {
				details.State = "Restarting"
				details.Message = "Terminating the previous Sandbox Pod."
			}
		case OperationStart:
			if details.State != "Ready" {
				details.State = "Starting"
			}
		case OperationPause:
			if details.State != "Suspended" {
				details.State = "Pausing"
			}
		}
	}
	return details, nil
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
	runtimeDigest := sha256.Sum256(credentials.ServerCertPEM)
	return k.sandboxManifestWithRuntimeGeneration(profile, hex.EncodeToString(runtimeDigest[:8]))
}

func (k *KubernetesControlPlane) sandboxManifestWithRuntimeGeneration(profile Profile, runtimeGeneration string) map[string]any {
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
					"annotations": map[string]string{"haro.yangkeao.io/runtime-generation": runtimeGeneration},
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

func (k *KubernetesControlPlane) sandboxPods(ctx context.Context, profile Profile) (map[string]any, []map[string]any, error) {
	var sandboxObject map[string]any
	status, err := k.request(ctx, http.MethodGet, k.sandboxesPath()+"/"+url.PathEscape(profile.KubernetesName), nil, &sandboxObject)
	if err != nil {
		return nil, nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil, errSandboxNotFound
	}
	if status < 200 || status >= 300 {
		return nil, nil, fmt.Errorf("get Sandbox returned HTTP %d", status)
	}
	selector := "haro.yangkeao.io/sandbox-id=" + strconv.FormatInt(profile.ID, 10)
	var list struct {
		Items []map[string]any `json:"items"`
	}
	status, err = k.request(ctx, http.MethodGet, k.corePath("pods")+"?labelSelector="+url.QueryEscape(selector), nil, &list)
	if err != nil {
		return nil, nil, err
	}
	if status < 200 || status >= 300 {
		return nil, nil, fmt.Errorf("list Sandbox Pods returned HTTP %d", status)
	}
	return sandboxObject, list.Items, nil
}

func podOwnedBySandbox(pod map[string]any, name, uid string) bool {
	metadata, _ := pod["metadata"].(map[string]any)
	owners, _ := metadata["ownerReferences"].([]any)
	for _, value := range owners {
		owner, _ := value.(map[string]any)
		if nestedString(owner, "kind") == "Sandbox" && nestedString(owner, "name") == name && (uid == "" || nestedString(owner, "uid") == uid) {
			return true
		}
	}
	return false
}

func podRuntimeStatus(pod map[string]any) *PodRuntimeStatus {
	result := &PodRuntimeStatus{
		Name:  nestedString(pod, "metadata", "name"),
		UID:   nestedString(pod, "metadata", "uid"),
		Phase: nestedString(pod, "status", "phase"),
	}
	result.CreatedAt = parseKubernetesTime(nestedString(pod, "metadata", "creationTimestamp"))
	result.DeletionTimestamp = parseKubernetesTime(nestedString(pod, "metadata", "deletionTimestamp"))
	containers := nestedSlice(pod, "spec", "containers")
	for _, value := range containers {
		container, _ := value.(map[string]any)
		if nestedString(container, "name") == "sandbox" {
			result.Image = nestedString(container, "image")
			break
		}
	}
	statuses := nestedSlice(pod, "status", "containerStatuses")
	for _, value := range statuses {
		status, _ := value.(map[string]any)
		if nestedString(status, "name") != "sandbox" {
			continue
		}
		result.Ready, _ = status["ready"].(bool)
		if count, ok := status["restartCount"].(float64); ok {
			result.RestartCount = int32(count)
		}
		if imageID := nestedString(status, "imageID"); imageID != "" {
			result.Image = imageID
		}
		result.StartedAt = parseKubernetesTime(nestedString(status, "state", "running", "startedAt"))
		result.WaitingReason = nestedString(status, "state", "waiting", "reason")
		result.WaitingMessage = nestedString(status, "state", "waiting", "message")
		break
	}
	return result
}

func nestedString(value map[string]any, path ...string) string {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[part]
	}
	result, _ := current.(string)
	return result
}

func nestedSlice(value map[string]any, path ...string) []any {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	result, _ := current.([]any)
	return result
}

func parseKubernetesTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
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
