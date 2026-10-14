<h1 align="center">AskShell</h1>

<p align="center">
  <strong>在真实 Shell 中，用 <code>?</code> 明确地询问 AI。</strong>
</p>

<p align="center">
  <a href="https://github.com/jimyag/askshell/actions/workflows/check.yaml"><img src="https://github.com/jimyag/askshell/actions/workflows/check.yaml/badge.svg" alt="Check"></a>
  <a href="https://github.com/jimyag/askshell/actions/workflows/release.yaml"><img src="https://github.com/jimyag/askshell/actions/workflows/release.yaml/badge.svg" alt="Release"></a>
</p>

<p align="center">
  <a href="#安装">安装</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="#模型配置">模型配置</a> ·
  <a href="#ssh">SSH</a>
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="./README.en.md">English</a>
</p>

---

AskShell 为现有 zsh、bash 或 fish 增加一个显式 AI 入口。普通命令仍由当前 Shell
原样执行；只有以 `?` 或 `？` 开头的输入才会发送给模型：

```text
git status                       # 直接执行
? 找出当前 CPU 使用率最高的进程   # 询问 AI
？ 解释上一条命令的输出            # 中文输入法也支持
```

AI 可以直接回答，也可以建议一条或多条命令。建议命令不会自动执行：选择
`y` 后，它只会被放回终端输入行，仍可修改或拒绝。

## 核心能力

- **真实 Shell**：使用用户现有的 zsh、bash 或 fish，不重新实现命令解析、alias、环境变量、`cd` 或 job control。
- **显式 AI 入口**：只处理 `?` 和 `？` 请求，不猜测一行输入是命令还是自然语言。
- **执行前确认**：AI 建议先回到终端输入行，用户保留最终控制权。
- **会话上下文**：完整模式会记录 zsh/fish 命令、退出码和有界输出；长上下文会滚动压缩。
- **多模型接口**：优先支持 OpenAI Responses API，同时支持 OpenAI Chat Completions-compatible API 和 Anthropic Messages API。
- **本地与远程一致**：本机模型和上下文可通过加密的 SSH 通道服务远端 Shell。
- **中英文界面**：终端 UI 默认英文，可通过 `ASKSHELL_LANG=zh` 切换中文。

AskShell 当前是早期版本，支持 macOS、Linux、zsh、bash 和 fish。

## 安装

需要 Go 1.23+，以及 zsh、bash 或 fish。

```bash
go install github.com/jimyag/askshell/cmd/askshell@latest
```

也可以从 [GitHub Releases](https://github.com/jimyag/askshell/releases/latest)
下载 Linux 或 macOS 的 amd64/arm64 二进制。

在源码目录开发安装：

```bash
go install ./cmd/askshell
```

确保 Go bin 目录在 `PATH` 中，然后将 AskShell integration 加入现有 Shell。

zsh，在 `.zshrc` 中加入：

```zsh
eval "$(askshell init zsh)"
```

bash，在 `.bashrc` 中加入：

```bash
eval "$(askshell init bash)"
```

fish，在 `~/.config/fish/config.fish` 中加入：

```fish
askshell init fish | source
```

重新打开 Shell 后即可使用。`askshell init` 只输出初始化脚本，不访问模型或网络。

## 快速开始

普通命令仍像以前一样执行：

```text
git status
cd ~/src
vim main.go
```

使用 `?` 或全角 `？` 询问 AI：

```text
? 查看当前目录占空间最大的十个文件
? 解释上一条命令的参数
?
```

zsh 和 fish 同时支持 `?问题` 和 `? 问题`；AskShell 只在行首问号后自动补
空格，不会替换 Enter binding。bash 请使用带空格的 `? 问题`。

只输入 `?` 时，AskShell 会继续提示输入问题。AI 返回多个命令时，先选择其中
一个，再决定是否放入终端：

- `y` / `yes` / `是` / `确认` / `放行`：放入终端输入行，可继续修改。
- `n` / `no` / `否` / `拒绝`：拒绝。

在 `askshell shell zsh`、`askshell shell fish` 或 AskShell SSH 完整模式中，
上一条命令返回非零退出码时会显示诊断入口：

```text
AskShell: 上一条命令执行失败。
[D] 诊断并修复    [N] 忽略
```

可以直接点击，也可以按 `d`/Enter 诊断，或按 `n`/Ctrl-C 忽略。只有明确选择
诊断后才会请求模型。模型建议的修复命令仍只会放入当前输入行，不会自动执行。
轻量 integration 和 bash 不启用这个失败诊断入口。

### fish 使用示例

先按安装说明将 integration 加入 `config.fish`，然后启动完整模式：

```fish
askshell shell fish
```

然后像普通 Shell 一样执行命令，并让 AskShell 根据输出继续分析：

```text
$ ps -Ao pid,%cpu,comm | head
$ ? 哪个进程占用 CPU 最高，它是做什么的
```

如果 AI 建议执行新命令，输入 `y` 后，命令只会放回 fish 输入行：

```text
Action: [y/yes] put in terminal  [n/no] reject: y
$ ps -Ao pid,%cpu,comm | sort -k2 -nr | head
```

此时可以继续修改；只有按 Enter 后，fish 才会执行该命令。

也可以启动一个启用 AskShell 的新交互式 Shell：

```bash
askshell
askshell shell zsh
askshell shell bash
askshell shell fish
```

启动器直接使用用户现有的 `.zshrc`、`.bashrc` 或 `config.fish`，不会创建
临时 rc 文件，因此对应配置文件需要已经包含上面的初始化命令。
`askshell` 会在真实 Shell 前建立 PTY 和仅当前用户可访问的 Unix Socket；
普通输入输出仍原样透传。

## 上下文

同一个 Shell 会话会保留全部上下文且不限制轮数；达到长度阈值后，完整历史
会压缩为滚动摘要并继续累计后续对话。

AskShell 不替换 zsh/fish 的 Enter widget，也不重定向普通命令的 stdout/stderr。
因此 alias、颜色、流式输出、TTY 检测和交互式程序保持原生行为。在
`askshell` 完整模式中，PTY 会复制 zsh/fish 非交互命令的有界输出到上下文：
终端显示不截断，内部最多保留 1 MiB，发送给模型时最多保留 64 KiB。检测到
后续键盘输入或 alternate screen 时，输出会标记为交互式并舍弃。

仅加载 shell integration 的轻量模式仍记录 zsh/fish 命令和退出码，但无法
捕获任意 stdout/stderr。bash 通过 DEBUG trap 与 PROMPT_COMMAND 提供同等的
命令记录与失败诊断；AI 建议的命令会加入历史记录，按 `Ctrl-G` 放入当前
输入行（也可按上方向键取出）。普通命令输出暂不进入上下文。

## 模型配置

未设置 `ASKSHELL_PROVIDER` 时，AskShell 按以下顺序选择：

1. 存在 `OPENAI_API_KEY`：使用 OpenAI Responses API。
2. 存在 `ANTHROPIC_API_KEY`：使用 Anthropic Messages API。
3. 否则使用本地 OpenAI Chat-compatible API，默认连接 Ollama。

### OpenAI Responses API

```bash
export ASKSHELL_PROVIDER=openai-responses
export OPENAI_API_KEY=...
export ASKSHELL_MODEL=gpt-5.6-luna
```

### OpenAI Chat Completions-compatible API

可连接 OpenAI、Ollama、vLLM 或其他兼容服务：

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

通用配置：

```text
ASKSHELL_LANG=en|zh
ASKSHELL_PROVIDER=openai-responses|openai-chat|anthropic
ASKSHELL_BASE_URL=<custom API base URL>
ASKSHELL_API_KEY=<provider API key override>
ASKSHELL_MODEL=<model name>
ASKSHELL_TIMEOUT=2m
ASKSHELL_CONFIRM_TIMEOUT=0s    # 0 = 永不超时；如 30s 则空闲自动拒绝
ASKSHELL_PICK=1                # 非交互式预选：纯数字 / yN / eN / xN
```

## SSH

使用本地 AskShell 连接远端：

```bash
askshell ssh server
askshell ssh p-qn
```

进入远端后继续使用相同的交互方式：

```text
remote$ free -h
remote$ ? 当前内存使用是否正常
```

AskShell 会自动使用远端的 zsh 或 fish；远端不需要安装 `askshell`。

AskShell 会为本次连接建立反向 Unix Socket 转发，并向远端 `/tmp` 临时上传
一个 Python helper。远端退出后 helper 自动删除；模型 API Key、对话上下文
和模型请求始终留在本机。建议命令仍只放回远端 zsh/fish 输入行，不会自动执行。

当前远端完整模式要求远端使用 zsh 或 fish，具备 Python 3 和 `base64`，并允许 OpenSSH
StreamLocal 反向转发。嵌套进入另一层 SSH 或 tmux 后不会继续注入 AskShell。

## 安全边界

- AI 生成的命令永远不会静默执行。
- AskShell 会为常见的删除、覆盖、提权和重启命令显示额外风险提示；该提示是辅助检查，不是 Shell 安全沙箱。
- `?` 请求会把当前操作系统、Shell、工作目录和相关会话上下文发送给配置的模型服务。
- `askshell ssh` 会把远端命令、目录和捕获到的非交互输出通过 SSH 加密通道发送到本机。
- 不要在启用的 Shell 中打印不希望发送给模型服务的密钥或敏感数据。

## 开发

```bash
task deps
task ci
```

`task lint` 使用 golangci-lint 检查 Go 代码，`task test` 运行 race detector，
`task build` 将二进制写入 `bin/askshell`。

## 发布

推送 `v*` tag 后，Release workflow 会先执行 lint、测试和构建，再通过
GoReleaser 上传 Linux 和 macOS 的 amd64/arm64 原始二进制及
`checksums.txt`。发布前也可运行 `task release-snapshot` 验证配置和本地产物。

## License

本项目基于 [MIT License](LICENSE) 发布。
