package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotHashesCompleteSkillAndExecutableMode(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\nRun the script.\n", 0o644)
	writeTestFile(t, filepath.Join(source, "scripts", "run.sh"), "#!/bin/sh\necho one\n", 0o755)
	writeTestFile(t, filepath.Join(source, ".git", "config"), "ignored", 0o644)

	base := t.TempDir()
	first, root, err := Snapshot(base, source)
	if err != nil {
		t.Fatal(err)
	}
	second, secondRoot, err := Snapshot(base, source)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash || root != secondRoot {
		t.Fatalf("snapshot is not stable: %#v %#v", first, second)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("VCS metadata was included: %v", err)
	}

	writeTestFile(t, filepath.Join(source, "scripts", "run.sh"), "#!/bin/sh\necho two\n", 0o755)
	changed, _, err := Snapshot(base, source)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Hash == first.Hash {
		t.Fatal("supporting file contents did not change the bundle hash")
	}
	if err := os.Chmod(filepath.Join(source, "scripts", "run.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	modeChanged, _, err := Snapshot(base, source)
	if err != nil {
		t.Fatal(err)
	}
	if modeChanged.Hash == changed.Hash {
		t.Fatal("executable mode did not change the bundle hash")
	}
}

func TestScanRejectsSymlinksAndLimits(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n", 0o644)
	if err := os.Symlink("SKILL.md", filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(source); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if err := os.Remove(filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "large"), strings.Repeat("x", int(MaxFileSize)+1), 0o644)
	if _, err := Scan(source); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected file size rejection, got %v", err)
	}
}

func TestScanRejectsTooManySupportingFiles(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n", 0o644)
	for i := 0; i <= MaxSupportingFiles; i++ {
		writeTestFile(t, filepath.Join(source, "references", fmt.Sprintf("%03d", i)), "x", 0o644)
	}
	if _, err := Scan(source); err == nil || !strings.Contains(err.Error(), "files") {
		t.Fatalf("expected file count rejection, got %v", err)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n", 0o644)
	writeTestFile(t, filepath.Join(source, "scripts", "run.sh"), "#!/bin/sh\necho ok\n", 0o755)
	archive, original, err := Archive(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	extracted, err := ExtractArchive(bytes.NewReader(archive), destination)
	if err != nil {
		t.Fatal(err)
	}
	if extracted.Hash != original.Hash {
		t.Fatalf("round-trip hash = %s, want %s", extracted.Hash, original.Hash)
	}
	info, err := os.Stat(filepath.Join(destination, "scripts", "run.sh"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable mode was not preserved: %v %#o", err, info.Mode().Perm())
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o444, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractArchive(bytes.NewReader(archive.Bytes()), t.TempDir()); err == nil || !strings.Contains(err.Error(), "invalid archive path") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
