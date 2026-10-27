package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func TestReposListsAddsAndRemoves(t *testing.T) {
	path := write(t, "[jira]\nsite = \"s\"\n\n[repos]\nadmin = \"/r/admin\"\n")
	repos := NewRepos(path, map[string]string{"admin": "/r/admin"})

	list := repos.List()
	list["leak"] = "/x"
	if _, ok := repos.List()["leak"]; ok {
		t.Fatal("List should return a copy")
	}
	if err := repos.Add("costs", "/r/costs"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Add("costs", "/r/costs"); err == nil {
		t.Error("adding a listed repo should fail")
	}
	if err := repos.Remove("admin"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Remove("admin"); err == nil {
		t.Error("removing an unlisted repo should fail")
	}
	if got := repos.List(); len(got) != 1 || got["costs"] != "/r/costs" {
		t.Errorf("repos = %v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `costs = "/r/costs"`) || strings.Contains(string(data), "admin") {
		t.Errorf("file = %s", data)
	}
}

func TestReposNilMap(t *testing.T) {
	path := write(t, "[jira]\nsite = \"s\"\n")
	repos := NewRepos(path, nil)
	if err := repos.Add("admin", "/r/admin"); err != nil {
		t.Fatal(err)
	}
	if got := repos.List(); got["admin"] != "/r/admin" {
		t.Errorf("repos = %v", got)
	}
}

// Run with -race: Add and Remove change the map while List reads it.
func TestReposConcurrentAccess(t *testing.T) {
	path := write(t, "[jira]\nsite = \"s\"\n")
	repos := NewRepos(path, nil)
	var wg sync.WaitGroup
	for i := range 4 {
		name := "repo" + strconv.Itoa(i)
		wg.Go(func() {
			for range 20 {
				if err := repos.Add(name, "/r/"+name); err != nil {
					t.Error(err)
					return
				}
				if err := repos.Remove(name); err != nil {
					t.Error(err)
					return
				}
			}
		})
		wg.Go(func() {
			for range 200 {
				for n, dir := range repos.List() {
					_ = n + dir
				}
			}
		})
	}
	wg.Wait()
	if got := repos.List(); len(got) != 0 {
		t.Errorf("repos = %v", got)
	}
}

func TestClaudeDirDefaultsThenEnvThenConfig(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv(ConfigDirEnv, "")
	c := NewClaudeDir(write(t, "[jira]\n"), "")
	if got := c.Dir(); got != filepath.Join(home, ".claude") {
		t.Errorf("default = %s", got)
	}
	t.Setenv(ConfigDirEnv, "~/.claude-team")
	if got := c.Dir(); got != filepath.Join(home, ".claude-team") {
		t.Errorf("env = %s", got)
	}
	if got := NewClaudeDir("", "/set").Dir(); got != "/set" {
		t.Errorf("config = %s", got)
	}
}

func TestClaudeDirSetWritesTheConfig(t *testing.T) {
	t.Setenv(TokenEnv, "secret")
	path := write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\n\n[repos]\na = \"/a\"\n")
	c := NewClaudeDir(path, "")
	if err := c.Set(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("Set took a missing folder")
	}
	if err := c.Set(""); err == nil {
		t.Fatal("Set took an empty folder")
	}
	for range 2 {
		dir := t.TempDir()
		if err := c.Set(dir); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if c.Dir() != dir || cfg.Claude.ConfigDir != dir || cfg.Repos["a"] != "/a" {
			t.Fatalf("dir = %s, cfg = %+v", c.Dir(), cfg)
		}
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "config_dir"); n != 1 {
		t.Fatalf("config_dir written %d times:\n%s", n, data)
	}
}

func TestLoadNotSetUp(t *testing.T) {
	t.Setenv(TokenEnv, "")
	for name, path := range map[string]string{
		"missing file": filepath.Join(t.TempDir(), "nope.toml"),
		"no token":     write(t, "[jira]\nsite = \"s\"\nemail = \"e\"\n"),
	} {
		if _, err := Load(path); !errors.Is(err, ErrNotSetUp) {
			t.Errorf("%s: err = %v, want ErrNotSetUp", name, err)
		}
	}
}

func TestSetJiraMakesTheFile(t *testing.T) {
	t.Setenv(TokenEnv, "")
	path := filepath.Join(t.TempDir(), "refine", "config.toml")
	if err := SetJira(path, "s", "e", "secret"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if want := "[jira]\nsite = \"s\"\nemail = \"e\"\ntoken = \"secret\"\n"; string(data) != want {
		t.Errorf("file =\n%s\nwant\n%s", data, want)
	}
	if cfg, err := Load(path); err != nil || cfg.Jira.Token != "secret" {
		t.Errorf("token = %q err = %v", cfg.Jira.Token, err)
	}
}

func TestSetJiraKeepsTheRestOfTheFile(t *testing.T) {
	path := write(t, `# my config
[jira]
site = "old"
sprint = "Mine"

[repos]
dating = "~/dating"
`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetJira(path, "s", "e", "secret"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := `# my config
[jira]
site = "s"
sprint = "Mine"
email = "e"
token = "secret"

[repos]
dating = "~/dating"
`
	if string(data) != want {
		t.Errorf("file =\n%s\nwant\n%s", data, want)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}
