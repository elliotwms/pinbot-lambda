package pinbot

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot-lambda"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"github.com/elliotwms/bot/interactions/router"
	"github.com/elliotwms/pinbot/internal/handlers"
	"github.com/elliotwms/pinbot/internal/metrics"
)

// TaskReportMetrics records the application's approximate guild and user install counts as metrics. It's run on a
// schedule.
const TaskReportMetrics = "report_metrics"

func New(k ed25519.PublicKey, s sessionprovider.Provider, l *slog.Logger, m *metrics.Metrics) *bot_lambda.Endpoint {
	e := bot_lambda.
		New(
			k,
			bot_lambda.WithLogger(l),
			bot_lambda.WithRouter(router.New(router.WithLogger(l))),
			bot_lambda.WithDeferredResponseEnabled(true),
		).
		WithSessionProvider(s).
		WithCommand(handlers.PinCommand, handlers.NewPinMessageCommandHandler(l, m)).
		WithTask(TaskReportMetrics, reportMetrics(m))

	return e
}

func reportMetrics(m *metrics.Metrics) bot_lambda.Task {
	return func(ctx context.Context, _ *bot_lambda.Endpoint, s *discordgo.Session) error {
		app, err := bot_lambda.CurrentApplication(ctx, s)
		if err != nil {
			return fmt.Errorf("get application: %w", err)
		}

		m.Record("Guilds", float64(app.ApproximateGuildCount), metrics.UnitCount, nil, nil)
		m.Record("UserInstalls", float64(app.ApproximateUserInstallCount), metrics.UnitCount, nil, nil)

		return nil
	}
}
