// Package garden is tamago's registry: a directory of agents, each with its
// live spec, generation history, and eval results.
//
// Layout: $TAMAGO_GARDEN or ~/.tamago/garden/
//
//	NAME/spec.yaml               live spec
//	NAME/generations/gen-001.yaml ... frozen copies, one per generation
//	NAME/evals/result-*.json     eval reports
package garden

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/tamago/pkg/spec"
)

// Root returns the garden directory, creating it if needed.
func Root() (string, error) {
	dir := os.Getenv("TAMAGO_GARDEN")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".tamago", "garden")
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// Dir returns the directory for one agent.
func Dir(name string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

// Save writes the live spec and freezes a copy under generations/.
func Save(s *spec.AgentSpec) error {
	dir, err := Dir(s.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "generations"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "evals"), 0o755); err != nil {
		return err
	}
	if err := s.Save(filepath.Join(dir, "spec.yaml")); err != nil {
		return err
	}
	frozen := filepath.Join(dir, "generations", fmt.Sprintf("gen-%03d.yaml", s.Meta.Generation))
	return s.Save(frozen)
}

// Load reads an agent's live spec by name.
func Load(name string) (*spec.AgentSpec, error) {
	dir, err := Dir(name)
	if err != nil {
		return nil, err
	}
	s, err := spec.Load(filepath.Join(dir, "spec.yaml"))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no agent named %q in the garden (try `tamago ls`)", name)
	}
	return s, err
}

// Entry is one row in the garden listing.
type Entry struct {
	Spec      *spec.AgentSpec
	LastScore float64 // -1 when never evaluated
}

// List returns every agent in the garden, sorted by name.
func List() ([]Entry, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		s, err := spec.Load(filepath.Join(root, d.Name(), "spec.yaml"))
		if err != nil {
			continue
		}
		score := -1.0
		if r, err := LatestEval(s.Name); err == nil && r != nil {
			score = r.Mean
		}
		out = append(out, Entry{Spec: s, LastScore: score})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec.Name < out[j].Spec.Name })
	return out, nil
}

// EvalResult mirrors evals.Report for storage without an import cycle.
type EvalResult struct {
	Generation int     `json:"generation"`
	When       string  `json:"when"`
	Mean       float64 `json:"mean"`
	Pass       bool    `json:"pass"`
	Fixtures   []struct {
		Input  string  `json:"input"`
		Expect string  `json:"expect"`
		Output string  `json:"output"`
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	} `json:"fixtures"`
}

// SaveEval persists an eval report for an agent.
func SaveEval(name string, r *EvalResult) error {
	dir, err := Dir(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "evals"), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	stamp := strings.ReplaceAll(strings.ReplaceAll(r.When, ":", ""), "-", "")
	path := filepath.Join(dir, "evals", fmt.Sprintf("result-gen%03d-%s.json", r.Generation, stamp))
	return os.WriteFile(path, b, 0o644)
}

// EvalHistory returns all stored eval reports, oldest first.
func EvalHistory(name string) ([]*EvalResult, error) {
	dir, err := Dir(name)
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "evals", "result-*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []*EvalResult
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r EvalResult
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		out = append(out, &r)
	}
	return out, nil
}

// LatestEval returns the most recent eval report, or nil when none exist.
func LatestEval(name string) (*EvalResult, error) {
	hist, err := EvalHistory(name)
	if err != nil || len(hist) == 0 {
		return nil, err
	}
	return hist[len(hist)-1], nil
}

// Generations lists the frozen generation files for an agent.
func Generations(name string) ([]string, error) {
	dir, err := Dir(name)
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "generations", "gen-*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
