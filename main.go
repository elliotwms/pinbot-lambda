package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"github.com/elliotwms/pinbot/internal/metrics"
	"github.com/elliotwms/pinbot/internal/pinbot"
	"github.com/elliotwms/pinbot/internal/tracing"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
)

// Version describes the build version
// it should be set via ldflags when building
var Version = "v0.0.0+unknown"

func main() {
	stack := os.Getenv("STACK")
	if stack == "" {
		stack = "local"
	}

	level := slog.LevelInfo
	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		level = slog.LevelDebug
	}

	// JSON logs can be queried by field with Logs Insights, and errors are counted by a metric filter on $.level
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).
		With(slog.String("stack", stack), slog.String("version", Version))
	slog.SetDefault(logger)

	k, err := hex.DecodeString(os.Getenv("DISCORD_BOT_PUBLIC_KEY"))
	if err != nil {
		panic(err)
	}
	// an empty key disables request verification, and a key of the wrong size panics on verify
	if len(k) != ed25519.PublicKeySize {
		panic("DISCORD_BOT_PUBLIC_KEY must be a hex-encoded ed25519 public key")
	}

	src := sessionprovider.Cached(sessionprovider.ParamStore(
		os.Getenv("PARAM_DISCORD_TOKEN"),
	))
	m := metrics.New(os.Stdout, map[string]string{"Stack": stack})
	h := pinbot.New(k, src, logger, m)

	// HandleInvocation handles both interactions from the function URL and tasks, such as registering commands
	var handler any = h.HandleInvocation

	// tracing needs the ADOT collector layer, which infra-pinbot adds when tracing is enabled
	if strings.ToLower(os.Getenv("TRACING_ENABLED")) == "true" {
		opts, err := tracing.Setup(context.Background())
		if err != nil {
			logger.Error("Could not set up tracing", "error", err)
		} else {
			handler = otellambda.InstrumentHandler(h.HandleInvocation, opts...)
		}
	}

	lambda.StartWithOptions(handler)
}
