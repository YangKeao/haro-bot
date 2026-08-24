package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/config"
)

func TestSandboxManifestUsesIsolatedRootRuntime(t *testing.T) {
	control := &KubernetesControlPlane{config: config.SandboxConfig{RuntimeClass: "gvisor", HelperImage: "helper:test", RuntimePort: 8888}}
	manifest := control.sandboxManifest(Profile{ID: 9, KubernetesName: "haro-test", Image: "custom:test", CPULimitMillis: 2000, MemoryLimitMiB: 2048, EphemeralStorageMiB: 1024, WorkspaceStorageMiB: 4096, DesiredState: StateRunning}, RuntimeCredentials{ServerCertPEM: []byte("certificate")})
	spec := manifest["spec"].(map[string]any)
	if spec["operatingMode"] != StateRunning || spec["service"] != true {
		t.Fatalf("unexpected sandbox spec: %#v", spec)
	}
	podTemplate := spec["podTemplate"].(map[string]any)
	annotations := podTemplate["metadata"].(map[string]any)["annotations"].(map[string]string)
	if annotations["haro.yangkeao.io/runtime-generation"] == "" {
		t.Fatal("runtime generation annotation is missing")
	}
	podSpec := podTemplate["spec"].(map[string]any)
	if podSpec["runtimeClassName"] != "gvisor" || podSpec["automountServiceAccountToken"] != false {
		t.Fatalf("unsafe pod defaults: %#v", podSpec)
	}
	container := podSpec["containers"].([]any)[0].(map[string]any)
	if container["image"] != "custom:test" || container["command"].([]string)[0] != "/runtime/haro-sandboxd" {
		t.Fatalf("runtime was not injected: %#v", container)
	}
	security := container["securityContext"].(map[string]any)
	if security["runAsUser"] != 0 || security["allowPrivilegeEscalation"] != false {
		t.Fatalf("unexpected security context: %#v", security)
	}
}

func TestApplyPreservesRuntimeGenerationAndRestartDeletesOwnedPod(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/sandboxes/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"uid": "sandbox-uid"},
				"spec":     map[string]any{"podTemplate": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"haro.yangkeao.io/runtime-generation": "stable-generation"}}}},
			})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/sandboxes/"):
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
			if got := nestedString(patch, "spec", "podTemplate", "metadata", "annotations", "haro.yangkeao.io/runtime-generation"); got != "stable-generation" {
				t.Fatalf("runtime generation = %q", got)
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pods"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
				map[string]any{"metadata": map[string]any{
					"name": "owned-pod", "uid": "pod-uid",
					"ownerReferences": []any{map[string]any{"kind": "Sandbox", "name": "sandbox-name", "uid": "sandbox-uid"}},
				}},
			}})
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/pods/owned-pod"):
			deleted = true
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(server.Close)
	control := &KubernetesControlPlane{config: config.SandboxConfig{Namespace: "test"}, base: server.URL, client: server.Client()}
	profile := Profile{ID: 1, KubernetesName: "sandbox-name", Image: "image:test", CPULimitMillis: 1000, MemoryLimitMiB: 512, EphemeralStorageMiB: 512, WorkspaceStorageMiB: 1024, DesiredState: StateRunning}
	if err := control.Apply(context.Background(), profile, RuntimeCredentials{}); err != nil {
		t.Fatal(err)
	}
	if err := control.Restart(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("owned Sandbox Pod was not deleted")
	}
}

func TestRestartIgnoresPodWithWrongOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Fatal("unowned Pod must not be deleted")
		}
		if strings.Contains(r.URL.Path, "/sandboxes/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"uid": "sandbox-uid"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{"metadata": map[string]any{
				"name": "other", "ownerReferences": []any{map[string]any{"kind": "Sandbox", "name": "other", "uid": "other-uid"}},
			}},
		}})
	}))
	t.Cleanup(server.Close)
	control := &KubernetesControlPlane{config: config.SandboxConfig{Namespace: "test"}, base: server.URL, client: server.Client()}
	if err := control.Restart(context.Background(), Profile{ID: 1, KubernetesName: "sandbox-name"}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncRuntimeHelperPreservesSandboxImageAndInitContainerConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"spec": map[string]any{"podTemplate": map[string]any{"spec": map[string]any{
				"initContainers": []any{map[string]any{"name": "runtime-helper", "image": "helper:old", "command": []any{"copy-runtime"}}},
				"containers":     []any{map[string]any{"name": "sandbox", "image": "user-image:pinned"}},
			}}}})
		case http.MethodPatch:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
			initContainers := nestedSlice(patch, "spec", "podTemplate", "spec", "initContainers")
			if len(initContainers) != 1 {
				t.Fatalf("unexpected init container patch: %#v", patch)
			}
			helper := initContainers[0].(map[string]any)
			if nestedString(helper, "image") != "helper:new" || len(nestedSlice(helper, "command")) != 1 {
				t.Fatalf("runtime helper configuration was not preserved: %#v", helper)
			}
			if nestedSlice(patch, "spec", "podTemplate", "spec", "containers") != nil {
				t.Fatalf("syncing the helper changed the pinned Sandbox image: %#v", patch)
			}
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(server.Close)
	control := &KubernetesControlPlane{config: config.SandboxConfig{Namespace: "test", HelperImage: "helper:new"}, base: server.URL, client: server.Client()}
	if err := control.SyncRuntimeHelper(context.Background(), Profile{KubernetesName: "sandbox-name"}); err != nil {
		t.Fatal(err)
	}
}

func TestStatusUsesPodLifecycleInsteadOfStaleSandboxReadyCondition(t *testing.T) {
	podUID := "old-pod"
	terminating := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/sandboxes/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"uid": "sandbox-uid"},
				"status":   map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True", "message": "stale Ready"}}},
			})
			return
		}
		metadata := map[string]any{
			"name": "sandbox-name", "uid": podUID, "creationTimestamp": "2026-08-24T00:00:00Z",
			"ownerReferences": []any{map[string]any{"kind": "Sandbox", "name": "sandbox-name", "uid": "sandbox-uid"}},
		}
		if terminating {
			metadata["deletionTimestamp"] = "2026-08-24T00:01:00Z"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{
			"metadata": metadata,
			"spec":     map[string]any{"containers": []any{map[string]any{"name": "sandbox", "image": "image:test"}}},
			"status":   map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "sandbox", "ready": !terminating, "restartCount": 0.0, "imageID": "image@test", "state": map[string]any{"running": map[string]any{"startedAt": "2026-08-24T00:00:05Z"}}}}},
		}}})
	}))
	t.Cleanup(server.Close)
	control := &KubernetesControlPlane{config: config.SandboxConfig{Namespace: "test"}, base: server.URL, client: server.Client()}
	profile := Profile{ID: 1, KubernetesName: "sandbox-name", DesiredState: StateRunning, Operation: OperationRestart, OperationPreviousPodUID: "old-pod"}
	details, err := control.Status(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if details.State != "Restarting" {
		t.Fatalf("state = %q, want Restarting", details.State)
	}

	podUID, terminating = "new-pod", false
	details, err = control.Status(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if details.State != "Ready" || details.Pod == nil || details.Pod.UID != "new-pod" {
		t.Fatalf("unexpected replacement Pod status: %#v", details)
	}
}
