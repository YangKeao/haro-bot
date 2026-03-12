package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestParsePatch(t *testing.T) {
	tests := []struct {
		name      string
		patch     string
		wantCount int
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := parsePatch(tt.patch)
			if err != nil {
				t.Fatalf("parsePatch failed: %v", err)
			}
			if len(ops) != tt.wantCount {
				t.Errorf("Expected %d operations, got %d", tt.wantCount, len(ops))
			}
		})
	}
}
