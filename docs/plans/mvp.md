# AskShell MVP 实现计划

## 目标

实现一个 Go 编写的 shell wrapper：普通输入始终由真实 zsh/bash 执行；只有显式输入 `?` 时才进入 AI 交互。AI 生成的命令必须由用户选择执行、编辑或拒绝。

## 前置条件

- macOS 或 Linux
- zsh 或 bash
- OpenAI、Anthropic，或 OpenAI Chat-compatible API

## Contract

- `askshell` 不自动判断自然语言。
- 除 `?` 入口外，所有输入原样交给后端 shell。
- `askshell` 不重新实现 shell，也不在 AI 子进程里执行建议命令。
- “执行”在当前父 shell 中运行。
- “编辑”允许用户使用当前 shell 的编辑能力修改。
- “拒绝”清空本次输入。
- AI 建议永远不会静默执行。

## 步骤

1. 建立 CLI 与 provider 接口。
2. 优先支持 OpenAI Responses，同时支持 OpenAI Chat Completions 和 Anthropic Messages。
3. 实现回答和命令候选的结构化输出。
4. 实现执行、编辑、拒绝选择及父 shell 回传协议。
5. 实现 zsh ZLE、bash alias/function 与 launcher。
6. 编写本地和 SSH 使用文档。
7. 运行 `go build` 和手工 CLI 冒烟验证，不主动运行测试。

## 当前实现

- 本地完整模式使用 PTY 透传终端字节，并通过 Unix Socket 接收 zsh 生命周期事件。
- SSH 完整模式通过反向 Unix Socket 连接本机模型，远端仅使用会话级临时 helper。
- 非交互输出有界保存；交互式和 alternate-screen 输出不进入模型上下文。

## 风险

- SSH 完整模式当前要求远端使用 zsh，并具备 Python 3 和 `base64`。
- SSH 服务端可能禁用 StreamLocal forwarding。
- 不同 API 对 JSON Schema 的支持程度可能不同，但始终要求人工确认命令。
