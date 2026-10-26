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
	if _, err := Load(write(t, "[jira]\n")); err == nil || !strings.Contains(err.Error(), "jira.site, jira.email, jira.token") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil || !strings.Contains(err.Error(), "no config at") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadReadsTheTokenFromTheFile(t *testing.T) {
	t.Setenv(TokenEnv, "")
	path := write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\ntoken = \"from-file\"\n")
	cfg, err := Load(path)
	if err != nil || cfg.Jira.Token != "from-file" {
		t.Errorf("token = %q err = %v", cfg.Jira.Token, err)
	}

	t.Setenv(TokenEnv, "from-env")
	if cfg, err := Load(path); err != nil || cfg.Jira.Token != "from-env" {
		t.Errorf("environment should win, token = %q err = %v", cfg.Jira.Token, err)
	}
}

func TestLoadRefusesATokenOthersCanRead(t *testing.T) {
	t.Setenv(TokenEnv, "")
	path := write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\ntoken = \"secret\"\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("err = %v", err)
	}

	// Without a token in it, the file may be read by anyone.
	if err := os.WriteFile(path, []byte("[jira]\nsite = \"s\"\nemail = \"e\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(TokenEnv, "from-env")
	if _, err := Load(path); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestAddAndRemoveRepoKeepTheRestOfTheFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	path := write(t, `# my config
[jira]
site = "s"

[repos]
dating = "~/creativex/dating" # front end
"starvault.tech" = "/abs/starvault.tech"

[extra]
x = 1
`)

	if err := AddRepo(path, "wannahookup", filepath.Join(home, "creativex/wannahookup")); err != nil {
		t.Fatal(err)
	}
	if err := AddRepo(path, "go.naughty", "/abs/gonaughty"); err != nil {
		t.Fatal(err)
	}
	if err := AddRepo(path, "dating", "/elsewhere"); err == nil {
		t.Error("adding a name twice succeeded")
	}
	if err := RemoveRepo(path, "starvault.tech"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRepo(path, "nope"); err == nil {
		t.Error("removing an unknown name succeeded")
	}

	data, _ := os.ReadFile(path)
	want := `# my config
[jira]
site = "s"

[repos]
dating = "~/creativex/dating" # front end
wannahookup = "~/creativex/wannahookup"
"go.naughty" = "/abs/gonaughty"

[extra]
x = 1
`
	if string(data) != want {
		t.Errorf("file =\n%s", data)
	}
}

func TestAddRepoAddsTheTable(t *testing.T) {
	path := write(t, "[jira]\nsite = \"s\"\n")
	if err := AddRepo(path, "dating", "/abs/dating"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if want := "[jira]\nsite = \"s\"\n\n[repos]\ndating = \"/abs/dating\"\n"; string(data) != want {
		t.Errorf("file =\n%s", data)
	}
}
