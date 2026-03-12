package skills

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
}

type Frontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	License     string         `yaml:"license"`
	Metadata    map[string]any `yaml:"metadata"`
}
