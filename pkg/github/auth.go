package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultHost = "github.com"

type hostConfig struct {
	OAuthToken string                `yaml:"oauth_token"`
	User       string                `yaml:"user"`
	Users      map[string]userConfig `yaml:"users"`
}

type userConfig struct {
	OAuthToken string `yaml:"oauth_token"`
}

func resolveAuth(ctx context.Context) (string, string) {
	cfg := readHostsConfig()
	host := resolveHost(cfg)

	return host, resolveToken(ctx, cfg, host)
}

func resolveHost(cfg map[string]hostConfig) string {
	if host := strings.TrimSpace(os.Getenv("GH_HOST")); host != "" {
		return strings.ToLower(host)
	}

	if len(cfg) == 1 {
		for host := range cfg {
			return strings.ToLower(host)
		}
	}

	return defaultHost
}

func resolveToken(ctx context.Context, cfg map[string]hostConfig, host string) string {
	if isEnterprise(host) {
		if token := firstEnv("GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"); token != "" {
			return token
		}
	} else {
		if token := firstEnv("GH_TOKEN", "GITHUB_TOKEN"); token != "" {
			return token
		}
	}

	if entry, ok := cfg[host]; ok {
		if entry.OAuthToken != "" {
			return entry.OAuthToken
		}

		if userEntry, ok := entry.Users[entry.User]; ok && userEntry.OAuthToken != "" {
			return userEntry.OAuthToken
		}
	}

	return tokenFromGHCLI(ctx, host)
}

func isEnterprise(host string) bool {
	h := strings.ToLower(host)

	return h != defaultHost && h != "github.localhost" && !strings.HasSuffix(h, ".github.com") && !strings.HasSuffix(h, ".ghe.com")
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if val := strings.TrimSpace(os.Getenv(name)); val != "" {
			return val
		}
	}

	return ""
}

func readHostsConfig() map[string]hostConfig {
	data, err := os.ReadFile(filepath.Join(ghConfigDir(), "hosts.yml"))
	if err != nil {
		return nil
	}

	var cfg map[string]hostConfig

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	return cfg
}

func ghConfigDir() string {
	if dir := os.Getenv("GH_CONFIG_DIR"); dir != "" {
		return dir
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gh")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".config", "gh")
}

func tokenFromGHCLI(ctx context.Context, host string) string {
	ghExe := os.Getenv("GH_PATH")
	if ghExe == "" {
		var err error

		ghExe, err = exec.LookPath("gh")
		if err != nil {
			return ""
		}
	}

	out, err := exec.CommandContext(ctx, ghExe, "auth", "token", "--secure-storage", "--hostname", host).Output() //nolint:gosec
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}
