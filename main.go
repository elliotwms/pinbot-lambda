package main

import (
	"encoding/hex"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/elliotwms/pinbot/internal/pinbot"
)

func init() {
	if strings.ToLower(os.Getenv("DEBUG")) == "true" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}

// Version describes the build version
// it should be set via ldflags when building
var Version = "v0.0.0+unknown"

func main() {
	k, err := hex.DecodeString(os.Getenv("DISCORD_BOT_PUBLIC_KEY"))
	if err != nil {
		panic(err)
	}

	logger := slog.Default().With(slog.String("version", Version))

	src := sessionprovider.Cached(sessionprovider.ParamStore(
		os.Getenv("PARAM_DISCORD_TOKEN"),
	))
	h := pinbot.New(k, src, logger)

	lambda.StartWithOptions(h.Handle)
}
