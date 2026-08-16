package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "8.8.8.8", want: true},
		{ip: "1.1.1.1", want: true},
		{ip: "127.0.0.1"},
		{ip: "10.0.0.1"},
		{ip: "169.254.169.254"},
		{ip: "192.168.1.4"},
		{ip: "100.64.0.1"},
		{ip: "::1"},
		{ip: "fc00::1"},
		{ip: "2001:db8::1"},
	}
	for _, test := range tests {
		t.Run(test.ip, func(t *testing.T) {
			if got := isPublicIP(net.ParseIP(test.ip)); got != test.want {
				t.Fatalf("isPublicIP(%s) = %v, want %v", test.ip, got, test.want)
			}
		})
	}
}

func TestValidateAvatarURL(t *testing.T) {
	for _, raw := range []string{"file:///tmp/avatar.png", "https://user:secret@example.com/a.png", "//example.com/a.png", "http://"} {
		if _, err := validateAvatarURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
	if _, err := validateAvatarURL("https://example.com:8443/avatar.png"); err != nil {
		t.Fatalf("expected public URL with arbitrary port to pass syntax validation: %v", err)
	}
}

func TestValidateAvatarDataChecksSizeAndType(t *testing.T) {
	if image, err := validateAvatarData([]byte("\x89PNG\r\n\x1a\nfixture")); err != nil || image.MIMEType != "image/png" {
		t.Fatalf("expected PNG avatar to pass, got %#v, %v", image, err)
	}
	if _, err := validateAvatarData([]byte("not an image")); err == nil {
		t.Fatal("expected unsupported avatar data to fail")
	}
	if _, err := validateAvatarData(make([]byte, maxAvatarBytes+1)); err == nil {
		t.Fatal("expected oversized avatar data to fail")
	}
}

func TestAvatarDownloaderRejectsPrivateResolution(t *testing.T) {
	d := newAvatarDownloader()
	d.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dialed := false
	d.dial = func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}
	_, err := d.Fetch(context.Background(), "http://avatar.example:8080/avatar.png")
	if err == nil || !strings.Contains(err.Error(), "private or reserved") {
		t.Fatalf("expected private address error, got %v", err)
	}
	if dialed {
		t.Fatal("private address was dialed")
	}
}

func TestAvatarDownloaderFetchesPublicImageOnArbitraryPort(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()
	serverAddress := strings.TrimPrefix(server.URL, "http://")

	d := newAvatarDownloader()
	d.lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "avatar.example" {
			t.Fatalf("unexpected host %q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	d.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, serverAddress)
	}
	port := strings.Split(serverAddress, ":")[1]
	image, err := d.Fetch(context.Background(), "http://avatar.example:"+port+"/avatar.png")
	if err != nil {
		t.Fatalf("fetch avatar: %v", err)
	}
	if image.MIMEType != "image/png" || string(image.Data) != string(png) {
		t.Fatalf("unexpected image: %#v", image)
	}
}

func TestOwnProfileToolSchemaDoesNotExposeCrossAgentOrProviderFields(t *testing.T) {
	tool := (&updateOwnProfileTool{}).Parameters()
	properties := tool["properties"].(map[string]any)
	for _, forbidden := range []string{"agent_id", "base_url", "api_key", "prompt_format", "archived"} {
		if _, ok := properties[forbidden]; ok {
			t.Fatalf("tool schema unexpectedly exposes %q", forbidden)
		}
	}
}

func TestOwnProfileUpdateNormalizesOptionalAvatarFields(t *testing.T) {
	emptyURL := "  "
	removeAvatar := false
	update := ownProfileUpdate{AvatarURL: &emptyURL, RemoveAvatarImage: &removeAvatar}
	update.normalizePatch()
	if update.AvatarURL != nil || update.RemoveAvatarImage != nil {
		t.Fatalf("expected empty avatar fields to be omitted, got %#v", update)
	}
	if !update.empty() {
		t.Fatal("normalized no-op update should be empty")
	}
}

func TestOwnProfileUpdateRejectsAvatarURLWithIconMode(t *testing.T) {
	avatarURL := "https://example.com/avatar.png"
	iconMode := "icon"
	update := ownProfileUpdate{AvatarURL: &avatarURL, AvatarMode: &iconMode}
	update.normalizePatch()
	if err := update.validateAvatarPatch(); err == nil || !strings.Contains(err.Error(), "avatar_mode icon") {
		t.Fatalf("expected contradictory avatar patch to fail, got %v", err)
	}
}
