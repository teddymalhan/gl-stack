package config

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/cli/go-gh/v2/pkg/term"

	glapi "github.com/teddymalhan/github-stacker-prs/internal/gitlab"
	"github.com/teddymalhan/github-stacker-prs/internal/theme"
)

// Config holds shared state for all commands.
type Config struct {
	Terminal term.Term
	Out      *os.File
	Err      *os.File
	In       *os.File

	ColorSuccess func(string) string
	ColorError   func(string) string
	ColorWarning func(string) string
	ColorBold    func(string) string
	ColorBlue    func(string) string
	ColorMagenta func(string) string
	ColorCyan    func(string) string
	ColorGray    func(string) string

	// GitLabClientOverride, when non-nil, is returned by GitLabClient()
	// instead of creating a real client. Used in tests to inject a MockClient.
	GitLabClientOverride glapi.ClientOps

	// ForceInteractive, when true, makes IsInteractive() return true
	// regardless of the terminal state. Used in tests.
	ForceInteractive bool

	// SelectFn, when non-nil, is called instead of prompting via the
	// terminal. Used in tests to simulate interactive selection.
	SelectFn func(prompt, defaultValue string, options []string) (int, error)

	// ConfirmFn, when non-nil, is called instead of prompting via the
	// terminal. Used in tests to simulate yes/no confirmation prompts.
	ConfirmFn func(prompt string, defaultValue bool) (bool, error)

	// InputFn, when non-nil, is called instead of prompting via the
	// terminal. Used in tests to simulate text input prompts.
	InputFn func(prompt string) (string, error)

	// RepoOverride, when non-nil, is returned by Repo() instead of detecting
	// the repository from its git remote. Used by tests.
	RepoOverride *repository.Repository
}

// New creates a new Config with terminal-aware output and color support.
func New() *Config {
	terminal := term.FromEnv()
	cfg := &Config{
		Terminal: terminal,
		Out:      os.Stdout,
		Err:      os.Stderr,
		In:       os.Stdin,
	}

	if terminal.IsColorEnabled() {
		cfg.ColorSuccess = theme.Success
		cfg.ColorError = theme.Error
		cfg.ColorWarning = theme.Warning
		cfg.ColorBold = theme.Bold
		cfg.ColorBlue = theme.Blue
		cfg.ColorMagenta = theme.Magenta
		cfg.ColorCyan = theme.Cyan
		cfg.ColorGray = theme.Gray
	} else {
		noop := func(s string) string { return s }
		cfg.ColorSuccess = noop
		cfg.ColorError = noop
		cfg.ColorWarning = noop
		cfg.ColorBold = noop
		cfg.ColorBlue = noop
		cfg.ColorMagenta = noop
		cfg.ColorCyan = noop
		cfg.ColorGray = noop
	}

	return cfg
}

func (c *Config) Successf(format string, args ...any) {
	fmt.Fprintf(c.Err, "%s %s\n", c.ColorSuccess("\u2713"), fmt.Sprintf(format, args...))
}

func (c *Config) Errorf(format string, args ...any) {
	fmt.Fprintf(c.Err, "%s %s\n", c.ColorError("\u2717"), fmt.Sprintf(format, args...))
}

func (c *Config) Warningf(format string, args ...any) {
	fmt.Fprintf(c.Err, "%s %s\n", c.ColorWarning("\u26a0"), fmt.Sprintf(format, args...))
}

func (c *Config) Infof(format string, args ...any) {
	fmt.Fprintf(c.Err, "%s %s\n", c.ColorCyan("\u2139"), fmt.Sprintf(format, args...))
}

func (c *Config) Printf(format string, args ...any) {
	fmt.Fprintf(c.Err, format+"\n", args...)
}

func (c *Config) Outf(format string, args ...any) {
	fmt.Fprintf(c.Out, format, args...)
}

// PRLink formats a merge request IID as a clickable terminal hyperlink.
// The name is retained internally to keep the provider-neutral UI contract.
func (c *Config) PRLink(number int, url string) string {
	label := fmt.Sprintf("!%d", number)
	if c.Terminal.IsColorEnabled() {
		if url != "" {
			// OSC 8 hyperlink
			label = fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, label)
		}
		// Underline
		label = fmt.Sprintf("\033[4m%s\033[24m", label)
	}
	return label
}

func (c *Config) IsInteractive() bool {
	return c.ForceInteractive || c.Terminal.IsTerminalOutput()
}

func (c *Config) Repo() (repository.Repository, error) {
	if c.RepoOverride != nil {
		return *c.RepoOverride, nil
	}
	return currentRepository()
}

func (c *Config) GitLabClient() (glapi.ClientOps, error) {
	if c.GitLabClientOverride != nil {
		return c.GitLabClientOverride, nil
	}
	repo, err := c.Repo()
	if err != nil {
		return nil, fmt.Errorf("determining repository: %w", err)
	}
	return glapi.NewClient(repo.Host, repo.Owner, repo.Name)
}

func currentRepository() (repository.Repository, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	remote := "origin"
	if output, err := exec.CommandContext(ctx, "git", "remote").Output(); err == nil {
		remotes := strings.Fields(string(output))
		if len(remotes) == 1 {
			remote = remotes[0]
		} else if len(remotes) > 1 && !contains(remotes, remote) {
			return repository.Repository{}, fmt.Errorf("multiple git remotes found and none is named origin")
		}
	}
	output, err := exec.CommandContext(ctx, "git", "remote", "get-url", remote).Output()
	if err != nil {
		return repository.Repository{}, fmt.Errorf("reading git remote %q: %w", remote, err)
	}
	return parseRemoteRepository(strings.TrimSpace(string(output)))
}

func parseRemoteRepository(raw string) (repository.Repository, error) {
	host := ""
	projectPath := ""
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return repository.Repository{}, fmt.Errorf("parsing git remote URL: %w", err)
		}
		host = u.Host
		projectPath = strings.TrimPrefix(u.Path, "/")
	} else {
		at := strings.LastIndex(raw, "@")
		colon := strings.Index(raw, ":")
		if at >= 0 {
			colon = strings.Index(raw[at+1:], ":")
			if colon >= 0 {
				colon += at + 1
			}
		}
		if colon <= 0 || colon == len(raw)-1 {
			return repository.Repository{}, fmt.Errorf("unsupported git remote URL %q", raw)
		}
		host = raw[:colon]
		if at >= 0 {
			host = raw[at+1 : colon]
		}
		projectPath = raw[colon+1:]
	}

	projectPath = strings.TrimSuffix(strings.Trim(projectPath, "/"), ".git")
	owner, name, ok := strings.Cut(projectPath, "/")
	if !ok || owner == "" || name == "" {
		return repository.Repository{}, fmt.Errorf("git remote does not identify a GitLab namespace and project")
	}
	lastSlash := strings.LastIndex(projectPath, "/")
	return repository.Repository{Host: host, Owner: projectPath[:lastSlash], Name: projectPath[lastSlash+1:]}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
