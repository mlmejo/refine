// Package config loads ~/.config/refine/config.toml.
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// TokenEnv names the environment variable that overrides the Atlassian API
// token in the config.
const TokenEnv = "REFINE_JIRA_TOKEN"

// ErrNotSetUp means the config is missing, or lacks the Jira site, email or
// token. The desktop app asks for them when it sees it.
var ErrNotSetUp = errors.New("refine is not set up")

// DefaultProject prefixes a bare ticket number typed on the home screen.
const DefaultProject = "GEEK"

// DefaultSprint is the refinement sprint the home screen lists.
const DefaultSprint = "Refinement Ready - Team John"

// DefaultReadySprint is where a ticket goes once its refinement is written.
const DefaultReadySprint = "Ready for Sprint - Team John"

// Config is the whole config file.
type Config struct {
	Jira   Jira              `toml:"jira"`
	Claude Claude            `toml:"claude"`
	Repos  map[string]string `toml:"repos"`
}

// Claude says which Claude Code config directory refine's runs use, for a
// developer who keeps one per account. "" means the default.
type Claude struct {
	ConfigDir string `toml:"config_dir"`
}

// Jira holds the Jira site and account, and the account's API token.
type Jira struct {
	Site        string `toml:"site"`
	Email       string `toml:"email"`
	Sprint      string `toml:"sprint"`
	ReadySprint string `toml:"ready_sprint"`
	Project     string `toml:"project"`
	Token       string `toml:"token"`
}

// Path returns ~/.config/refine/config.toml, on macOS too.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "refine", "config.toml"), nil
}

// StateDir returns $XDG_STATE_HOME/refine, or ~/.local/state/refine.
func StateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "refine"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "refine"), nil
}

// Load reads the config at path. TokenEnv, when set, takes the place of the
// token in the file. A file holding the token must be readable by its owner
// alone.
func Load(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("%w: no config at %s", ErrNotSetUp, path)
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	if cfg.Jira.Token != "" {
		info, err := os.Stat(path)
		if err != nil {
			return cfg, err
		}
		if info.Mode().Perm()&0o077 != 0 {
			return cfg, fmt.Errorf("%s holds the Jira token but others can read it: run chmod 600 %s", path, path)
		}
	}
	if value := os.Getenv(TokenEnv); value != "" {
		cfg.Jira.Token = value
	}
	if cfg.Jira.Sprint == "" {
		cfg.Jira.Sprint = DefaultSprint
	}
	if cfg.Jira.ReadySprint == "" {
		cfg.Jira.ReadySprint = DefaultReadySprint
	}
	if cfg.Jira.Project == "" {
		cfg.Jira.Project = DefaultProject
	}
	cfg.Claude.ConfigDir = expandHome(cfg.Claude.ConfigDir)
	for name, dir := range cfg.Repos {
		cfg.Repos[name] = expandHome(dir)
	}

	return cfg, cfg.validate(path)
}

func (c Config) validate(path string) error {
	var missing []string
	if c.Jira.Site == "" {
		missing = append(missing, "jira.site")
	}
	if c.Jira.Email == "" {
		missing = append(missing, "jira.email")
	}
	if c.Jira.Token == "" {
		missing = append(missing, "jira.token")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s: missing %s", ErrNotSetUp, path, strings.Join(missing, ", "))
	}
	return nil
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// AddRepo adds name = dir to the [repos] table of the config at path, leaving
// the rest of the file as written. A dir under the home directory is written
// with ~.
func AddRepo(path, name, dir string) error {
	return editRepos(path, func(lines []string, start, end int) ([]string, error) {
		if repoLine(lines[start:end], name) >= 0 {
			return nil, fmt.Errorf("a repo named %s is already listed", name)
		}
		at := end
		for at > start && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		entry := tomlKey(name) + " = " + strconv.Quote(collapseHome(dir))
		return slices.Insert(lines, at, entry), nil
	})
}

// RemoveRepo deletes name from the [repos] table of the config at path.
func RemoveRepo(path, name string) error {
	return editRepos(path, func(lines []string, start, end int) ([]string, error) {
		i := repoLine(lines[start:end], name)
		if i < 0 {
			return nil, fmt.Errorf("no repo named %s in %s", name, path)
		}
		return slices.Delete(lines, start+i, start+i+1), nil
	})
}

// Repos are the configured repos by name, kept in step with the [repos]
// table of the config file. It is safe to use from many goroutines.
type Repos struct {
	path string
	mu   sync.RWMutex
	m    map[string]string
}

// NewRepos returns the repos m, listed in the config at path.
func NewRepos(path string, m map[string]string) *Repos {
	r := &Repos{path: path, m: maps.Clone(m)}
	if r.m == nil {
		r.m = map[string]string{}
	}
	return r
}

// List returns a copy of the repos, so the caller may keep or change it.
func (r *Repos) List() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.m)
}

// Add lists name = dir in the config file, then in r.
func (r *Repos) Add(name, dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := AddRepo(r.path, name, dir); err != nil {
		return err
	}
	r.m[name] = dir
	return nil
}

// Remove drops name from the config file, then from r.
func (r *Repos) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := RemoveRepo(r.path, name); err != nil {
		return err
	}
	delete(r.m, name)
	return nil
}

// SetJira sets jira.site, jira.email and jira.token in the config at path,
// leaving the rest of the file as written. It makes the file when there is
// none, and leaves it readable by its owner alone since it holds the token.
func SetJira(path, site, email, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return editTable(path, "jira", func(lines []string, start, end int) ([]string, error) {
		at := end
		for at > start && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		for _, kv := range [][2]string{{"site", site}, {"email", email}, {"token", token}} {
			entry := kv[0] + " = " + strconv.Quote(kv[1])
			if i := repoLine(lines[start:at], kv[0]); i >= 0 {
				lines[start+i] = entry
				continue
			}
			lines = slices.Insert(lines, at, entry)
			at++
		}
		return lines, nil
	})
}

// ConfigDirEnv is the variable Claude Code reads its config directory from.
const ConfigDirEnv = "CLAUDE_CONFIG_DIR"

// DefaultClaudeDir returns ~/.claude, Claude Code's config directory when
// nothing says otherwise.
func DefaultClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// SetClaudeDir sets claude.config_dir in the config at path, leaving the rest
// of the file as written. A dir under the home directory is written with ~.
func SetClaudeDir(path, dir string) error {
	return editTable(path, "claude", func(lines []string, start, end int) ([]string, error) {
		entry := "config_dir = " + strconv.Quote(collapseHome(dir))
		if i := repoLine(lines[start:end], "config_dir"); i >= 0 {
			lines[start+i] = entry
			return lines, nil
		}
		return slices.Insert(lines, start, entry), nil
	})
}

// ClaudeDir is the Claude Code config directory refine's runs use, kept in
// step with claude.config_dir in the config file. It is safe to use from many
// goroutines.
type ClaudeDir struct {
	path string
	mu   sync.RWMutex
	dir  string
}

// NewClaudeDir returns dir, the claude.config_dir in the config at path; ""
// when it is not set.
func NewClaudeDir(path, dir string) *ClaudeDir {
	return &ClaudeDir{path: path, dir: dir}
}

// Dir returns claude.config_dir when it is set, else ConfigDirEnv from
// refine's environment, else ~/.claude.
func (c *ClaudeDir) Dir() string {
	c.mu.RLock()
	dir := c.dir
	c.mu.RUnlock()
	if dir != "" {
		return dir
	}
	if dir := os.Getenv(ConfigDirEnv); dir != "" {
		return expandHome(dir)
	}
	dir, _ = DefaultClaudeDir()
	return dir
}

// Set writes dir as claude.config_dir in the config file, then uses it. dir
// must be a directory.
func (c *ClaudeDir) Set(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("give the folder")
	}
	dir = filepath.Clean(expandHome(dir))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := SetClaudeDir(c.path, dir); err != nil {
		return err
	}
	c.dir = dir
	return nil
}

// editRepos hands edit the file's lines and the bounds of its [repos] table.
func editRepos(path string, edit func(lines []string, start, end int) ([]string, error)) error {
	return editTable(path, "repos", edit)
}

// editTable hands edit the file's lines and the bounds of its [table] table,
// adding the table when there is none, and writes the result back once it
// still parses.
func editTable(path, table string, edit func(lines []string, start, end int) ([]string, error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines []string
	if text := strings.TrimRight(string(data), "\n"); text != "" {
		lines = strings.Split(text, "\n")
	}
	start := slices.IndexFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "["+table+"]" }) + 1
	if start == 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "["+table+"]")
		start = len(lines)
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}

	lines, err = edit(lines, start, end)
	if err != nil {
		return err
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if _, err := toml.Decode(out, &Config{}); err != nil {
		return fmt.Errorf("edit %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// repoLine returns the index of the line that sets name, or -1.
func repoLine(lines []string, name string) int {
	return slices.IndexFunc(lines, func(line string) bool {
		var entry map[string]any
		if _, err := toml.Decode(line, &entry); err != nil || len(entry) != 1 {
			return false
		}
		_, ok := entry[name]
		return ok
	})
}

// tomlKey writes name bare when TOML allows it, else quoted.
func tomlKey(name string) string {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return strconv.Quote(name)
		}
	}
	return name
}

// collapseHome is the reverse of expandHome.
func collapseHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return path
}
