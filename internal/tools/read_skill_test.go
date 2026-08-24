package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type fakeSkillReader struct {
	meta skills.Metadata
	data []byte
}

func (f fakeSkillReader) Get(name string) (skills.Metadata, bool) {
	return f.meta, name == f.meta.Name
}

func (f fakeSkillReader) ReadResource(name, hash, resource string) (skills.Metadata, []byte, error) {
	if name != f.meta.Name || hash != f.meta.Hash {
		return skills.Metadata{}, nil, errors.New("not found")
	}
	return f.meta, f.data, nil
}

type fakeSkillMaterializer struct {
	enabled bool
	root    string
	err     error
	calls   int
}

func (f *fakeSkillMaterializer) Enabled() bool { return f.enabled }
func (f *fakeSkillMaterializer) EnsureSkill(context.Context, int64, string, string) (sandbox.SkillMaterialization, error) {
	f.calls++
	return sandbox.SkillMaterialization{SkillRoot: f.root}, f.err
}

func TestReadSkillIsAgentScopedAndMaterializesBundle(t *testing.T) {
	hash := strings.Repeat("a", 64)
	reader := fakeSkillReader{meta: skills.Metadata{Name: "demo", Version: "commit", Hash: hash, Dir: "/host/bundle"}, data: []byte("---\nname: demo\ndescription: demo\n---\nUse scripts/run.sh\n")}
	materializer := &fakeSkillMaterializer{enabled: true, root: "/workspace/.haro/skills/sha256/" + hash}
	tool := NewReadSkillTool(reader, materializer, 42, []string{"demo"})

	output, err := tool.Execute(context.Background(), ToolContext{}, json.RawMessage(`{"package":"skill://demo/`+hash+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result readSkillResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Contents, "---") || result.SkillRoot != materializer.root || materializer.calls != 1 {
		t.Fatalf("unexpected read result: %#v calls=%d", result, materializer.calls)
	}

	blocked := NewReadSkillTool(reader, materializer, 42, []string{"another"})
	if _, err := blocked.Execute(context.Background(), ToolContext{}, json.RawMessage(`{"package":"skill://demo/`+hash+`"}`)); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected agent scope rejection, got %v", err)
	}
}

func TestReadSkillWithoutSandboxStillReturnsInstructions(t *testing.T) {
	hash := strings.Repeat("b", 64)
	reader := fakeSkillReader{meta: skills.Metadata{Name: "demo", Hash: hash}, data: []byte("complete instructions")}
	tool := NewReadSkillTool(reader, nil, 7, []string{"demo"})
	output, err := tool.Execute(context.Background(), ToolContext{}, json.RawMessage(`{"package":"skill://demo/`+hash+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result readSkillResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Contents != "complete instructions" || result.Warning == "" || result.SkillRoot != "" {
		t.Fatalf("unexpected no-sandbox result: %#v", result)
	}
}

func TestReadSkillPaginationPreservesUTF8Boundaries(t *testing.T) {
	hash := strings.Repeat("e", 64)
	data := strings.Repeat("a", skillResourcePageSize-1) + "你tail"
	reader := fakeSkillReader{meta: skills.Metadata{Name: "demo", Hash: hash}, data: []byte(data)}
	tool := NewReadSkillTool(reader, nil, 7, []string{"demo"})
	firstOutput, err := tool.Execute(context.Background(), ToolContext{}, json.RawMessage(`{"package":"skill://demo/`+hash+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var first readSkillResult
	if err := json.Unmarshal([]byte(firstOutput), &first); err != nil {
		t.Fatal(err)
	}
	if first.Encoding != "" || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	args, _ := json.Marshal(readSkillArgs{Package: "skill://demo/" + hash, Cursor: first.NextCursor})
	secondOutput, err := tool.Execute(context.Background(), ToolContext{}, args)
	if err != nil {
		t.Fatal(err)
	}
	var second readSkillResult
	if err := json.Unmarshal([]byte(secondOutput), &second); err != nil {
		t.Fatal(err)
	}
	if first.Contents+second.Contents != data || second.NextCursor != "" {
		t.Fatal("paginated UTF-8 content did not round-trip")
	}
}

func TestActivateSkillCompatibilityAliasIsHiddenAndScoped(t *testing.T) {
	hash := strings.Repeat("d", 64)
	reader := fakeSkillReader{meta: skills.Metadata{Name: "demo", Hash: hash}, data: []byte("instructions")}
	read := NewReadSkillTool(reader, nil, 7, []string{"demo"})
	compat := NewActivateSkillCompatTool(read)
	registry := NewRegistry(read, compat)
	for _, tool := range registry.List() {
		if tool.Name() == "activate_skill" {
			t.Fatal("compatibility alias was advertised")
		}
	}
	if _, ok := registry.Get("activate_skill"); !ok {
		t.Fatal("compatibility alias was not callable")
	}
	output, err := compat.Execute(context.Background(), ToolContext{}, json.RawMessage(`{"name":"demo"}`))
	if err != nil || !strings.Contains(output, "instructions") {
		t.Fatalf("compatibility read failed: output=%s err=%v", output, err)
	}
}

func TestReadSkillRejectsPackageAndResourceTraversal(t *testing.T) {
	hash := strings.Repeat("c", 64)
	reader := fakeSkillReader{meta: skills.Metadata{Name: "demo", Hash: hash}, data: []byte("x")}
	tool := NewReadSkillTool(reader, nil, 1, []string{"demo"})
	for _, locator := range []string{"demo/" + hash, "skill://../demo/" + hash, "skill://demo/not-a-hash"} {
		args, _ := json.Marshal(readSkillArgs{Package: locator})
		if _, err := tool.Execute(context.Background(), ToolContext{}, args); err == nil {
			t.Fatalf("expected invalid locator %q to fail", locator)
		}
	}
}
