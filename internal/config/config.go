// Package config loads ~/.config/refine/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// TokenEnv names the environment variable holding the Atlassian API token.
const TokenEnv = "REFINE_JIRA_TOKEN"

// DefaultProject prefixes a bare ticket number typed on the home screen.
const DefaultProject = "GEEK"

// DefaultSprint is the refinement sprint the home screen lists.
const DefaultSprint = "Refinement Ready - Team John"

// DefaultReadySprint is where a ticket goes once its refinement is written.
const DefaultReadySprint = "Ready for Sprint - Team John"

// Config is the whole config file plus the token.
type Config struct {
	Jira  Jira              `toml:"jira"`
	Repos map[string]string `toml:"repos"`
	Flags Flags             `toml:"flags"`
	// Experiments switches A/B tests on; see docs/experiments.
	Experiments Experiments `toml:"experiments"`
}

// Experiments are the A/B tests that can run. Each assigns tickets to arms.
type Experiments struct {
	// PlainNames tests plain names with a glossary against inline code in
	// findings and questions (docs/experiments/plain-names.md).
	PlainNames bool `toml:"plain_names"`
}

// TrackingEnv turns tracking on (1) or off (0) over the config's flag.
const TrackingEnv = "REFINE_TRACKING"

// Flags switch features that are on trial.
type Flags struct {
	// Tracking records how refine is used, locally in refine.db, so its
	// design can be judged from real use. Nothing leaves the machine.
	Tracking bool `toml:"tracking"`
}

// Jira holds the Jira site and account.
type Jira struct {
	Site        string `toml:"site"`
	Email       string `toml:"email"`
	Sprint      string `toml:"sprint"`
	ReadySprint string `toml:"ready_sprint"`
	Project     string `toml:"project"`
	Token       string `toml:"-"`
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

// Load reads the config at path and the token from the environment.
func Load(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("no config at %s", path)
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	cfg.Jira.Token = token(".env")
	switch os.Getenv(TrackingEnv) {
	case "1", "true":
		cfg.Flags.Tracking = true
	case "0", "false":
		cfg.Flags.Tracking = false
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
	for name, dir := range cfg.Repos {
		cfg.Repos[name] = expandHome(dir)
	}

	return cfg, cfg.validate(path)
}

// token reads TokenEnv from the environment, else from the first .env file that sets it.
func token(envFiles ...string) string {
	if value := os.Getenv(TokenEnv); value != "" {
		return value
	}
	for _, file := range envFiles {
		if value := readEnvFile(file)[TokenEnv]; value != "" {
			return value
		}
	}
	return ""
}

// readEnvFile parses KEY=VALUE lines, skipping blanks, comments and "export ".
func readEnvFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}

func (c Config) validate(path string) error {
	var missing []string
	if c.Jira.Site == "" {
		missing = append(missing, "jira.site")
	}
	if c.Jira.Email == "" {
		missing = append(missing, "jira.email")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: missing %s", path, strings.Join(missing, ", "))
	}
	if c.Jira.Token == "" {
		return fmt.Errorf("%s is not set in the environment or ./.env", TokenEnv)
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

// editRepos hands edit the file's lines and the bounds of its [repos] table,
// adding the table when there is none, and writes the result back once it
// still parses.
func editRepos(path string, edit func(lines []string, start, end int) ([]string, error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := slices.IndexFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "[repos]" }) + 1
	if start == 0 {
		lines = append(lines, "", "[repos]")
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
