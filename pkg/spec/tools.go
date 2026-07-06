package spec

import (
	"fmt"
	"sort"
)

// riskRank orders risk classes for comparison.
var riskRank = map[string]int{
	RiskRead:  0,
	RiskWrite: 1,
	RiskAdmin: 2,
}

// ToolCatalog lists every tool an agent may declare, with the minimum risk
// class the tool requires. The designer picks from this list and the risk
// gate refuses specs whose tools exceed their declared class.
var ToolCatalog = map[string]string{
	"web_search":  RiskRead,
	"web_fetch":   RiskRead,
	"http_get":    RiskRead,
	"read_file":   RiskRead,
	"list_files":  RiskRead,
	"grep":        RiskRead,
	"write_file":  RiskWrite,
	"bash":        RiskWrite,
	"http_post":   RiskWrite,
	"git_commit":  RiskWrite,
	"git_push":    RiskAdmin,
	"deploy":      RiskAdmin,
	"send_email":  RiskAdmin,
	"delete_file": RiskAdmin,
}

// CatalogNames returns the tool names sorted, for prompts and help text.
func CatalogNames() []string {
	names := make([]string, 0, len(ToolCatalog))
	for n := range ToolCatalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// CheckRisk refuses a spec whose tool allowlist exceeds its declared risk
// class, and rejects tools that are not in the catalog at all.
func (s *AgentSpec) CheckRisk() error {
	declared, ok := riskRank[s.Risk]
	if !ok {
		return fmt.Errorf("unknown risk class %q", s.Risk)
	}
	for _, t := range s.Tools {
		floor, ok := ToolCatalog[t]
		if !ok {
			return fmt.Errorf("tool %q is not in the catalog", t)
		}
		if riskRank[floor] > declared {
			return fmt.Errorf("tool %q needs risk %s but the spec declares %s", t, floor, s.Risk)
		}
	}
	return nil
}
