package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/skillbundle"
)

func TestManagerReadsOnlyCurrentPackageResources(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	contents := "---\nname: demo\ndescription: demo\n---\nComplete instructions\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, root, err := skillbundle.Snapshot(base, source)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{baseDir: base, skills: map[string]Metadata{"demo": {Name: "demo", Hash: manifest.Hash, Dir: root}}}

	_, data, err := manager.ReadResource("demo", manifest.Hash, "")
	if err != nil || string(data) != contents {
		t.Fatalf("default resource = %q, err %v", data, err)
	}
	_, data, err = manager.ReadResource("demo", manifest.Hash, "references/guide.md")
	if err != nil || string(data) != "guide" {
		t.Fatalf("reference = %q, err %v", data, err)
	}
	for _, resource := range []string{"../secret", "/etc/passwd", `references\guide.md`} {
		if _, _, err := manager.ReadResource("demo", manifest.Hash, resource); err == nil {
			t.Fatalf("resource traversal %q was accepted", resource)
		}
	}
	if _, _, err := manager.ReadResource("demo", strings.Repeat("0", 64), "SKILL.md"); err == nil {
		t.Fatal("stale package hash was accepted")
	}
}

func TestDuplicateSkillNamesFailClosed(t *testing.T) {
	merged := make(map[string]Metadata)
	conflicts := make(map[string]struct{})
	mergeMetadata(merged, conflicts, Metadata{Name: "demo", Hash: strings.Repeat("a", 64)})
	mergeMetadata(merged, conflicts, Metadata{Name: "demo", Hash: strings.Repeat("b", 64)})
	mergeMetadata(merged, conflicts, Metadata{Name: "demo", Hash: strings.Repeat("c", 64)})
	if _, ok := merged["demo"]; ok {
		t.Fatal("duplicate skill name remained available")
	}
}
