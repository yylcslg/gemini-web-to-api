package providers

import (
	"context"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Options(
	fx.Provide(NewAccountStore),
	fx.Provide(NewAccountPool),
	fx.Provide(NewProviderManager),
	fx.Invoke(RegisterProvider),
)

func RegisterProvider(lc fx.Lifecycle, pm *ProviderManager, pool *AccountPool, log *zap.Logger) {
	pm.Register("gemini", pool)

	// Select Gemini as the active provider
	if err := pm.SelectProvider("gemini"); err != nil {
		log.Error("Failed to select Gemini provider", zap.Error(err))
	}

	initCtx, cancelInit := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			log.Info("Starting Gemini account initialization in background", zap.Int("accounts", pool.Len()))
			go func() {
				if err := pool.Init(initCtx); err != nil {
					log.Error("Gemini account initialization failed", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancelInit()
			return pool.Close()
		},
	})
}
