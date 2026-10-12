package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	t.Setenv(TokenEnv, "secret")
	home, _ := os.UserHomeDir()
	path := write(t, `
[jira]
site = "geekee.atlassian.net"
email = "me@example.com"

[repos]
dating = "~/creativex/dating"
"starvault.tech" = "/abs/starvault.tech"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.Token != "secret" || cfg.Jira.Sprint != DefaultSprint || cfg.Jira.ReadySprint != DefaultReadySprint {
		t.Errorf("jira = %+v", cfg.Jira)
	}
	if cfg.Repos["dating"] != filepath.Join(home, "creativex/dating") || cfg.Repos["starvault.tech"] != "/abs/starvault.tech" {
		t.Errorf("repos = %v", cfg.Repos)
	}
}

func TestLoadNeedsSiteEmailAndToken(t *testing.T) {
	t.Setenv(TokenEnv, "")
	if _, err := Load(write(t, "[jira]\n")); err == nil || !strings.Contains(err.Error(), "jira.site, jira.email") {
		t.Errorf("err = %v", err)
	}
	if _, err := Load(write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\n")); err == nil || !strings.Contains(err.Error(), TokenEnv) {
		t.Errorf("err = %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil || !strings.Contains(err.Error(), "no config at") {
		t.Errorf("err = %v", err)
	}
}

func TestTokenFromEnvFiles(t *testing.T) {
	t.Setenv(TokenEnv, "")
	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	os.WriteFile(first, []byte("# comment\nOTHER=x\n"), 0o600)
	os.WriteFile(second, []byte("export REFINE_JIRA_TOKEN = \"from-file\"\n"), 0o600)

	if got := token(filepath.Join(dir, "missing.env"), first, second); got != "from-file" {
		t.Errorf("token = %q", got)
	}

	os.WriteFile(first, []byte("REFINE_JIRA_TOKEN='first'\n"), 0o600)
	if got := token(first, second); got != "first" {
		t.Errorf("first file should win, token = %q", got)
	}

	t.Setenv(TokenEnv, "from-env")
	if got := token(first, second); got != "from-env" {
		t.Errorf("environment should win, token = %q", got)
	}
}

func TestLoadReadsEnvFileInWorkingDir(t *testing.T) {
	t.Setenv(TokenEnv, "")
	dir := t.TempDir()
	t.Chdir(dir)
	path := write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\n")
	os.WriteFile(filepath.Join(dir, ".env"), []byte("REFINE_JIRA_TOKEN=secret\n"), 0o600)

	cfg, err := Load(path)
	if err != nil || cfg.Jira.Token != "secret" {
		t.Errorf("token = %q err = %v", cfg.Jira.Token, err)
	}
}
