package gen

import (
	"fmt"
	"strings"

	"github.com/tamnd/tamago/pkg/spec"
)

// Cron renders a shell wrapper plus a crontab line for agents with a cron
// trigger. Agents without one get nothing from this target.
type Cron struct{}

func (Cron) Name() string { return "cron" }

func (Cron) Generate(s *spec.AgentSpec) (map[string]string, error) {
	var cronExpr string
	for _, t := range s.Triggers {
		if t.Type == "cron" && t.Cron != "" {
			cronExpr = t.Cron
			break
		}
	}
	if cronExpr == "" {
		return map[string]string{}, nil
	}
	if s.Risk != spec.RiskRead {
		return nil, fmt.Errorf("refusing a cron harness for %s: risk %s agents act without a human watching; run it manually with --yes instead", s.Name, s.Risk)
	}
	var sh strings.Builder
	sh.WriteString("#!/bin/sh\n")
	sh.WriteString(header(s, "#"))
	fmt.Fprintf(&sh, "# runs the agent through tamago on its schedule: %s\n", cronExpr)
	sh.WriteString("# exit 2 means the agent escalated to a human, wire it to your alerting\n")
	sh.WriteString("set -eu\n")
	fmt.Fprintf(&sh, "tamago run %s --quiet \"scheduled run: do your job for the current period\"\n", s.Name)

	crontab := fmt.Sprintf("%s %s/run-%s.sh >> %s/%s.log 2>&1\n",
		cronExpr, "$HOME/.tamago/cron", s.Name, "$HOME/.tamago/cron", s.Name)

	return map[string]string{
		fmt.Sprintf("run-%s.sh", s.Name): sh.String(),
		"crontab.txt":                    "# add with: crontab -e\n" + crontab,
	}, nil
}
