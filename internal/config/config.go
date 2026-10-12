// Package config loads ~/.config/refine/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
