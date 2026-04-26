package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Skill struct {
	Meta map[string]string
	Body string
	Path string
}

type SkillLoader struct {
	skillsDir string
	Skills    map[string]Skill
}

func NewSkillLoader(skillsDir string) (*SkillLoader, error) {
	loader := &SkillLoader{
		skillsDir: skillsDir,
		Skills:    make(map[string]Skill),
	}
	err := loader.loadAll()
	return loader, err
}

func (s *SkillLoader) loadAll() error {
	_, err := os.Stat(s.skillsDir)
	if os.IsNotExist(err) {
		fmt.Printf("Warning: Skills directory not found: %s\n", s.skillsDir)
		return nil
	} else if err != nil {
		return err
	}

	return filepath.WalkDir(s.skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == "SKILL.md" {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			meta, body := s.parseFrontMatter(string(content))

			defaultName := filepath.Base(filepath.Dir(path))
			name := defaultName
			if val, ok := meta["name"]; ok && val != "" {
				name = val
			}
			s.Skills[name] = Skill{Meta: meta, Body: body, Path: path}
		}
		return nil
	})
}

func (s *SkillLoader) parseFrontMatter(text string) (map[string]string, string) {
	re := regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 3 {
		return make(map[string]string), text
	}

	metaText := strings.TrimSpace(matches[1])
	body := strings.TrimSpace(matches[2])
	meta := make(map[string]string)

	for _, line := range strings.Split(metaText, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return meta, body
}

func (s *SkillLoader) GetDescriptions() string {
	if len(s.Skills) == 0 {
		return "no skills available."
	}
	var names []string
	for k := range s.Skills {
		names = append(names, k)
	}
	sort.Strings(names)

	var lines []string
	for _, name := range names {
		skill := s.Skills[name]
		desc, ok := skill.Meta["description"]
		if !ok {
			desc = "No description."
		}
		line := fmt.Sprintf(" - %s: %s", name, desc)
		if tags, ok := skill.Meta["tags"]; ok && tags != "" {
			line += fmt.Sprintf(" [%s]", tags)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *SkillLoader) GetContent(name string) string {
	skill, ok := s.Skills[name]
	if !ok {
		var available []string
		for k := range s.Skills {
			available = append(available, k)
		}
		return fmt.Sprintf("Error: Unknown skill '%s'. Available skills: %s", name, strings.Join(available, ", "))
	}
	return fmt.Sprintf("<skill name=\"%s\">\n%s\n</skill>", name, skill.Body)
}
