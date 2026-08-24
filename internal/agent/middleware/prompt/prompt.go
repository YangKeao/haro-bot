package prompt

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/skills"
)

// GuidelinesLoader fetches active guidelines for prompt building.
type GuidelinesLoader interface {
	GetActive(ctx context.Context) (*guidelines.Guidelines, error)
}

type middleware struct {
	gl GuidelinesLoader
}

func New(gl GuidelinesLoader) agent.RunMiddleware {
	if isNilGuidelinesLoader(gl) {
		gl = nil
	}
	return &middleware{gl: gl}
}

func (m *middleware) Name() string {
	return "prompt"
}

func (m *middleware) Priority() int {
	return 200
}

func (m *middleware) HandleRun(ctx context.Context, run *agent.RunState, next agent.RunHandler) (string, error) {
	run.Prompt = buildPromptWithInstructions(ctx, m.gl, run.AvailableSkills, run.PromptFormat, run.AgentInstructions)
	return next(ctx, run)
}

func buildPrompt(ctx context.Context, gl GuidelinesLoader, skillsList []skills.Metadata, format string) string {
	return buildPromptWithInstructions(ctx, gl, skillsList, format, "")
}

func buildPromptWithInstructions(ctx context.Context, gl GuidelinesLoader, skillsList []skills.Metadata, format, instructions string) string {
	var b strings.Builder
	format = strings.ToLower(strings.TrimSpace(format))
	skillsXML := ""
	if isClaudeFormat(format) {
		skillsXML = buildSkillsXML(skillsList)
		if skillsXML != "" {
			b.WriteString(skillsXML)
			b.WriteString("\n")
		}
	}
	b.WriteString("You are an assistant.\n")
	b.WriteString("When the conversation gets long or you need a clean handoff, create a session summary using the session_summary tool with a concise summary and optional state.\n")

	if gl != nil {
		if g, err := gl.GetActive(ctx); err == nil && g != nil && g.Content != "" {
			b.WriteString("\n## Guidelines\n")
			b.WriteString(g.Content)
			b.WriteString("\n\n")
		}
	}
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		b.WriteString("\n## Agent Instructions\n")
		b.WriteString(instructions)
		b.WriteString("\n\n")
	}

	if !isClaudeFormat(format) {
		section := renderSkillsSection(skillsList)
		if section != "" {
			b.WriteString(section)
			b.WriteString("\n")
		}
	}
	if len(skillsList) > 0 {
		b.WriteString("To use a skill, call read_skill with its package locator. Only read a skill when it is necessary.\n")
	}
	return b.String()
}

func buildSkillsXML(skillsList []skills.Metadata) string {
	if len(skillsList) == 0 {
		return "<available_skills></available_skills>\nNo skills are assigned to this agent. The workspace may contain other installed skills, but they are unavailable unless explicitly assigned. If asked which skills are available, say that none are assigned; do not list workspace-installed skills."
	}
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range skillsList {
		b.WriteString("  <skill>\n")
		b.WriteString(fmt.Sprintf("    <name>%s</name>\n", xmlEscape(s.Name)))
		b.WriteString(fmt.Sprintf("    <description>%s</description>\n", xmlEscape(s.Description)))
		b.WriteString(fmt.Sprintf("    <package>%s</package>\n", xmlEscape(skills.Package(s))))
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func renderSkillsSection(skillsList []skills.Metadata) string {
	if len(skillsList) == 0 {
		return strings.Join([]string{
			"## Skills",
			"No skills are assigned to this agent. The workspace may contain other installed skills, but they are unavailable unless explicitly assigned. If asked which skills are available, say that none are assigned; do not list workspace-installed skills.",
		}, "\n")
	}
	lines := []string{
		"## Skills",
		"A skill is an immutable package containing a `SKILL.md` file and optional scripts, references, and assets. Below are the skills available to this agent.",
		"### Available skills",
	}
	for _, s := range skillsList {
		lines = append(lines, fmt.Sprintf("- %s: %s (package: %s)", s.Name, s.Description, skills.Package(s)))
	}
	lines = append(lines, "### How to use skills")
	lines = append(lines,
		"- Discovery: The list above is the complete set of skills available to this agent. Package locators are immutable and agent-scoped. Do not list or claim access to workspace-installed skills that are not shown above.",
		"- Trigger rules: If the user names a skill (with `$SkillName` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. Multiple mentions mean use them all. Do not carry skills across turns unless re-mentioned.",
		"- Missing/blocked: If a named skill isn't in the list or its package can't be read, say so briefly and continue with the best fallback.",
		"- How to use a skill (progressive disclosure):",
		"  1) After deciding to use a skill, call `read_skill` with its exact package locator. The default resource is the complete `SKILL.md` including frontmatter.",
		"  2) Follow `next_cursor` until the selected resource is complete. Use `read_skill` with `resource` for specific referenced text files.",
		"  3) If `skill_root` is returned, resolve relative script, reference, and asset paths beneath that sandbox directory.",
		"  4) If `scripts/` exist, prefer running them from `skill_root` instead of retyping large code blocks. Dependencies are not installed automatically.",
		"  5) If no `skill_root` is returned, follow the instructions that do not require bundled files and report the limitation when relevant.",
		"- Coordination and sequencing:",
		"  - If multiple skills apply, choose the minimal set that covers the request and state the order you'll use them.",
		"  - Announce which skill(s) you're using and why (one short line). If you skip an obvious skill, say why.",
		"- Context hygiene:",
		"  - Keep context small: summarize long sections instead of pasting them; only load extra files when needed.",
		"  - Avoid deep reference-chasing: prefer opening only files directly linked from `SKILL.md` unless you're blocked.",
		"  - When variants exist (frameworks, providers, domains), pick only the relevant reference file(s) and note that choice.",
		"- Safety and fallback: If a skill can't be applied cleanly (missing files, unclear instructions), state the issue, pick the next-best approach, and continue.",
	)
	return strings.Join(lines, "\n")
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func isClaudeFormat(format string) bool {
	return format == "claude" || format == "anthropic" || format == "xml"
}

func isNilGuidelinesLoader(gl GuidelinesLoader) bool {
	if gl == nil {
		return true
	}
	v := reflect.ValueOf(gl)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return true
	}
	return false
}
