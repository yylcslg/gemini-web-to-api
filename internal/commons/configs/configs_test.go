package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCookieValueExtractsAuthFromFullHeader(t *testing.T) {
	header := "NID=rollout; __Secure-1PSID=account=value; __Secure-1PSIDTS=timestamp; SIDCC=flags"
	if got := cookieValue(header, "__Secure-1PSID"); got != "account=value" {
		t.Fatalf("1PSID = %q", got)
	}
	if got := cookieValue(header, "__Secure-1PSIDTS"); got != "timestamp" {
		t.Fatalf("1PSIDTS = %q", got)
	}
}

func TestLoadRejectsInvalidGeminiAuthUser(t *testing.T) {
	path := writeConfig(t, `
gemini:
  auth_user: account-two
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "gemini.auth_user") {
		t.Fatalf("expected invalid account slot error, got %v", err)
	}
}

func TestLoadAllowsMissingGeminiCookies(t *testing.T) {
	// Accounts are added through POST /v1/addAccount, so the server must start
	// without gemini.cookies.
	path := writeConfig(t, `
gemini:
  cookies: ""
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("expected startup without gemini.cookies, got %v", err)
	}

	path = writeConfig(t, `
gemini:
  cookies: "NID=rollout; __Secure-1PSID=psid"
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "__Secure-1PSIDTS") {
		t.Fatalf("expected incomplete Cookie header error, got %v", err)
	}
}

func TestLoadReadsGeminiProxy(t *testing.T) {
	path := writeConfig(t, `
gemini:
  cookies: "__Secure-1PSID=psid; __Secure-1PSIDTS=psidts"
  proxy: " http://127.0.0.1:8889 "
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gemini.Proxy != "http://127.0.0.1:8889" {
		t.Fatalf("Gemini proxy = %q", cfg.Gemini.Proxy)
	}
}

func TestLoadDerivesAuthenticationFromGeminiCookies(t *testing.T) {
	path := writeConfig(t, `
gemini:
  cookies: "NID=rollout; __Secure-1PSID=psid; __Secure-1PSIDTS=psidts"
  auth_user: "2"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gemini.Secure1PSID != "psid" || cfg.Gemini.Secure1PSIDTS != "psidts" || cfg.Gemini.AuthUser != "2" {
		t.Fatalf("incorrect Gemini config derived from full Cookie header: %#v", cfg.Gemini)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, "log_level: debug\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != defaultServerPort || cfg.Gemini.AccountDir != defaultGeminiAccountDir ||
		cfg.Gemini.RefreshInterval != defaultGeminiRefreshInterval || cfg.Gemini.MaxRetries != defaultGeminiMaxRetries {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
}
