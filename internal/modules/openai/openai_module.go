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
	app.Get("/data/images/:id", c.HandleGetImage)
	openaiV1 := app.Group("/v1")
	c.Register(openaiV1)
}
