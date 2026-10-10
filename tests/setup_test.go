package tests

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
	pkgfakediscord "github.com/elliotwms/fakediscord/pkg/fakediscord"
)

const testGuildName = "Pinbot Integration Testing"
const testAppID = "1290742494824366183"
const testToken = "bot"
const fakediscordURL = "http://localhost:8080/"

var (
	session     *discordgo.Session
	testGuildID string
	fakediscord *pkgfakediscord.Client
)

func TestMain(m *testing.M) {
	u, err := url.Parse(fakediscordURL)
	if err != nil {
		panic(err)
	}
	// send every request for Discord to fakediscord. This is set on the default transport rather than with
	// ConfigureSession, because bot-lambda creates its own session for each interaction to send the deferred response
	http.DefaultTransport = pkgfakediscord.Transport(u, http.DefaultTransport)

	fakediscord = pkgfakediscord.NewClient(testToken).WithBaseURL(fakediscordURL)

	openSession()

	code := m.Run()

	closeSession()

	os.Exit(code)
}

func openSession() {
	var err error
	session, err = discordgo.New("Bot " + testToken)
	if err != nil {
		panic(err)
	}

	if os.Getenv("TEST_DEBUG") != "" {
		session.LogLevel = discordgo.LogDebug
		session.Debug = true
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions

	// session is used for asserting on events from fakediscord
	if err := session.Open(); err != nil {
		panic(err)
	}

	createGuild()
}

func createGuild() {
	guild, err := session.GuildCreate(testGuildName)
	if err != nil {
		panic(err)
	}

	testGuildID = guild.ID
}

func closeSession() {
	if err := session.Close(); err != nil {
		panic(err)
	}
}
