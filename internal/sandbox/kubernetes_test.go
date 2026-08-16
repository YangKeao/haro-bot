package sandbox

import (
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
