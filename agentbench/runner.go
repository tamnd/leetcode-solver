package agentbench

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Suite = "leetcode-v6"

var Tools = []string{"leetcode-solver", "tomo", "pi", "opencode", "codex", "claude-code"}

type RunOptions struct {
	Repository, LabRepository, Workspace, Cache, Data string
	Offline, SkipBuild, PrepareOnly                   bool
	Tools, Providers, Scenarios                       []string
	Progress                                          func(string)
}

func Run(ctx context.Context, o RunOptions) error {
	if o.Repository == "" || o.LabRepository == "" || o.Workspace == "" || o.Cache == "" || o.Data == "" {
		return errors.New("repository, lab repository, workspace, cache, and data are required")
	}
	var err error
	o, err = normalizePaths(o)
	if err != nil {
		return err
	}
	dataset, err := Sync(ctx, o.Cache, o.Offline, o.Progress)
	if err != nil {
		return err
	}
	rows, err := SelectLatest(dataset, 1<<20)
	if err != nil {
		return err
	}
	if err := prepareLab(ctx, o, rows); err != nil {
		return err
	}
	if o.PrepareOnly {
		return nil
	}
	tools := o.Tools
	if len(tools) == 0 {
		tools = Tools
	}
	providers := o.Providers
	if len(providers) == 0 {
		providers = []string{"deepseek", "luna"}
	}
	scenarios := o.Scenarios
	if len(scenarios) == 0 {
		for _, r := range rows {
			scenarios = append(scenarios, ScenarioName(r))
		}
	}
	if !o.SkipBuild {
		for _, tool := range tools {
			if err := labCommand(ctx, o, nil, "build", tool); err != nil {
				return err
			}
		}
	}
	for _, provider := range providers {
		var bridge *exec.Cmd
		if provider == "luna" {
			bridge, err = startBridge(ctx, o)
			if err != nil {
				return err
			}
		}
		for _, tool := range tools {
			for _, scenario := range scenarios {
				if o.Progress != nil {
					o.Progress(fmt.Sprintf("run %s / %s / %s", provider, tool, scenario))
				}
				env := providerEnv(provider, tool, o)
				if err := labCommand(ctx, o, env, "run", tool, scenario); err != nil {
					return fmt.Errorf("%s/%s/%s: %w", provider, tool, scenario, err)
				}
			}
		}
		if bridge != nil {
			stopProcess(bridge)
		}
	}
	return WriteReport(o.Data, filepath.Join(o.Workspace, "benchmark.json"), filepath.Join(o.Workspace, "benchmark.md"))
}

func normalizePaths(o RunOptions) (RunOptions, error) {
	for name, target := range map[string]*string{"repository": &o.Repository, "lab repository": &o.LabRepository, "workspace": &o.Workspace, "cache": &o.Cache, "data": &o.Data} {
		absolute, err := filepath.Abs(*target)
		if err != nil {
			return o, fmt.Errorf("resolve %s: %w", name, err)
		}
		*target = absolute
	}
	return o, nil
}

func prepareLab(ctx context.Context, o RunOptions, rows []Row) error {
	if err := os.RemoveAll(o.Workspace); err != nil {
		return err
	}
	if err := os.MkdirAll(o.Workspace, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", TomoLabsRevision)
	cmd.Dir = o.LabRepository
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("archive tomo-labs %s: %w", TomoLabsRevision, err)
	}
	if err := extractTar(bytes.NewReader(b), o.Workspace); err != nil {
		return err
	}
	if err := Materialize(o.Workspace, Suite, rows); err != nil {
		return err
	}
	// Provision the shared image before isolation. Agents may run tests immediately
	// but cannot install packages once attached to the no-egress network.
	baseDocker := filepath.Join(o.Workspace, "tools", "base", "Dockerfile")
	base, err := os.ReadFile(baseDocker)
	if err != nil {
		return err
	}
	baseText := strings.Replace(string(base), "python3 python3-pip", "python3 python3-pip python3-pytest python3-numpy", 1)
	if baseText == string(base) {
		return errors.New("pinned lab base image layout changed: Python package insertion point missing")
	}
	if err := os.WriteFile(baseDocker, []byte(baseText), 0o644); err != nil {
		return err
	}
	// The tomo image must measure the freshly merged engine, not the older pin in
	// the historical lab commit.
	tomoDocker := filepath.Join(o.Workspace, "tools", "tomo", "Dockerfile")
	docker, err := os.ReadFile(tomoDocker)
	if err != nil {
		return err
	}
	lines := strings.Split(string(docker), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "ARG TOMO_VERSION=") {
			lines[i] = "ARG TOMO_VERSION=" + TomoRevision
		}
	}
	if err := os.WriteFile(tomoDocker, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	toolDir := filepath.Join(o.Workspace, "tools", "leetcode-solver")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		return err
	}
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(toolDir, "leetcode-solver"), "./cmd/leetcode-solver")
	build.Dir = o.Repository
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build leetcode-solver: %w: %s", err, out)
	}
	dockerfile := "FROM tomolab-base\nCOPY leetcode-solver /usr/local/bin/leetcode-solver\nCOPY adapter.sh /usr/local/bin/adapter\nRUN chmod +x /usr/local/bin/leetcode-solver /usr/local/bin/adapter\nENTRYPOINT [\"/usr/local/bin/adapter\"]\n"
	adapter := "#!/usr/bin/env bash\nset -uo pipefail\ncd /work\n/usr/bin/time -v -o /trace/time.txt leetcode-solver agent-solve </dev/null >/trace/stdout.log 2>/trace/stderr.log\nstatus=$?\necho \"$status\" >/trace/exit_code\nexit 0\n"
	if err := writeFile(filepath.Join(toolDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return err
	}
	return writeFile(filepath.Join(toolDir, "adapter.sh"), []byte(adapter), 0o755)
}

func extractTar(r io.Reader, dst string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(h.Name)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		path := filepath.Join(dst, clean)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			_, cp := io.Copy(f, tr)
			cl := f.Close()
			if cp != nil {
				return cp
			}
			if cl != nil {
				return cl
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, path); err != nil {
				return err
			}
		}
	}
}

func labCommand(ctx context.Context, o RunOptions, extra []string, args ...string) error {
	a := append([]string{"run", "./cmd/lab", "--suite", Suite}, args...)
	cmd := exec.CommandContext(ctx, "go", a...)
	cmd.Dir = o.Workspace
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), append([]string{"LAB_ROOT=" + o.Workspace, "LAB_DATA=" + o.Data, "LAB_CONCURRENCY=1", "LAB_ATTEMPTS=1", "LAB_ISOLATE=1", "LAB_KEEP_RUNS=0"}, extra...)...)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func providerEnv(provider, tool string, o RunOptions) []string {
	switch provider {
	case "deepseek":
		// DeepSeek free is the preferred Zen route. If its daily quota is exhausted,
		// the documented fallback keeps the benchmark moving and the result records
		// the actual model rather than pretending the cell is still DeepSeek.
		return []string{"LAB_MODEL=hy3-free", "LAB_UPSTREAM=https://opencode.ai/zen", "LAB_PASSTHROUGH=0", "LAB_NAME_PREFIX=leetcode-zen"}
	case "luna":
		pass := "0"
		if tool == "codex" {
			pass = "1"
		}
		return []string{"LAB_MODEL=gpt-5.6-luna", "LAB_UPSTREAM=http://host.containers.internal:8790", "LAB_PASSTHROUGH=" + pass, "LAB_NAME_PREFIX=leetcode-luna"}
	default:
		return nil
	}
}

func startBridge(ctx context.Context, o RunOptions) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/lab", "bridge", "--model", "gpt-5.6-luna", "--effort", "high", "--port", "8790")
	cmd.Dir = o.Workspace
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	client := http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		resp, pingErr := client.Get("http://127.0.0.1:8790/healthz")
		if pingErr == nil {
			_ = resp.Body.Close()
		}
		if pingErr == nil && resp.StatusCode == http.StatusOK {
			return cmd, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	stopProcess(cmd)
	return nil, errors.New("codex bridge did not become ready")
}
func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func DefaultWorkspace() string {
	return filepath.Join(os.TempDir(), "leetcode-solver-agentbench-"+runtime.GOARCH)
}
