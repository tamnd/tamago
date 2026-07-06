// Package tools executes the catalog tools an agent declares in its spec.
// Every executor is a real implementation or an honest not-configured error;
// nothing here fakes a result.
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/tamago/pkg/spec"
)

// outputCap keeps observations small enough for another model turn.
const outputCap = 16 * 1024

// Args is one tool invocation's arguments, all strings for a wire-agnostic protocol.
type Args map[string]string

// Executor runs one catalog tool.
type Executor func(ctx context.Context, a Args) (string, error)

// Registry maps catalog tool names to their executors. Tools in the catalog
// but not here exist for design purposes only and fail with a clear error.
var Registry = map[string]Executor{
	"read_file":   readFile,
	"list_files":  listFiles,
	"grep":        grepTool,
	"http_get":    httpGet,
	"web_fetch":   httpGet,
	"write_file":  writeFile,
	"bash":        bashTool,
	"http_post":   httpPost,
	"git_commit":  gitCommit,
	"git_push":    gitPush,
	"delete_file": deleteFile,
}

// Help describes each implemented tool's arguments for the action prompt.
var Help = map[string]string{
	"read_file":   `{"path": "file to read"}`,
	"list_files":  `{"path": "directory, default ."}`,
	"grep":        `{"pattern": "regexp", "path": "file or directory, default ."}`,
	"http_get":    `{"url": "https://..."}`,
	"web_fetch":   `{"url": "https://..."}`,
	"write_file":  `{"path": "file to write", "content": "full new content"}`,
	"bash":        `{"command": "shell command, 60s timeout"}`,
	"http_post":   `{"url": "https://...", "body": "request body", "content_type": "default application/json"}`,
	"git_commit":  `{"message": "commit message", "dir": "repo, default ."}`,
	"git_push":    `{"dir": "repo, default ."}`,
	"delete_file": `{"path": "file to delete"}`,
}

// Run executes one tool call after re-checking its risk floor against the
// spec's declared class: defense in depth on top of design-time validation.
func Run(ctx context.Context, s *spec.AgentSpec, name string, a Args) (string, error) {
	if !slices.Contains(s.Tools, name) {
		return "", fmt.Errorf("tool %q is not in this agent's allowlist", name)
	}
	if err := s.CheckRisk(); err != nil {
		return "", err
	}
	ex, ok := Registry[name]
	if !ok {
		return "", fmt.Errorf("tool %q is not configured on this machine", name)
	}
	out, err := ex(ctx, a)
	if err != nil {
		return "", err
	}
	if len(out) > outputCap {
		out = out[:outputCap] + "\n[output truncated]"
	}
	return out, nil
}

// Floor returns the risk class a tool requires, for approval decisions.
func Floor(name string) string {
	return spec.ToolCatalog[name]
}

func readFile(ctx context.Context, a Args) (string, error) {
	if a["path"] == "" {
		return "", fmt.Errorf("read_file needs a path")
	}
	b, err := os.ReadFile(a["path"])
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func listFiles(ctx context.Context, a Args) (string, error) {
	dir := a["path"]
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

func grepTool(ctx context.Context, a Args) (string, error) {
	if a["pattern"] == "" {
		return "", fmt.Errorf("grep needs a pattern")
	}
	re, err := regexp.Compile(a["pattern"])
	if err != nil {
		return "", err
	}
	root := a["path"]
	if root == "" {
		root = "."
	}
	var sb strings.Builder
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || sb.Len() > outputCap {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil || !isText(b) {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if sb.Len() == 0 {
		return "no matches", nil
	}
	return sb.String(), nil
}

func isText(b []byte) bool {
	return !slices.Contains(b[:min(len(b), 8000)], 0)
}

func httpGet(ctx context.Context, a Args) (string, error) {
	if a["url"] == "" {
		return "", fmt.Errorf("http_get needs a url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a["url"], nil)
	if err != nil {
		return "", err
	}
	return doHTTP(req)
}

func httpPost(ctx context.Context, a Args) (string, error) {
	if a["url"] == "" {
		return "", fmt.Errorf("http_post needs a url")
	}
	ct := a["content_type"]
	if ct == "" {
		ct = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a["url"], strings.NewReader(a["body"]))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", ct)
	return doHTTP(req)
}

func doHTTP(req *http.Request) (string, error) {
	hc := &http.Client{Timeout: 60 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, outputCap))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, b), nil
}

func writeFile(ctx context.Context, a Args) (string, error) {
	if a["path"] == "" {
		return "", fmt.Errorf("write_file needs a path")
	}
	if err := os.MkdirAll(filepath.Dir(a["path"]), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(a["path"], []byte(a["content"]), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(a["content"]), a["path"]), nil
}

func bashTool(ctx context.Context, a Args) (string, error) {
	if a["command"] == "" {
		return "", fmt.Errorf("bash needs a command")
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "sh", "-c", a["command"]).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	if len(out) == 0 {
		return "(no output)", nil
	}
	return string(out), nil
}

func gitCommit(ctx context.Context, a Args) (string, error) {
	if a["message"] == "" {
		return "", fmt.Errorf("git_commit needs a message")
	}
	dir := a["dir"]
	if dir == "" {
		dir = "."
	}
	add := exec.CommandContext(ctx, "git", "-C", dir, "add", "-A")
	if out, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %w\n%s", err, out)
	}
	commit := exec.CommandContext(ctx, "git", "-C", dir, "commit", "-m", a["message"])
	out, err := commit.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit: %w\n%s", err, out)
	}
	return string(out), nil
}

func gitPush(ctx context.Context, a Args) (string, error) {
	dir := a["dir"]
	if dir == "" {
		dir = "."
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "push").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git push: %w\n%s", err, out)
	}
	return string(out), nil
}

func deleteFile(ctx context.Context, a Args) (string, error) {
	if a["path"] == "" {
		return "", fmt.Errorf("delete_file needs a path")
	}
	if err := os.Remove(a["path"]); err != nil {
		return "", err
	}
	return "deleted " + a["path"], nil
}
