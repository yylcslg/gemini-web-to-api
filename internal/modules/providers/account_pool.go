package providers

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"gemini-web-to-api/internal/commons/configs"

	"go.uber.org/zap"
)

// ErrNoAvailableAccount is returned when the pool has no client to serve a
// request, for example before any account finished initializing.
var ErrNoAvailableAccount = errors.New("no Gemini account available")

// AccountPool owns one Client per account and randomly picks a client for each
// request. Each Client keeps its own HTTP client, cookie jar and session state,
// so account credentials are never mixed.
type AccountPool struct {
	cfg   *configs.Config
	store *AccountStore
	log   *zap.Logger

	mu            sync.RWMutex
	clients       []*Client
	clientsByName map[string]*Client
	jobOwners     map[string]*Client
}

func NewAccountPool(store *AccountStore, cfg *configs.Config, log *zap.Logger) (*AccountPool, error) {
	pool := &AccountPool{
		cfg:           cfg,
		store:         store,
		log:           log,
		clientsByName: make(map[string]*Client),
		jobOwners:     make(map[string]*Client),
	}

	accounts, err := store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load Gemini accounts: %w", err)
	}

	seenPSIDs := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if psid := cookieValueFromHeader(account.Cookies, "__Secure-1PSID"); psid != "" {
			seenPSIDs[psid] = true
		}
		pool.addClient(NewAccountClient(account, cfg, log))
	}

	// Keep GEMINI_COOKIES working as a single-account fallback.
	if strings.TrimSpace(cfg.Gemini.Cookies) != "" {
		psid := cookieValueFromHeader(cfg.Gemini.Cookies, "__Secure-1PSID")
		if !seenPSIDs[psid] {
			pool.addClient(NewAccountClient(Account{
				Name:     "env",
				AuthUser: strings.TrimSpace(cfg.Gemini.AuthUser),
				Cookies:  cfg.Gemini.Cookies,
				Enabled:  true,
			}, cfg, log))
		}
	}

	if pool.Len() == 0 {
		log.Warn("No Gemini account configured; add one with POST /v1/addAccount",
			zap.String("account_dir", cfg.Gemini.AccountDir))
	}
	log.Info("Gemini account pool loaded", zap.Int("accounts", pool.Len()), zap.String("account_dir", cfg.Gemini.AccountDir))

	return pool, nil
}

// NewTestAccountPool builds a pool around existing clients. It is for tests in
// other packages.
func NewTestAccountPool(clients ...*Client) *AccountPool {
	return &AccountPool{
		log:           zap.NewNop(),
		clients:       clients,
		clientsByName: make(map[string]*Client),
		jobOwners:     make(map[string]*Client),
	}
}

func (p *AccountPool) addClient(client *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = append(p.clients, client)
	p.clientsByName[client.accountName] = client
}

// Len returns the number of registered accounts.
func (p *AccountPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

func (p *AccountPool) snapshot() []*Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]*Client(nil), p.clients...)
}

// pick prefers a healthy account and falls back to any account when none is
// healthy yet, so startup requests still have a chance to run.
func (p *AccountPool) pick() *Client {
	clients := p.snapshot()
	if len(clients) == 0 {
		return nil
	}
	healthy := make([]*Client, 0, len(clients))
	for _, client := range clients {
		if client.IsHealthy() {
			healthy = append(healthy, client)
		}
	}
	if len(healthy) > 0 {
		return healthy[rand.IntN(len(healthy))]
	}
	return clients[rand.IntN(len(clients))]
}

func (p *AccountPool) pickWithModels() *Client {
	clients := p.snapshot()
	var withModels []*Client
	for _, client := range clients {
		if len(client.ListModels()) > 0 {
			withModels = append(withModels, client)
		}
	}
	if len(withModels) > 0 {
		return withModels[rand.IntN(len(withModels))]
	}
	return p.pick()
}

// Init initializes every account concurrently. Individual failures are logged
// but do not fail startup; the account is skipped until the next refresh.
func (p *AccountPool) Init(ctx context.Context) error {
	clients := p.snapshot()
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			if err := client.Init(ctx); err != nil {
				p.log.Error("Gemini account initialization failed",
					zap.String("account", client.accountName),
					zap.Error(err),
					zap.String("tip", "check that __Secure-1PSID/__Secure-1PSIDTS are fresh and auth_user matches /u/<n>/"),
				)
				p.disableAccount(client, ctx, err)
				return
			}
			p.log.Info("Gemini account initialized", zap.String("account", client.accountName))
		}(client)
	}
	wg.Wait()
	return nil
}

// UpsertAccount writes the account file, then adds the account to the pool or
// refreshes the existing client in place. File contents are fully overwritten.
func (p *AccountPool) UpsertAccount(email, cookie, authUser string) (bool, error) {
	if p.cfg == nil || p.store == nil {
		return false, errors.New("account pool has no configuration")
	}
	account, created, err := p.store.Upsert(email, cookie, authUser)
	if err != nil {
		return false, err
	}

	p.mu.Lock()
	existing := p.clientsByName[account.Name]
	p.mu.Unlock()

	target := existing
	if target == nil {
		target = NewAccountClient(account, p.cfg, p.log)
		p.addClient(target)
	} else {
		target.UpdateCredentials(account.Cookies, account.AuthUser)
	}
	p.initAsync(target)
	return created, nil
}

func (p *AccountPool) initAsync(client *Client) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := client.Init(ctx); err != nil {
			p.log.Warn("Added Gemini account failed to initialize",
				zap.String("account", client.accountName), zap.Error(err))
			p.disableAccount(client, ctx, err)
			return
		}
		p.log.Info("Added Gemini account initialized", zap.String("account", client.accountName))
	}()
}

// disableAccount persists enabled=false after an initialization failure so the
// account is skipped on the next start. Shutdown cancellations are ignored.
func (p *AccountPool) disableAccount(client *Client, ctx context.Context, cause error) {
	if ctx.Err() != nil || p.store == nil {
		return
	}
	if err := p.store.SetEnabled(client.accountName, false); err != nil {
		p.log.Warn("Failed to disable Gemini account file",
			zap.String("account", client.accountName), zap.Error(err))
		return
	}
	p.log.Warn("Gemini account file disabled",
		zap.String("account", client.accountName), zap.Error(cause))
}

func (p *AccountPool) Close() error {
	for _, client := range p.snapshot() {
		if err := client.Close(); err != nil {
			p.log.Error("Failed to close Gemini account", zap.String("account", client.accountName), zap.Error(err))
		}
	}
	return nil
}

func (p *AccountPool) GetName() string { return "gemini" }

func (p *AccountPool) IsHealthy() bool {
	for _, client := range p.snapshot() {
		if client.IsHealthy() {
			return true
		}
	}
	return false
}

func (p *AccountPool) ListModels() []ModelInfo {
	client := p.pickWithModels()
	if client == nil {
		return []ModelInfo{}
	}
	return client.ListModels()
}

func (p *AccountPool) ResolveModel(requested string) (ModelInfo, error) {
	client := p.pickWithModels()
	if client == nil {
		return ModelInfo{}, ErrNoAvailableAccount
	}
	return client.ResolveModel(requested)
}

func (p *AccountPool) GenerateContent(ctx context.Context, prompt string, options ...GenerateOption) (*Response, error) {
	client := p.pick()
	if client == nil {
		return nil, ErrNoAvailableAccount
	}
	return client.GenerateContent(ctx, prompt, options...)
}

func (p *AccountPool) StartChat(options ...ChatOption) ChatSession {
	client := p.pick()
	if client == nil {
		return unavailableChatSession{}
	}
	return client.StartChat(options...)
}

func (p *AccountPool) GenerateMusic(ctx context.Context, prompt, model string, config MusicConfig) (*MusicResult, error) {
	client := p.pick()
	if client == nil {
		return nil, ErrNoAvailableAccount
	}
	return client.GenerateMusic(ctx, prompt, model, config)
}

func (p *AccountPool) DeepResearch(ctx context.Context, query string, opts ...DeepResearchOption) (*DeepResearchResult, error) {
	client := p.pick()
	if client == nil {
		return nil, ErrNoAvailableAccount
	}
	return client.DeepResearch(ctx, query, opts...)
}

func (p *AccountPool) DeepResearchStream(ctx context.Context, query string, cb ProgressCallback, opts ...DeepResearchOption) error {
	client := p.pick()
	if client == nil {
		return ErrNoAvailableAccount
	}
	return client.DeepResearchStream(ctx, query, cb, opts...)
}

// Pick returns a random healthy account client. Callers that run a multi-step
// flow (for example generate image then resolve its URL) should pick once and
// reuse the same client so the session stays consistent.
func (p *AccountPool) Pick() *Client {
	return p.pick()
}

// StartVideoJob starts a job on a random account and remembers which client
// owns it, so polling and downloads reach the same account.
func (p *AccountPool) StartVideoJob(prompt, model string, aspect VideoAspectRatio) (VideoJob, error) {
	client := p.pick()
	if client == nil {
		return VideoJob{}, ErrNoAvailableAccount
	}
	job, err := client.StartVideoJob(prompt, model, aspect)
	if err != nil {
		return VideoJob{}, err
	}
	p.mu.Lock()
	p.jobOwners[job.ID] = client
	p.mu.Unlock()
	return job, nil
}

func (p *AccountPool) GetVideoJob(id string) (VideoJob, error) {
	client := p.videoJobOwner(id)
	if client == nil {
		return VideoJob{}, ErrVideoJobNotFound
	}
	return client.GetVideoJob(id)
}

func (p *AccountPool) VideoJobContent(id string) (VideoJob, []byte, error) {
	client := p.videoJobOwner(id)
	if client == nil {
		return VideoJob{}, nil, ErrVideoJobNotFound
	}
	return client.VideoJobContent(id)
}

func (p *AccountPool) DeleteVideoJob(id string) error {
	client := p.videoJobOwner(id)
	if client == nil {
		return ErrVideoJobNotFound
	}
	if err := client.DeleteVideoJob(id); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.jobOwners, id)
	p.mu.Unlock()
	return nil
}

func (p *AccountPool) ListVideoJobs() []VideoJob {
	var jobs []VideoJob
	for _, client := range p.snapshot() {
		jobs = append(jobs, client.ListVideoJobs()...)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs
}

func (p *AccountPool) videoJobOwner(id string) *Client {
	p.mu.RLock()
	owner := p.jobOwners[id]
	clients := append([]*Client(nil), p.clients...)
	p.mu.RUnlock()
	if owner != nil {
		return owner
	}
	for _, client := range clients {
		if _, err := client.GetVideoJob(id); err != nil {
			continue
		}
		p.mu.Lock()
		p.jobOwners[id] = client
		p.mu.Unlock()
		return client
	}
	return nil
}

type unavailableChatSession struct{}

func (unavailableChatSession) SendMessage(context.Context, string, ...GenerateOption) (*Response, error) {
	return nil, ErrNoAvailableAccount
}

func (unavailableChatSession) GetMetadata() *SessionMetadata { return nil }
func (unavailableChatSession) GetHistory() []Message         { return nil }
func (unavailableChatSession) Clear()                        {}
