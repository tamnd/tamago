package runner

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tamnd/tamago/pkg/spec"
	"github.com/tamnd/tamago/pkg/tools"
)

// fakeClient scripts the model side of the action loop.
type fakeClient struct {
	replies []string
	calls   int
	seen    []string
}

func (f *fakeClient) Name() string                  { return "fake" }
func (f *fakeClient) DesignerModel() string         { return "fake-model" }
func (f *fakeClient) ModelForTier(tier string) string { return "fake-" + tier }

func (f *fakeClient) StreamText(ctx context.Context, model, system, user string, maxTokens int64, onDelta func(string)) (string, error) {
	f.seen = append(f.seen, user)
	r := f.replies[min(f.calls, len(f.replies)-1)]
	f.calls++
	return r, nil
}

func (f *fakeClient) JSON(ctx context.Context, model, system, user string, maxTokens int64, schema map[string]any, out any, onDelta func(string)) error {
	return nil
}

func testSpec(toolNames ...string) *spec.AgentSpec {
	return &spec.AgentSpec{
		Name: "t", Job: "j", Role: "r", Risk: spec.RiskAdmin, Tier: "fast",
		SystemPrompt: "sys", Tools: toolNames,
	}
}

func TestParseAction(t *testing.T) {
	a, ok := parseAction(`{"tool": "grep", "args": {"pattern": "x", "n": 3}}`)
	if !ok || a.Tool != "grep" || a.args["pattern"] != "x" || a.args["n"] != "3" {
		t.Fatalf("bad parse: %+v ok=%v", a, ok)
	}
	a, ok = parseAction("Sure!\n```json\n{\"final\": \"done\"}\n```")
	if !ok || a.Final != "done" {
		t.Fatalf("fenced final not parsed: %+v", a)
	}
	if _, ok := parseAction("plain prose, no json"); ok {
		t.Fatal("prose should not parse as an action")
	}
}

func TestToolLoopReadThenFinal(t *testing.T) {
	dir := t.TempDir()
	if err := writeTemp(dir+"/hello.txt", "hello tools"); err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{replies: []string{
		`{"tool": "read_file", "args": {"path": "` + dir + `/hello.txt"}}`,
		`{"final": "the file says hello tools"}`,
	}}
	var steps []Step
	out, err := RunWithOptions(context.Background(), cl, testSpec("read_file"), "read it", nil,
		Options{OnStep: func(s Step) { steps = append(steps, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if out != "the file says hello tools" {
		t.Fatalf("final: %q", out)
	}
	if len(steps) != 1 || steps[0].Tool != "read_file" || steps[0].Observation != "hello tools" {
		t.Fatalf("steps: %+v", steps)
	}
	if !strings.Contains(cl.seen[1], "Observation 1:\nhello tools") {
		t.Fatalf("observation not fed back:\n%s", cl.seen[1])
	}
}

func TestWriteDeniedWithoutApprover(t *testing.T) {
	cl := &fakeClient{replies: []string{
		`{"tool": "write_file", "args": {"path": "/tmp/x", "content": "y"}}`,
		`{"final": "could not write"}`,
	}}
	var steps []Step
	out, err := RunWithOptions(context.Background(), cl, testSpec("write_file"), "write it", nil,
		Options{OnStep: func(s Step) { steps = append(steps, s) }})
	if err != nil || out != "could not write" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if len(steps) != 1 || !steps[0].Denied {
		t.Fatalf("expected a denied step: %+v", steps)
	}
}

func TestToolOutsideAllowlist(t *testing.T) {
	cl := &fakeClient{replies: []string{
		`{"tool": "bash", "args": {"command": "id"}}`,
		`{"final": "ok"}`,
	}}
	var steps []Step
	_, err := RunWithOptions(context.Background(), cl, testSpec("read_file"), "try bash", nil,
		Options{Approve: func(string, tools.Args) bool { return true },
			OnStep: func(s Step) { steps = append(steps, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || !strings.Contains(steps[0].Observation, "not in this agent's allowlist") {
		t.Fatalf("allowlist not enforced: %+v", steps)
	}
}

func TestStepCapForcesFinal(t *testing.T) {
	cl := &fakeClient{replies: []string{
		`{"tool": "list_files", "args": {"path": "."}}`,
		`{"tool": "list_files", "args": {"path": "."}}`,
		`{"tool": "list_files", "args": {"path": "."}}`,
		`{"final": "ran out"}`,
	}}
	out, err := RunWithOptions(context.Background(), cl, testSpec("list_files"), "loop forever", nil,
		Options{MaxSteps: 3})
	if err != nil || out != "ran out" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if cl.calls != 4 {
		t.Fatalf("calls: %d", cl.calls)
	}
}

func TestNoToolsStaysSingleShot(t *testing.T) {
	cl := &fakeClient{replies: []string{"plain answer"}}
	out, err := Run(context.Background(), cl, testSpec(), "hi", nil)
	if err != nil || out != "plain answer" || cl.calls != 1 {
		t.Fatalf("out=%q calls=%d err=%v", out, cl.calls, err)
	}
}

func writeTemp(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestEscalation(t *testing.T) {
	if !Escalated("  ESCALATE: security incident") || Escalated("all fine") {
		t.Fatal("Escalated prefix detection wrong")
	}
	cl := &fakeClient{replies: []string{`{"final": "found a leaked key", "escalate": true}`}}
	out, err := RunWithOptions(context.Background(), cl, testSpec("read_file"), "audit", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !Escalated(out) {
		t.Fatalf("escalate flag did not mark the answer: %q", out)
	}
}

func TestProseReplyGetsOneNudge(t *testing.T) {
	dir := t.TempDir()
	if err := writeTemp(dir+"/n.txt", "nudge worked"); err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{replies: []string{
		"I cannot access your filesystem from this chat.",
		`{"tool": "read_file", "args": {"path": "` + dir + `/n.txt"}}`,
		`{"final": "done"}`,
	}}
	out, err := RunWithOptions(context.Background(), cl, testSpec("read_file"), "read it", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Fatalf("nudge did not recover the loop: %q", out)
	}
	if cl.calls != 3 {
		t.Fatalf("expected 3 model calls, got %d", cl.calls)
	}
}

func TestZeroCallFinalGetsPushback(t *testing.T) {
	dir := t.TempDir()
	if err := writeTemp(dir+"/z.txt", "pushback worked"); err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{replies: []string{
		`{"final": "I do not have access to your files."}`,
		`{"tool": "read_file", "args": {"path": "` + dir + `/z.txt"}}`,
		`{"final": "the file says pushback worked"}`,
	}}
	out, err := RunWithOptions(context.Background(), cl, testSpec("read_file"), "read it", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out != "the file says pushback worked" {
		t.Fatalf("pushback did not recover the run: %q", out)
	}
	if !strings.Contains(cl.seen[1], "finished without using any tool") {
		t.Fatalf("pushback text missing:\n%s", cl.seen[1])
	}
}
