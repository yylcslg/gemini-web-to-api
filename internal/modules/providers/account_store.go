package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gemini-web-to-api/internal/commons/configs"

	"go.uber.org/zap"
)

// Account is one Gemini Web login stored under the accounts directory.
type Account struct {
	Name     string
	AuthUser string
	Cookies  string
	Proxy    string
	Enabled  bool
}

// AccountValidationError marks invalid add-account input so the HTTP layer can
// answer 400 instead of 500.
type AccountValidationError struct {
	Message string
}

func (e *AccountValidationError) Error() string { return e.Message }

type accountFile struct {
	Name     string `json:"name"`
	AuthUser string `json:"auth_user"`
	Cookies  string `json:"cookies"`
	Proxy    string `json:"proxy"`
	Enabled  *bool  `json:"enabled"`
}

// AccountStore reads and writes one JSON file per account.
type AccountStore struct {
	dir string
	log *zap.Logger
	mu  sync.Mutex
}

func NewAccountStore(cfg *configs.Config, log *zap.Logger) *AccountStore {
	return &AccountStore{dir: cfg.Gemini.AccountDir, log: log}
}

// LoadAll returns every account file in the store's directory. A missing
// directory means no accounts, not an error.
func (s *AccountStore) LoadAll() ([]Account, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	accounts := make([]Account, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			s.log.Warn("Failed to read account file", zap.String("file", path), zap.Error(err))
			continue
		}
		var file accountFile
		if err := json.Unmarshal(data, &file); err != nil {
			s.log.Warn("Failed to parse account file", zap.String("file", path), zap.Error(err))
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if file.Name != "" {
			name = file.Name
		}
		enabled := true
		if file.Enabled != nil {
			enabled = *file.Enabled
		}
		accounts = append(accounts, Account{
			Name:     name,
			AuthUser: strings.TrimSpace(file.AuthUser),
			Cookies:  file.Cookies,
			Proxy:    strings.TrimSpace(file.Proxy),
			Enabled:  enabled,
		})
	}

	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	return accounts, nil
}

// Upsert writes the account file for email, replacing the whole file when it
// already exists. It returns whether the file was newly created.
func (s *AccountStore) Upsert(email, cookie, authUser string) (Account, bool, error) {
	email, err := normalizeAccountEmail(email)
	if err != nil {
		return Account{}, false, err
	}
	cookie, err = validateAccountCookie(cookie)
	if err != nil {
		return Account{}, false, err
	}
	authUser, err = normalizeAuthUser(authUser)
	if err != nil {
		return Account{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return Account{}, false, fmt.Errorf("create account directory: %w", err)
	}

	path := filepath.Join(s.dir, email+".json")
	created := true
	if _, err := os.Stat(path); err == nil {
		created = false
	} else if !errors.Is(err, os.ErrNotExist) {
		return Account{}, false, fmt.Errorf("stat account file: %w", err)
	}

	enabled := true
	file := accountFile{
		Name:     email,
		AuthUser: authUser,
		Cookies:  cookie,
		Proxy:    "",
		Enabled:  &enabled,
	}
	if err := writeAccountFile(path, file); err != nil {
		return Account{}, false, err
	}

	return Account{Name: email, AuthUser: authUser, Cookies: cookie, Enabled: true}, created, nil
}

// SetEnabled flips the enabled flag of an existing account file while keeping
// the rest of its contents. Missing files (for example the legacy env account)
// are ignored.
func (s *AccountStore) SetEnabled(name string, enabled bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path, file, err := s.findAccountFile(name)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	file.Enabled = &enabled
	return writeAccountFile(path, file)
}

func (s *AccountStore) findAccountFile(name string) (string, accountFile, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", accountFile{}, nil
	}
	if err != nil {
		return "", accountFile{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file accountFile
		if err := json.Unmarshal(data, &file); err != nil {
			continue
		}
		baseName := strings.TrimSuffix(entry.Name(), ".json")
		if file.Name == name || baseName == name {
			return path, file, nil
		}
	}
	return "", accountFile{}, nil
}

func writeAccountFile(path string, file accountFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode account file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".account-*.json")
	if err != nil {
		return fmt.Errorf("create temporary account file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set account file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write account file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync account file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close account file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace account file: %w", err)
	}
	return nil
}

func normalizeAccountEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", &AccountValidationError{Message: "email is required"}
	}
	if len(email) > 254 || strings.ContainsAny(email, "/\\\x00\r\n") || strings.Contains(email, "..") {
		return "", &AccountValidationError{Message: "email contains characters that cannot be used as a file name"}
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Name != "" || parsed.Address != email {
		return "", &AccountValidationError{Message: "email is invalid"}
	}
	return email, nil
}

func validateAccountCookie(cookie string) (string, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return "", &AccountValidationError{Message: "cookie is required"}
	}
	if strings.ContainsAny(cookie, "\r\n") {
		return "", &AccountValidationError{Message: "cookie must be a single line"}
	}
	if cookieValueFromHeader(cookie, "__Secure-1PSID") == "" || cookieValueFromHeader(cookie, "__Secure-1PSIDTS") == "" {
		return "", &AccountValidationError{Message: "cookie must contain __Secure-1PSID and __Secure-1PSIDTS"}
	}
	return cookie, nil
}

func normalizeAuthUser(authUser string) (string, error) {
	authUser = strings.TrimSpace(authUser)
	if authUser == "" {
		return "", nil
	}
	if n, err := strconv.Atoi(authUser); err != nil || n < 0 {
		return "", &AccountValidationError{Message: "auth_user must be a non-negative account slot"}
	}
	return authUser, nil
}

func cookieValueFromHeader(header, wanted string) string {
	for _, pair := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(name) == wanted {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
