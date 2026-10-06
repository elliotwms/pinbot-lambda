# Pinbot ƛ

A Discord bot with one message command, "Pin". It runs as an AWS Lambda function behind a public function URL. When someone uses the command, Pinbot reposts the message as an embed into a pins channel, reacts with 📌, and edits its reply with a link to the pin.

## Layout

- `main.go`: Lambda entrypoint. Reads `DISCORD_BOT_PUBLIC_KEY`, `PARAM_DISCORD_TOKEN`, `STACK` and `DEBUG` from the environment, and logs JSON. `Version` is set with `-ldflags` at build time.
- `internal/pinbot`: wires up the [bot-lambda](https://github.com/elliotwms/bot-lambda) endpoint. It sends the deferred response, verifies the ed25519 signature and gets the bot session from Parameter Store.
  - The endpoint's handler is `HandleInvocation`, which takes both interactions from the function URL and tasks invoked directly: `{"task":"register_commands"}` and `{"task":"report_metrics"}`.
- `internal/metrics`: records CloudWatch metrics in the embedded metric format, as JSON log lines in namespace `Pinbot` with a `Stack` dimension.
  - `Pins` has an `Outcome` dimension.
  - `Guilds` and `UserInstalls` are recorded hourly by `report_metrics`.
  - Keep dimensions low-cardinality; IDs such as `guild_id` go in properties.
- `internal/handlers/pin.go`: the Pin command, where all the bot's logic lives.
- `internal/handlers/pin_test.go`: unit tests. HTTP calls are faked by swapping `discordgo.Endpoint*` for an `httptest` server.
- `tests/`: integration tests against [fakediscord](https://github.com/elliotwms/fakediscord). They're written as given/when/then stages in `pin_stage_test.go`.

## Commands

```sh
go vet ./...
go test -race ./internal/...          # unit tests, no dependencies
docker compose up -d && go test -race ./...  # also runs the integration tests against fakediscord on :8080
golangci-lint run                     # CI uses the latest version; needs a build that supports the go.mod Go version
```

Build the Lambda as CI does: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap .`

## Behaviour to preserve

- **Target channel order:** `#<channel>-pins`, then `#pins`, then the source channel. Threads look up pins channels by their parent's name and fall back to the thread itself.
- **Don't leak pins** (`canPinTo`). Never pin from an NSFW channel to a non-NSFW one. Never pin from a private thread, or a channel hidden from `@everyone`, to a channel that isn't hidden too. Any change to channel selection needs a matching case in `TestGetTargetChannel_DoesNotLeakMessages`.
- **Discord limits:** at most 10 embeds per message (`maxEmbeds`) and 6000 characters across them (`maxEmbedsLength`). The first embed (the pinned message itself) is always kept.
- **Duplicate check:** a message counts as already pinned if the bot has reacted with 📌. `isAlreadyPinned` pages through every reaction, 100 users at a time.
- **Errors to the user:** reply through `respond`/`respondError`, which edit the deferred, ephemeral response. A 403 means a permissions problem, so tell the user that rather than asking them to retry. Log failed Discord calls with `logAPIError`, which logs 403s as warnings: they're the server's permissions to fix, and `ERROR` lines fire the logged-errors alarm.

## Conventions

- Commit messages and PR titles use [Conventional Commits](https://www.conventionalcommits.org). Every merge to `master` runs `release.yml`, which works out the next version from them, creates a GitHub release and deploys it. PRs are squash-merged, so the PR title becomes the commit message.
- Dependabot patch and minor updates are approved and auto-merged by `dependabot_reviewer.yml`, using a GitHub App token whose credentials come from [elliotwms/infra](https://github.com/elliotwms/infra).
- The `Pin` command's definition is `handlers.PinCommand`. `deploy.yml` registers it after each deploy by invoking the function with the `register_commands` task, which overwrites the app's global commands. Commands not defined in code are deleted.

## Deployment

`deploy.yml` builds once, deploys to the `test` GitHub environment, then to `prod`. Each environment's `AWS_ROLE_ARN` variable is a `pinbot-deploy-<stack>` role, assumed over OIDC. The function, roles and parameters are managed in [elliotwms/infra-pinbot](https://github.com/elliotwms/infra-pinbot); this repo only updates the function code. To roll back, run `deploy.yml` manually with an older tag.
