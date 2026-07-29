package remote

const helperScript = `#!/usr/bin/env python3
import json
import os
import pathlib
import platform
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile


def rpc(request):
    path = os.environ.get("ASKSHELL_SOCKET", "")
    if not path:
        raise RuntimeError("ASKSHELL_SOCKET is not set")
    client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    client.connect(path)
    with client:
        stream = client.makefile("rwb")
        stream.write((json.dumps(request, ensure_ascii=False) + "\n").encode())
        stream.flush()
        response = json.loads(stream.readline())
    if response.get("error"):
        raise RuntimeError(response["error"])
    return response


def marker(event):
    session_id = os.environ.get("ASKSHELL_SESSION_ID", "")
    with open("/dev/tty", "wb", buffering=0) as tty:
        tty.write(f"\x1b]777;askshell;{session_id};{event}\x07".encode())


def write_result(directory, action, command=""):
    pathlib.Path(directory, "action").write_text(action + "\n")
    pathlib.Path(directory, "command").write_text(command)


def clean_choice(value):
    return (
        value.replace("\x1b[200~", "")
        .replace("\x1b[201~", "")
        .replace("^[[200~", "")
        .replace("^[[201~", "")
        .strip()
        .lower()
    )


def prompt(query):
    response = rpc(
        {
            "type": "analyze",
            "query": query,
            "shell": os.environ.get("ASKSHELL_BACKEND", pathlib.Path(os.environ.get("SHELL", "zsh")).name),
            "cwd": os.getcwd(),
            "os": platform.system().lower(),
            "arch": platform.machine().lower(),
        }
    )
    answer = response["answer"]
    chinese = os.environ.get("ASKSHELL_LANG", "").lower().startswith("zh")
    tty_in = open("/dev/tty", "r")
    tty_out = open("/dev/tty", "w", buffering=1)
    if answer.get("reasoning_summary"):
        print("\n" + ("分析摘要：" if chinese else "Reasoning summary:"), file=tty_out)
        print(answer["reasoning_summary"], file=tty_out)
    if answer.get("answer"):
        print("\n" + answer["answer"], file=tty_out)
    commands = answer.get("commands", [])
    if not commands:
        return "reject", ""
    print(file=tty_out)
    for index, candidate in enumerate(commands, 1):
        print(f"{index}. {candidate['command']}", file=tty_out)
        if candidate.get("description"):
            print(f"   {candidate['description']}", file=tty_out)
        if candidate.get("risk"):
            risks = {
                "destructive": "可能删除或覆盖数据" if chinese else "may delete or overwrite data",
                "privileged": "需要提权或影响系统状态" if chinese else "requires privileges or affects system state",
            }
            risk = risks.get(candidate["risk"])
            if risk:
                print(("   风险：" if chinese else "   Risk: ") + risk, file=tty_out)
    selected = commands[0]
    if len(commands) > 1:
        while True:
            message = (
                f"选择命令 [1-{len(commands)}]，或输入 n/否："
                if chinese
                else f"Select command [1-{len(commands)}], or n/no to reject: "
            )
            tty_out.write(message)
            tty_out.flush()
            value = clean_choice(tty_in.readline())
            if value in ("n", "no", "否", "拒绝"):
                return "reject", ""
            if value.isdigit() and 1 <= int(value) <= len(commands):
                selected = commands[int(value) - 1]
                break
            print("无效选择。" if chinese else "Invalid selection.", file=tty_out)
    while True:
        tty_out.write(
            "操作：[y/是] 放入终端  [n/否] 拒绝："
            if chinese
            else "Action: [y/yes] put in terminal  [n/no] reject: "
        )
        tty_out.flush()
        value = clean_choice(tty_in.readline())
        if value in ("y", "yes", "是", "确认", "放行"):
            return "execute", selected["command"]
        if value in ("n", "no", "否", "拒绝"):
            return "reject", ""
        print("无效选择。" if chinese else "Invalid selection.", file=tty_out)


def zsh_integration(binary):
    binary = shlex.quote(binary)
    return r'''
if [[ "${_ASKSHELL_SESSION_OWNER_PID:-}" != "$$" ]]; then
  typeset -g _ASKSHELL_SESSION_OWNER_PID="$$"
fi
if [[ -z "${ASKSHELL_ZSH_INTEGRATION:-}" ]]; then
  typeset -g ASKSHELL_ZSH_INTEGRATION=1
  typeset -gx ASKSHELL_ACTIVE=1
  typeset -gx ASKSHELL_BACKEND=zsh
  typeset -g _ASKSHELL_BIN=__BINARY__

  function _askshell_query() {
    unset _ASKSHELL_NATIVE_COMMAND
    "$_ASKSHELL_BIN" event cancel >/dev/null 2>&1
    local result_dir
    result_dir="$(command mktemp -d "${TMPDIR:-/tmp}/askshell.XXXXXXXX")" || return 1
    "$_ASKSHELL_BIN" ask --result-dir "$result_dir" -- "$*"
    local ask_status=$?
    local action="reject"
    local suggested=""
    [[ -r "$result_dir/action" ]] && action="$(<"$result_dir/action")"
    [[ -r "$result_dir/command" ]] && suggested="$(<"$result_dir/command")"
    command rm -rf -- "$result_dir"
    if (( ask_status == 0 )) && [[ "$action" == "execute" || "$action" == "edit" ]] && [[ -n "$suggested" ]]; then
      print -z -- "$suggested"
    fi
    return "$ask_status"
  }

  function _askshell_record_preexec() {
    typeset -g _ASKSHELL_NATIVE_COMMAND="$1"
    "$_ASKSHELL_BIN" event start -- "$1" >/dev/null 2>&1
  }

  function _askshell_record_native_command() {
    local exit_code=$?
    if [[ -n "${_ASKSHELL_NATIVE_COMMAND+x}" ]]; then
      unset _ASKSHELL_NATIVE_COMMAND
      "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1
    fi
    return "$exit_code"
  }

  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _askshell_record_preexec
  precmd_functions=(
    _askshell_record_native_command
    ${precmd_functions:#_askshell_record_native_command}
  )
  alias '?'='_askshell_query'
  alias '？'='_askshell_query'

  if (( $+functions[command_not_found_handler] )); then
    functions[_askshell_previous_command_not_found_handler]="$functions[command_not_found_handler]"
    typeset -g _ASKSHELL_HAS_COMMAND_NOT_FOUND_HANDLER=1
  fi

  function command_not_found_handler() {
    local command_name="$1"
    if [[ "$command_name" == ？* ]]; then
      shift
      _askshell_query "${command_name#？}" "$@"
      return $?
    fi
    if (( ${_ASKSHELL_HAS_COMMAND_NOT_FOUND_HANDLER:-0} )); then
      _askshell_previous_command_not_found_handler "$@"
      return $?
    fi
    print -u2 "zsh: command not found: $command_name"
    return 127
  }

  if [[ -o interactive ]]; then
    function _askshell_insert_question_mark() {
      if [[ -z "${LBUFFER//[[:space:]]/}" ]]; then
        LBUFFER+="${KEYS} "
      else
        LBUFFER+="$KEYS"
      fi
    }
    zle -N _askshell_insert_question_mark
    bindkey -M emacs '?' _askshell_insert_question_mark
    bindkey -M emacs '？' _askshell_insert_question_mark
    bindkey -M viins '?' _askshell_insert_question_mark
    bindkey -M viins '？' _askshell_insert_question_mark

    function _askshell_normalize_question_buffer() {
      if [[ "$BUFFER" == \?[^[:space:]]* ]]; then
        BUFFER="? ${BUFFER#\?}"
        (( CURSOR > 0 )) && (( CURSOR++ ))
      elif [[ "$BUFFER" == ？[^[:space:]]* ]]; then
        BUFFER="？ ${BUFFER#？}"
        (( CURSOR > 0 )) && (( CURSOR++ ))
      fi
    }

    autoload -Uz add-zle-hook-widget
    add-zle-hook-widget line-pre-redraw _askshell_normalize_question_buffer
  fi
fi
'''.replace("__BINARY__", binary)


def fish_quote(value):
    return "'" + value.replace("\\", "\\\\").replace("'", "\\'") + "'"


def fish_integration(binary):
    binary = fish_quote(binary)
    return r'''
if test "$_ASKSHELL_SESSION_OWNER_PID" != "$fish_pid"
    set -g _ASKSHELL_SESSION_OWNER_PID "$fish_pid"
end
if not set -q ASKSHELL_FISH_INTEGRATION
    set -g ASKSHELL_FISH_INTEGRATION 1
    set -gx ASKSHELL_ACTIVE 1
    set -gx ASKSHELL_BACKEND fish
    set -g _ASKSHELL_BIN __BINARY__

    function _askshell_query
        set -e _ASKSHELL_NATIVE_COMMAND
        "$_ASKSHELL_BIN" event cancel >/dev/null 2>&1
        set -l result_dir (command mktemp -d "/tmp/askshell.XXXXXXXX")
        or return 1
        "$_ASKSHELL_BIN" ask --result-dir "$result_dir" -- (string join ' ' -- $argv)
        set -l ask_status $status
        set -l action reject
        set -l suggested
        if test -r "$result_dir/action"
            set action (string trim -- (command cat "$result_dir/action"))
        end
        if test -r "$result_dir/command"
            set suggested (string collect <"$result_dir/command")
        end
        command rm -rf -- "$result_dir"
        if test $ask_status -eq 0
            if contains -- "$action" execute edit
                if test -n "$suggested"
                    commandline -r -- "$suggested"
                end
            end
        end
        return $ask_status
    end

    function _askshell_record_preexec --on-event fish_preexec
        set -g _ASKSHELL_NATIVE_COMMAND "$argv[1]"
        "$_ASKSHELL_BIN" event start -- "$argv[1]" >/dev/null 2>&1
    end

    function _askshell_record_postexec --on-event fish_postexec
        set -l exit_code $status
        if set -q _ASKSHELL_NATIVE_COMMAND
            set -e _ASKSHELL_NATIVE_COMMAND
            "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1
        end
        return $exit_code
    end

    function '?'
        _askshell_query $argv
    end
    function '？'
        _askshell_query $argv
    end

    if functions -q fish_command_not_found
        functions -c fish_command_not_found _askshell_previous_command_not_found
        set -g _ASKSHELL_HAS_COMMAND_NOT_FOUND_HANDLER 1
    end
    function fish_command_not_found
        set -l command_name "$argv[1]"
        set -l prefix (string sub -s 1 -l 1 -- "$command_name")
        if test "$prefix" = '?' -o "$prefix" = '？'
            _askshell_query (string sub -s 2 -- "$command_name") $argv[2..-1]
            return $status
        end
        if set -q _ASKSHELL_HAS_COMMAND_NOT_FOUND_HANDLER
            _askshell_previous_command_not_found $argv
            return $status
        end
        __fish_default_command_not_found_handler $argv
    end

    function _askshell_insert_ascii_question
        if test -z (string trim -- (commandline))
            commandline -i -- '? '
        else
            commandline -i -- '?'
        end
    end
    function _askshell_insert_fullwidth_question
        if test -z (string trim -- (commandline))
            commandline -i -- '？ '
        else
            commandline -i -- '？'
        end
    end
    if status is-interactive
        bind '?' _askshell_insert_ascii_question
        bind '？' _askshell_insert_fullwidth_question
        bind -M insert '?' _askshell_insert_ascii_question
        bind -M insert '？' _askshell_insert_fullwidth_question
    end
end
'''.replace("__BINARY__", binary)


def run_shell():
    backend = pathlib.Path(os.environ.get("SHELL", "zsh")).name
    if backend not in ("zsh", "fish"):
        raise RuntimeError(f"remote shell {backend!r} is not supported; use zsh or fish")
    helper = str(pathlib.Path(__file__).resolve())
    runtime_dir = os.environ.get("ASKSHELL_REMOTE_DIR") or tempfile.mkdtemp(prefix="askshell-")
    env = os.environ.copy()
    env["ASKSHELL_CONTROL_SESSION_ID"] = env.get("ASKSHELL_SESSION_ID", "")
    if backend == "zsh":
        original_zdotdir = env.get("ZDOTDIR")
        startup_dir = original_zdotdir or env.get("HOME", "")
        rc_dir = pathlib.Path(runtime_dir, "zdot")
        rc_dir.mkdir(mode=0o700, exist_ok=True)
        original_rc = pathlib.Path(startup_dir, ".zshrc")
        rc = pathlib.Path(rc_dir, ".zshrc")
        parts = [
            "unset ZDOTDIR"
            if original_zdotdir is None
            else f"typeset -gx ZDOTDIR={shlex.quote(original_zdotdir)}"
        ]
        if original_rc.is_file():
            parts.append(f"source {shlex.quote(str(original_rc))}")
        parts.append('typeset -gx ASKSHELL_SESSION_ID="$ASKSHELL_CONTROL_SESSION_ID"')
        parts.append("unset ASKSHELL_ZSH_INTEGRATION")
        parts.append(zsh_integration(helper))
        rc.write_text("\n".join(parts))
        env["ZDOTDIR"] = str(rc_dir)
    else:
        original_config_home = env.get("XDG_CONFIG_HOME")
        startup_config_home = original_config_home or str(
            pathlib.Path(env.get("HOME", ""), ".config")
        )
        config_home = pathlib.Path(runtime_dir, "config")
        fish_dir = pathlib.Path(config_home, "fish")
        fish_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        original_config = pathlib.Path(startup_config_home, "fish", "config.fish")
        config = pathlib.Path(fish_dir, "config.fish")
        parts = [
            "set -e XDG_CONFIG_HOME"
            if original_config_home is None
            else f"set -gx XDG_CONFIG_HOME {fish_quote(original_config_home)}"
        ]
        if original_config.is_file():
            parts.append(f"source {fish_quote(str(original_config))}")
        parts.append('set -gx ASKSHELL_SESSION_ID "$ASKSHELL_CONTROL_SESSION_ID"')
        parts.append("set -e ASKSHELL_FISH_INTEGRATION")
        parts.append(fish_integration(helper))
        config.write_text("\n".join(parts))
        env["XDG_CONFIG_HOME"] = str(config_home)
    try:
        return subprocess.call([os.environ.get("SHELL", "/usr/bin/zsh"), "-i"], env=env)
    finally:
        shutil.rmtree(runtime_dir, ignore_errors=True)


def main():
    if len(sys.argv) < 2:
        raise RuntimeError("missing remote helper command")
    command = sys.argv[1]
    if command == "shell":
        return run_shell()
    if command == "event":
        kind = sys.argv[2]
        request = {"type": kind}
        if kind == "start":
            values = sys.argv[3:]
            if values and values[0] == "--":
                values = values[1:]
            request["command"] = " ".join(values)
        elif kind == "finish":
            request["exit_code"] = int(sys.argv[4] if sys.argv[3] == "--exit-code" else 0)
        rpc(request)
        if kind in ("start", "finish"):
            marker(kind)
        return 0
    if command == "ask":
        result_dir = sys.argv[sys.argv.index("--result-dir") + 1]
        separator = sys.argv.index("--")
        query = " ".join(sys.argv[separator + 1 :])
        write_result(result_dir, "reject")
        print(
            "AskShell 正在思考…" if os.environ.get("ASKSHELL_LANG", "").lower().startswith("zh")
            else "AskShell is thinking…",
            file=open("/dev/tty", "w"),
        )
        action, suggested = prompt(query)
        write_result(result_dir, action, suggested)
        return 0
    raise RuntimeError(f"unknown remote helper command {command!r}")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"askshell: {error}", file=sys.stderr)
        raise SystemExit(1)
`
