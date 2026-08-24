package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skills"
)

const skillResourcePageSize = 512 << 10

type ReadSkillTool struct {
	skills    skillReader
	sandboxes skillMaterializer
	agentID   int64
	allowed   map[string]struct{}
}

type skillReader interface {
	Get(string) (skills.Metadata, bool)
	ReadResource(string, string, string) (skills.Metadata, []byte, error)
}

type skillMaterializer interface {
	Enabled() bool
	EnsureSkill(context.Context, int64, string, string) (sandbox.SkillMaterialization, error)
}

type readSkillArgs struct {
	Package  string `json:"package"`
	Resource string `json:"resource,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
}

type activateSkillArgs struct {
	Name string `json:"name"`
	Goal string `json:"goal,omitempty"`
}

type readSkillResult struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Hash       string `json:"hash"`
	Resource   string `json:"resource"`
	Contents   string `json:"contents"`
	Encoding   string `json:"encoding,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	SkillRoot  string `json:"skill_root,omitempty"`
	Warning    string `json:"warning,omitempty"`
}

func NewReadSkillTool(skillsManager skillReader, sandboxes skillMaterializer, agentID int64, allowedNames []string) *ReadSkillTool {
	allowed := make(map[string]struct{}, len(allowedNames))
	for _, name := range allowedNames {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	return &ReadSkillTool{skills: skillsManager, sandboxes: sandboxes, agentID: agentID, allowed: allowed}
}

func (t *ReadSkillTool) Name() string { return "read_skill" }

func (t *ReadSkillTool) Description() string {
	return "Read an allowed immutable skill package and make its scripts and resources available in the agent sandbox."
}

func (t *ReadSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"package":  map[string]any{"type": "string", "description": "Package locator from the available skills list (skill://name/sha256)."},
			"resource": map[string]any{"type": "string", "description": "Relative resource path; defaults to SKILL.md."},
			"cursor":   map[string]any{"type": "string", "description": "Pagination cursor returned by a previous read_skill call."},
		},
		"required": []string{"package"},
	}
}

func (t *ReadSkillTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	if t.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var input readSkillArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	name, hash, err := parseSkillPackage(input.Package)
	if err != nil {
		return "", err
	}
	if _, ok := t.allowed[name]; !ok {
		return "", errors.New("skill is not available to this agent")
	}
	resource := strings.TrimSpace(input.Resource)
	if resource == "" {
		resource = "SKILL.md"
	}
	meta, data, err := t.skills.ReadResource(name, hash, resource)
	if err != nil {
		return "", err
	}
	offset := 0
	if input.Cursor != "" {
		offset, err = strconv.Atoi(input.Cursor)
		if err != nil || offset < 0 || offset > len(data) {
			return "", errors.New("invalid read_skill cursor")
		}
		if utf8.Valid(data) && offset < len(data) && data[offset]&0xc0 == 0x80 {
			return "", errors.New("invalid read_skill cursor")
		}
	}
	end := offset + skillResourcePageSize
	if end > len(data) {
		end = len(data)
	}
	if utf8.Valid(data) && end < len(data) {
		for end > offset && data[end]&0xc0 == 0x80 {
			end--
		}
	}
	page := data[offset:end]
	result := readSkillResult{Name: name, Version: meta.Version, Hash: hash, Resource: resource}
	if utf8.Valid(page) {
		result.Contents = string(page)
	} else {
		result.Contents = base64.StdEncoding.EncodeToString(page)
		result.Encoding = "base64"
	}
	if end < len(data) {
		result.NextCursor = strconv.Itoa(end)
	}
	if input.Cursor == "" {
		if t.sandboxes == nil || !t.sandboxes.Enabled() {
			result.Warning = "Skill instructions are available, but this agent has no enabled sandbox for bundled scripts and resources."
		} else if materialized, materializeErr := t.sandboxes.EnsureSkill(ctx, t.agentID, hash, meta.Dir); materializeErr != nil {
			result.Warning = fmt.Sprintf("Skill instructions are available, but the bundle could not be materialized in the sandbox: %v", materializeErr)
		} else {
			result.SkillRoot = materialized.SkillRoot
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

type ActivateSkillCompatTool struct {
	read *ReadSkillTool
}

func NewActivateSkillCompatTool(read *ReadSkillTool) *ActivateSkillCompatTool {
	return &ActivateSkillCompatTool{read: read}
}

func (t *ActivateSkillCompatTool) Name() string { return "activate_skill" }
func (t *ActivateSkillCompatTool) Hidden() bool { return true }
func (t *ActivateSkillCompatTool) Description() string {
	return "Compatibility alias for read_skill."
}
func (t *ActivateSkillCompatTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"goal": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
}
func (t *ActivateSkillCompatTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.read == nil || t.read.skills == nil {
		return "", errors.New("skills manager not configured")
	}
	var input activateSkillArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	if _, allowed := t.read.allowed[input.Name]; !allowed {
		return "", errors.New("skill is not available to this agent")
	}
	meta, ok := t.read.skills.Get(input.Name)
	if !ok {
		return "", errors.New("skill not found")
	}
	readArgs, err := json.Marshal(readSkillArgs{Package: skills.Package(meta)})
	if err != nil {
		return "", err
	}
	return t.read.Execute(ctx, tc, readArgs)
}

func parseSkillPackage(locator string) (string, string, error) {
	const prefix = "skill://"
	if !strings.HasPrefix(locator, prefix) {
		return "", "", errors.New("invalid skill package locator")
	}
	value := strings.TrimPrefix(locator, prefix)
	separator := strings.LastIndexByte(value, '/')
	if separator <= 0 || separator == len(value)-1 {
		return "", "", errors.New("invalid skill package locator")
	}
	name, hash := value[:separator], value[separator+1:]
	if strings.ContainsAny(name, `/\\`) || !isSHA256(hash) {
		return "", "", errors.New("invalid skill package locator")
	}
	return name, hash, nil
}

func isSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
