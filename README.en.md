<h1 align="center">AskShell</h1>

<p align="center">
  <strong>Ask AI explicitly with <code>?</code>, inside your real shell.</strong>
</p>

<p align="center">
  <a href="https://github.com/jimyag/askshell/actions/workflows/check.yaml"><img src="https://github.com/jimyag/askshell/actions/workflows/check.yaml/badge.svg" alt="Check"></a>
  <a href="https://github.com/jimyag/askshell/actions/workflows/release.yaml"><img src="https://github.com/jimyag/askshell/actions/workflows/release.yaml/badge.svg" alt="Release"></a>
</p>

<p align="center">
  <a href="#installation">Installation</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#model-configuration">Model Configuration</a> ·
  <a href="#ssh">SSH</a>
</p>

<p align="center">
  <a href="./README.md">简体中文</a> · <strong>English</strong>
</p>

---

AskShell adds an explicit AI entry point to your existing zsh, bash, or fish session.
Normal commands still run directly in the current shell. Only input beginning
with `?` or `？` is sent to the model:

```text
git status                         # run directly
? find the processes using most CPU # ask AI
？ explain the previous output       # full-width question mark also works
```

The AI may answer directly or suggest one or more commands. A suggested command
is never executed automatically. Choosing `y` only places it back into the
terminal input line, where it can still be edited or rejected.

## Core Capabilities

- **A real shell**: uses your existing zsh, bash, or fish instead of reimplementing parsing, aliases, environment variables, `cd`, or job control.
- **Explicit AI entry point**: handles only `?` and `？` requests and never guesses whether a line is natural language.
- **Confirmation before execution**: suggested commands return to the input line so the user keeps final control.
- **Session context**: full mode records zsh/fish commands, exit codes, and bounded output while long histories are compacted progressively.
- **Multiple model APIs**: prefers the OpenAI Responses API and also supports OpenAI Chat Completions-compatible and Anthropic Messages APIs.
- **Local and remote use**: serves a remote shell from the local model and context through an encrypted SSH channel.
- **English and Chinese UI**: terminal UI defaults to English and supports Chinese with `ASKSHELL_LANG=zh`.

AskShell is currently an early release supporting macOS, Linux, zsh, bash, and fish.

## Installation

AskShell requires Go 1.23+ and zsh, bash, or fish.

```bash
go install github.com/jimyag/askshell/cmd/askshell@latest
```

Raw amd64 and arm64 binaries for Linux and macOS are also available from
[GitHub Releases](https://github.com/jimyag/askshell/releases/latest).

For development installs from the source tree:

```bash
go install ./cmd/askshell
```

Make sure the Go bin directory is in `PATH`, then add the integration to your
existing shell.

For zsh, add this to `.zshrc`:

```zsh
eval "$(askshell init zsh)"
```

For bash, add this to `.bashrc`:

```bash
eval "$(askshell init bash)"
```

For fish, add this to `~/.config/fish/config.fish`:

```fish
askshell init fish | source
```

Open a new shell after updating the configuration. `askshell init` only prints
an integration script; it does not contact a model or the network.

## Quick Start

Normal commands continue to work as before:

```text
git status
cd ~/src
vim main.go
```

Use `?` or the full-width `？` to ask AI:

```text
? show the ten largest files in this directory
? explain the arguments in the previous command
?
```

In zsh and fish, both `?question` and `? question` work. AskShell only inserts
a space after a question mark typed at the beginning of the line and does not
replace the Enter binding. In bash, use the spaced form: `? question`.

Entering only `?` prompts for a question. When the model returns multiple
commands, select one and decide whether to place it into the terminal:

- `y` / `yes` / `是` / `确认` / `放行`: place it in the input line for review or editing.
- `n` / `no` / `否` / `拒绝`: reject it.

### fish example

Add the integration to `config.fish` as described above, then start full mode:

```fish
askshell shell fish
```

Run commands normally, then ask AskShell to analyze their output:

```text
$ ps -Ao pid,%cpu,comm | head
$ ? which process is using the most CPU, and what does it do
```

If the model suggests another command, entering `y` only places it in the fish
input line:

```text
Action: [y/yes] put in terminal  [n/no] reject: y
$ ps -Ao pid,%cpu,comm | sort -k2 -nr | head
```

You can still edit it. fish does not execute the command until you press Enter.

You can also start a new interactive shell with AskShell enabled:

```bash
askshell
askshell shell zsh
askshell shell bash
askshell shell fish
```

The launcher uses the existing `.zshrc`, `.bashrc`, or `config.fish` and does
not create temporary startup files. The relevant configuration file must
already contain the initialization command shown above.
It places a PTY and a user-only Unix socket in front of the real shell while
forwarding ordinary input and output unchanged.

## Context

A shell session retains its full context without a fixed turn limit. Once the
history reaches the configured threshold, it is compacted into a rolling
summary and later turns continue to accumulate.

AskShell does not replace the zsh/fish Enter widget or redirect stdout/stderr for
normal commands. Aliases, colors, streaming output, TTY detection, and
interactive programs therefore retain their native behavior. In full
`askshell` mode, the PTY copies bounded output from non-interactive zsh/fish commands
into context. Terminal display is never truncated; internal capture is limited
to 1 MiB and model context to 64 KiB. Output is discarded when further keyboard
input or an alternate screen is detected.

Lightweight shell integration records zsh/fish commands and exit codes but
cannot capture arbitrary stdout/stderr. In bash, AskShell installs neither
an Enter binding nor a DEBUG trap. Suggested commands are added to history;
normal command output is not yet included in context.

## Model Configuration

When `ASKSHELL_PROVIDER` is unset, AskShell selects a provider in this order:

1. Use the OpenAI Responses API when `QINIU_LITELLM_KEY` or `OPENAI_API_KEY` is present.
2. Use the Anthropic Messages API when `ANTHROPIC_API_KEY` is present.
3. Otherwise use a local OpenAI Chat-compatible API, defaulting to Ollama.

### OpenAI Responses API

```bash
export ASKSHELL_PROVIDER=openai-responses
export OPENAI_API_KEY=...
export ASKSHELL_MODEL=gpt-5.6-luna
```

### OpenAI Chat Completions-compatible API

This works with OpenAI, Ollama, vLLM, and other compatible services:

```bash
export ASKSHELL_PROVIDER=openai-chat
export ASKSHELL_BASE_URL=http://127.0.0.1:11434/v1
export ASKSHELL_API_KEY=ollama
export ASKSHELL_MODEL=qwen3:4b
```

### Anthropic Messages API

```bash
export ASKSHELL_PROVIDER=anthropic
export ANTHROPIC_API_KEY=...
export ASKSHELL_MODEL=claude-haiku-4-5
```

Common settings:

```text
ASKSHELL_LANG=en|zh
ASKSHELL_PROVIDER=openai-responses|openai-chat|anthropic
ASKSHELL_BASE_URL=<custom API base URL>
ASKSHELL_API_KEY=<provider API key override>
ASKSHELL_MODEL=<model name>
ASKSHELL_TIMEOUT=2m
```

For Qiniu LiteLLM:

```bash
export QINIU_LITELLM_KEY=...
export ASKSHELL_MODEL=gpt-5.6-luna
```

This selects the OpenAI Responses API at
`https://llmapi.qiniu.com/v1`.

## SSH

Connect to a remote host while keeping the model and context local:

```bash
askshell ssh server
askshell ssh p-qn
```

Use the same interaction after entering the remote shell:

```text
remote$ free -h
remote$ ? is the current memory usage healthy
```

AskShell automatically uses the remote zsh or fish installation; `askshell`
does not need to be installed remotely.

AskShell creates a reverse Unix socket forwarding and uploads a temporary
Python helper under remote `/tmp`. The helper is removed when the session exits.
API keys, conversation state, and model calls stay on the local machine.

Remote full mode currently requires zsh or fish, Python 3, `base64`, and OpenSSH
StreamLocal reverse forwarding. AskShell is not reinjected after entering a
nested SSH or tmux session.

## Security Boundaries

- AI-generated commands are never executed silently.
- Commands that delete or overwrite data, require privileges, or restart the system show an additional warning.
- A `?` request sends the operating system, shell, working directory, and relevant session context to the configured model service.
- `askshell ssh` sends remote commands, directories, and captured non-interactive output to the local process through the encrypted SSH channel.
- Do not print secrets or sensitive data in an enabled shell if they must not be sent to the model service.

## Development

```bash
task deps
task ci
```

`task lint` checks the Go source with golangci-lint, `task test` runs the race
detector, and `task build` writes the binary to `bin/askshell`.

## Release

Pushing a `v*` tag runs GoReleaser and uploads raw amd64 and arm64 binaries for
Linux and macOS together with `checksums.txt`. Run `task release-snapshot`
before publishing to validate the configuration and local artifacts.

## License

This project is licensed under the [MIT License](LICENSE).
