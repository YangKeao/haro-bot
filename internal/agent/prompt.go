package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

func buildSystemPrompt(memories []memory.Memory, skillsList []skills.Metadata, format string) string {
	return buildPrompt(memories, skillsList, format, true)
}

func buildInterruptPrompt(memories []memory.Memory, format string) string {
	return buildPrompt(memories, nil, format, false)
}

type DefaultPromptBuilder struct{}

func (DefaultPromptBuilder) System(memories []memory.Memory, skillsList []skills.Metadata, format string) string {
	return buildSystemPrompt(memories, skillsList, format)
}

func (DefaultPromptBuilder) Interrupt(memories []memory.Memory, format string) string {
	return buildInterruptPrompt(memories, format)
}

func (DefaultPromptBuilder) Skill(skill skills.Skill) string {
	return buildSkillPrompt(skill)
}

func buildPrompt(memories []memory.Memory, skillsList []skills.Metadata, format string, includeSkills bool) string {
	var b strings.Builder
	format = strings.ToLower(strings.TrimSpace(format))
	skillsXML := ""
	if includeSkills && isClaudeFormat(format) {
		skillsXML = buildSkillsXML(skillsList)
		if skillsXML != "" {
			b.WriteString(skillsXML)
			b.WriteString("\n")
		}
	}
	b.WriteString("You are an assistant. Use the provided long-term memory when relevant.\n")
	if len(memories) > 0 {
		b.WriteString("Long-term memory:\n")
		for _, m := range memories {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", m.Type, m.Content))
		}
	}
	if !includeSkills {
		b.WriteString("Do not use tools or skills. Respond directly.\n")
		return b.String()
	}
	if len(skillsList) > 0 && !isClaudeFormat(format) {
		section := renderSkillsSection(skillsList)
		if section != "" {
			b.WriteString(section)
			b.WriteString("\n")
		}
	}
	if len(skillsList) > 0 {
		b.WriteString("To use a skill, call the tool activate_skill with {name, goal}. Only activate when a skill is necessary.\n")
	}
	return b.String()
}

func buildSkillPrompt(skill skills.Skill) string {
	var b strings.Builder
	path := skillLocation(skill.Metadata.Dir)
	if path == "" {
		path = filepath.Join(skill.Metadata.Dir, "SKILL.md")
	}
	b.WriteString("<skill>\n")
	b.WriteString(fmt.Sprintf("<name>%s</name>\n", xmlEscape(skill.Metadata.Name)))
	b.WriteString(fmt.Sprintf("<path>%s</path>\n", xmlEscape(path)))
	b.WriteString(skill.Instructions)
	b.WriteString("\n</skill>")
	return b.String()
}

func buildSkillsXML(skillsList []skills.Metadata) string {
	if len(skillsList) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range skillsList {
		b.WriteString("  <skill>\n")
		b.WriteString(fmt.Sprintf("    <name>%s</name>\n", xmlEscape(s.Name)))
		b.WriteString(fmt.Sprintf("    <description>%s</description>\n", xmlEscape(s.Description)))
		if loc := skillLocation(s.Dir); loc != "" {
			b.WriteString(fmt.Sprintf("    <location>%s</location>\n", xmlEscape(loc)))
		}
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func renderSkillsSection(skillsList []skills.Metadata) string {
	if len(skillsList) == 0 {
		return ""
	}
	lines := []string{
		"## Skills",
		"A skill is a set of local instructions to follow that is stored in a `SKILL.md` file. Below is the list of skills that can be used. Each entry includes a name, description, and file path so you can open the source for full instructions when using a specific skill.",
		"### Available skills",
	}
	for _, s := range skillsList {
		path := skillLocation(s.Dir)
		if path == "" {
			path = filepath.Join(s.Dir, "SKILL.md")
		}
		path = strings.ReplaceAll(path, `\\`, `/`)
		lines = append(lines, fmt.Sprintf("- %s: %s (file: %s)", s.Name, s.Description, path))
	}
	lines = append(lines, "### How to use skills")
	lines = append(lines,
		"- Discovery: The list above is the skills available in this session (name + description + file path). Skill bodies live on disk at the listed paths.",
		"- Trigger rules: If the user names a skill (with `$SkillName` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. Multiple mentions mean use them all. Do not carry skills across turns unless re-mentioned.",
		"- Missing/blocked: If a named skill isn't in the list or the path can't be read, say so briefly and continue with the best fallback.",
		"- How to use a skill (progressive disclosure):",
		"  1) After deciding to use a skill, open its `SKILL.md`. Read only enough to follow the workflow.",
		"  2) When `SKILL.md` references relative paths (e.g., `scripts/foo.py`), resolve them relative to the skill directory listed above first, and only consider other paths if needed.",
		"  3) If `SKILL.md` points to extra folders such as `references/`, load only the specific files needed for the request; don't bulk-load everything.",
		"  4) If `scripts/` exist, prefer running or patching them instead of retyping large code blocks.",
		"  5) If `assets/` or templates exist, reuse them instead of recreating from scratch.",
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

func skillLocation(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return ""
	}
	return abs
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func isClaudeFormat(format string) bool {
	return format == "claude" || format == "anthropic" || format == "xml"
}
