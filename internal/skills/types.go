package skills

import (
	"strings"

	"gopkg.in/yaml.v3"
)

type Metadata struct {
	Name        string
	Description string
	Dir         string
	Version     string
	Hash        string
}

type Skill struct {
	Metadata     Metadata
	Instructions string
	AllowedTools []string
}

type AllowedTools []string

func (a *AllowedTools) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		parts := strings.Fields(value.Value)
		*a = parts
		return nil
	case yaml.SequenceNode:
		var out []string
		for _, n := range value.Content {
			if n.Kind == yaml.ScalarNode {
				out = append(out, n.Value)
			}
		}
		*a = out
		return nil
	default:
		*a = nil
		return nil
	}
}

type Frontmatter struct {
	Name          string                 `yaml:"name"`
	Description   string                 `yaml:"description"`
	License       string                 `yaml:"license"`
	Compatibility map[string]any         `yaml:"compatibility"`
	Metadata      map[string]any         `yaml:"metadata"`
	AllowedTools  AllowedTools           `yaml:"allowed-tools"`
}
