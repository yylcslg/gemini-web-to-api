package openai

import (
	"github.com/gofiber/fiber/v3"
	"go.uber.org/fx"
)

var Module = fx.Options(
	fx.Provide(NewOpenAIService),
	fx.Provide(NewOpenAIController),
	fx.Invoke(RegisterRoutes),
)

func RegisterRoutes(app *fiber.App, c *OpenAIController) {
	// Image links are also fetched with HEAD by prefetchers and bots, so both
	// methods must be registered; only GET routes would answer HEAD with 404.
	app.Get("/data/images/:id", c.HandleGetImage)
	app.Head("/data/images/:id", c.HandleGetImage)
	app.Get("/data/images/:date/:id", c.HandleGetImage)
	app.Head("/data/images/:date/:id", c.HandleGetImage)
	openaiV1 := app.Group("/v1")
	c.Register(openaiV1)
}
