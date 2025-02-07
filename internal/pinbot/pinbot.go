package pinbot

import (
	"log/slog"

	"crypto/ed25519"
	"github.com/elliotwms/bot-lambda"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"github.com/elliotwms/bot/interactions/router"
	"github.com/elliotwms/pinbot/internal/handlers"
)

func New(k ed25519.PublicKey, s sessionprovider.Provider, l *slog.Logger) *bot_lambda.Endpoint {
	r := router.New(
		router.WithLogger(l),
		router.WithDeferredResponse(true),
	)

	e := bot_lambda.
		New(k, bot_lambda.WithLogger(l), bot_lambda.WithRouter(r)).
		WithSessionProvider(s).
		WithMessageApplicationCommand("Pin", handlers.PinMessageCommandHandler)

	return e
}
