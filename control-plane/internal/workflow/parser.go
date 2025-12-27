package workflow

import (
	"gopkg.in/yaml.v3"
)

type Workflow struct {
	Name string         `yaml:"name"`
	Jobs map[string]Job `yaml:"jobs"`
}

type Job struct {
	Name  string   `yaml:"name"`
	Needs []string `yaml:"needs,omitempty"`
	Steps []Step   `yaml:"steps"`
}

type Step struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func Parse(data []byte) (*Workflow, error) {
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}