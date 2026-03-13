package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPatch_AddFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Add File: test.txt
+Hello, World!
+This is a test file.
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	result, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify file was created
	content, err := os.ReadFile(filepath.Join(tmpDir, "test.txt"))
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	expected := "Hello, World!\nThis is a test file."
	if string(content) != expected {
		t.Errorf("Expected %q, got %q", expected, string(content))
	}

	t.Logf("Result: %s", result)
}

func TestApplyPatch_DeleteFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file to delete
	testFile := filepath.Join(tmpDir, "delete_me.txt")
	if err := os.WriteFile(testFile, []byte("delete me"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Delete File: delete_me.txt
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	result, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("File should have been deleted")
	}

	t.Logf("Result: %s", result)
}

func TestApplyPatch_UpdateFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file to update
	testFile := filepath.Join(tmpDir, "update.txt")
	originalContent := "line1\nline2\nline3\n"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Update File: update.txt
@@
 line1
-line2
+line2 modified
 line3
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	result, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify file was updated
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	expected := "line1\nline2 modified\nline3\n"
	if string(content) != expected {
		t.Errorf("Expected %q, got %q", expected, string(content))
	}

	t.Logf("Result: %s", result)
}

func TestApplyPatch_RenameFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file to rename
	oldFile := filepath.Join(tmpDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
 content
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	result, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify old file was deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("Old file should have been deleted")
	}

	// Verify new file was created
	newFile := filepath.Join(tmpDir, "new.txt")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read renamed file: %v", err)
	}

	if string(content) != "content" {
		t.Errorf("Expected content %q, got %q", "content", string(content))
	}

	t.Logf("Result: %s", result)
}

func TestApplyPatch_RenameFile_MoveTargetResolvesFromWorkdir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldFile := filepath.Join(tmpDir, "src", "old.txt")
	if err := os.MkdirAll(filepath.Dir(oldFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Update File: src/old.txt
*** Move to: dest/new.txt
 content
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	if _, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "src", "dest", "new.txt")); err == nil {
		t.Fatalf("move target should resolve from workdir, not source directory")
	}

	newFile := filepath.Join(tmpDir, "dest", "new.txt")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("Failed to read renamed file: %v", err)
	}
	if string(content) != "content" {
		t.Fatalf("Expected content %q, got %q", "content", string(content))
	}
}

func TestApplyPatch_MultipleOperations(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create files for operations
	if err := os.WriteFile(filepath.Join(tmpDir, "delete.txt"), []byte("delete"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "update.txt"), []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Add File: new.txt
+new file content
*** Update File: update.txt
 old content
+new line
*** Delete File: delete.txt
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	result, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify new file
	if _, err := os.Stat(filepath.Join(tmpDir, "new.txt")); os.IsNotExist(err) {
		t.Errorf("New file should have been created")
	}

	// Verify updated file
	content, _ := os.ReadFile(filepath.Join(tmpDir, "update.txt"))
	expected := "old content\nnew line"
	if string(content) != expected {
		t.Errorf("Expected %q, got %q", expected, string(content))
	}

	// Verify deleted file
	if _, err := os.Stat(filepath.Join(tmpDir, "delete.txt")); !os.IsNotExist(err) {
		t.Errorf("File should have been deleted")
	}

	t.Logf("Result: %s", result)
}

func TestApplyPatch_UpdateFileWithMultipleDistantHunks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lines := []string{
		"line1",
		"line2",
		"line3",
		"line4",
		"line5",
		"line6",
		"line7",
		"line8",
		"line9",
		"line10",
	}
	testFile := filepath.Join(tmpDir, "update.txt")
	if err := os.WriteFile(testFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Update File: update.txt
@@
 line1
-line2
+line2 modified
 line3
@@
 line8
-line9
+line9 modified
 line10
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	if _, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	expected := strings.Join([]string{
		"line1",
		"line2 modified",
		"line3",
		"line4",
		"line5",
		"line6",
		"line7",
		"line8",
		"line9 modified",
		"line10",
		"",
	}, "\n")
	if string(content) != expected {
		t.Fatalf("Expected %q, got %q", expected, string(content))
	}
}

func TestApplyPatch_UpdateFileWithEndOfFileMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apply_patch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "tail.txt")
	if err := os.WriteFile(testFile, []byte("first\nsecond"), 0644); err != nil {
		t.Fatal(err)
	}

	fs := NewFS(nil)
	tool := NewApplyPatchTool(fs)

	patch := `*** Begin Patch
*** Update File: tail.txt
@@
 first
-second
+second updated
*** End of File
*** End Patch`

	args, _ := json.Marshal(applyPatchArgs{Patch: patch, Workdir: tmpDir})
	if _, err := tool.Execute(context.Background(), ToolContext{BaseDir: tmpDir}, args); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}
	if string(content) != "first\nsecond updated" {
		t.Fatalf("Expected %q, got %q", "first\nsecond updated", string(content))
	}
}

func TestParsePatch(t *testing.T) {
	tests := []struct {
		name      string
		patch     string
		wantCount int
		wantErr   string
	}{
		{
			name: "single add",
			patch: `*** Begin Patch
*** Add File: test.txt
+content
*** End Patch`,
			wantCount: 1,
		},
		{
			name: "multiple operations",
			patch: `*** Begin Patch
*** Add File: new.txt
+content
*** Update File: old.txt
 old
-new
+new modified
*** Delete File: obsolete.txt
*** End Patch`,
			wantCount: 3,
		},
		{
			name: "with move",
			patch: `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
 content
*** End Patch`,
			wantCount: 1,
		},
		{
			name: "missing end patch marker",
			patch: `*** Begin Patch
*** Add File: test.txt
+content`,
			wantErr: "patch must end with *** End Patch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := parsePatch(tt.patch)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePatch failed: %v", err)
			}
			if len(ops) != tt.wantCount {
				t.Errorf("Expected %d operations, got %d", tt.wantCount, len(ops))
			}
		})
	}
}
