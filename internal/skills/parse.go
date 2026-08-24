package skills

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	errNoFrontmatter = errors.New("missing frontmatter")
)

func parseSkillFile(data []byte) (Frontmatter, string, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return Frontmatter{}, "", err
	}
	var meta Frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return Frontmatter{}, "", err
	}
	return meta, string(body), nil
}

func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil, nil, errNoFrontmatter
	}
	parts := bytes.SplitN(data, []byte("---"), 3)
	if len(parts) < 3 {
		return nil, nil, errNoFrontmatter
	}
	fm := strings.TrimSpace(string(parts[1]))
	body := strings.TrimLeft(string(parts[2]), "\r\n")
	return []byte(fm), []byte(body), nil
}
