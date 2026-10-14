package shell

import (
	"fmt"
	"os/exec"
	"strings"
)

// BashIntegration installs preexec/precmd semantics on top of the DEBUG
// trap and PROMPT_COMMAND. DEBUG fires before each simple command and
// BASH_COMMAND holds the raw text about to run; PROMPT_COMMAND runs
// before every prompt and sees $? of the last command.
func BashIntegration(binary string) string {
	script := `
if [[ "${_ASKSHELL_SESSION_OWNER_PID:-}" != "$$" ]]; then
  declare -g _ASKSHELL_SESSION_OWNER_PID="$$"
  if [[ -z "${ASKSHELL_SESSION_ID:-}" ]]; then
    declare -gx ASKSHELL_SESSION_ID="bash-$$-$RANDOM-$RANDOM"
  fi
fi

if [[ -z "${ASKSHELL_BASH_INTEGRATION:-}" ]]; then
  declare -g ASKSHELL_BASH_INTEGRATION=1
  declare -gx ASKSHELL_ACTIVE=1
  declare -gx ASKSHELL_BACKEND=bash
  declare -g _ASKSHELL_BIN=__ASKSHELL_BINARY__

  function _askshell_error() {
    case "${ASKSHELL_LANG:-en}" in
      zh|zh-*|zh_*)
        case "$1" in
          result-dir) printf 'askshell: 无法创建临时目录\n' >&2 ;;
        esac
        ;;
      *)
        case "$1" in
          result-dir) printf 'askshell: cannot create temporary directory\n' >&2 ;;
        esac
        ;;
    esac
  }

  function _askshell_query() {
    unset _ASKSHELL_NATIVE_COMMAND
    local query="$*"
    local result_dir
    result_dir="$(command mktemp -d "${TMPDIR:-/tmp}/askshell.XXXXXXXX")" || {
      _askshell_error result-dir
      return 1
    }

    "$_ASKSHELL_BIN" ask --result-dir "$result_dir" -- "$query"
    local ask_status=$?

    local action="reject"
    local suggested=""
    if [[ -r "$result_dir/action" ]]; then
      action="$(<"$result_dir/action")"
    fi
    if [[ -r "$result_dir/command" ]]; then
      suggested="$(<"$result_dir/command")"
    fi
    command rm -rf -- "$result_dir"

    if (( ask_status != 0 )); then
      return "$ask_status"
    fi

    case "$action" in
      execute|edit)
        if [[ -n "$suggested" ]]; then
          builtin history -s -- "$suggested"
          _ASKSHELL_INSERT="$suggested"
          _askshell_arm_insert
        fi
        ;;
    esac
  }

  # bash has no print -z equivalent, so we stage the accepted text and let
  # a temporary PROMPT_COMMAND drop it via a one-shot key binding.
  function _askshell_arm_insert() {
    _ASKSHELL_ARMED=1
    bind -x '"\C-g": _askshell_insert_pending' 2>/dev/null
  }
  function _askshell_insert_pending() {
    if [[ -n "${_ASKSHELL_INSERT:-}" ]]; then
      READLINE_LINE="${READLINE_LINE:0:$READLINE_POINT}${_ASKSHELL_INSERT}${READLINE_LINE:$READLINE_POINT}"
      READLINE_POINT=$(( READLINE_POINT + ${#_ASKSHELL_INSERT} ))
      _ASKSHELL_INSERT=""
    fi
  }

  function _askshell_fix_failed() {
    local result_dir
    result_dir="$(command mktemp -d "${TMPDIR:-/tmp}/askshell.XXXXXXXX")" || {
      _askshell_error result-dir
      return 1
    }

    "$_ASKSHELL_BIN" fix --result-dir "$result_dir"
    local fix_status=$?

    local action="reject"
    local suggested=""
    [[ -r "$result_dir/action" ]] && action="$(<"$result_dir/action")"
    [[ -r "$result_dir/command" ]] && suggested="$(<"$result_dir/command")"
    command rm -rf -- "$result_dir"

    if (( fix_status == 0 )) &&
      [[ "$action" == "execute" || "$action" == "edit" ]] &&
      [[ -n "$suggested" ]]; then
      builtin history -s -- "$suggested"
      _ASKSHELL_INSERT="$suggested"
      _askshell_arm_insert
    fi
    return "$fix_status"
  }

  declare -g _ASKSHELL_CMD_PENDING=""
  declare -g _ASKSHELL_IN_PROMPT=0

  function _askshell_debug() {
    # 忽略我们在 precmd 里调用的子命令
    (( _ASKSHELL_IN_PROMPT == 1 )) && return 0
    local cmd="$BASH_COMMAND"
    # 忽略 PROMPT_COMMAND 自己展开出来的求值以及 trap 递归
    case "$cmd" in
      _askshell_*|_ASKSHELL_*=*|trap*|PROMPT_COMMAND*) return 0 ;;
    esac
    _ASKSHELL_CMD_PENDING="$cmd"
    # preexec 语义
    if [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
      "$_ASKSHELL_BIN" event start -- "$cmd" >/dev/null 2>&1
    fi
    return 0
  }

  function _askshell_precmd() {
    local exit_code=$?
    _ASKSHELL_IN_PROMPT=1
    local had_cmd=0
    local cmd=""
    if [[ -n "$_ASKSHELL_CMD_PENDING" ]]; then
      had_cmd=1
      cmd="$_ASKSHELL_CMD_PENDING"
      _ASKSHELL_CMD_PENDING=""
    fi
    if (( had_cmd == 1 )); then
      local recorded=0
      if [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
        "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1 || recorded=$?
      else
        "$_ASKSHELL_BIN" record --exit-code "$exit_code" -- "$cmd"
      fi
      if (( exit_code != 0 && recorded == 0 )) && [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
        _askshell_fix_failed
      fi
    fi
    _ASKSHELL_IN_PROMPT=0
    return "$exit_code"
  }

  trap _askshell_debug DEBUG
  if [[ -n "${PROMPT_COMMAND:-}" ]]; then
    case "$PROMPT_COMMAND" in
      *_askshell_precmd*) ;;
      *) PROMPT_COMMAND="_askshell_precmd; $PROMPT_COMMAND" ;;
    esac
  else
    PROMPT_COMMAND="_askshell_precmd"
  fi

  alias '?'='_askshell_query'
  alias '？'='_askshell_query'
fi
`
	return strings.ReplaceAll(script, "__ASKSHELL_BINARY__", bashQuote(binary))
}

func BashCommand() (*exec.Cmd, error) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("find bash: %w", err)
	}
	//nolint:gosec // bashPath was resolved through exec.LookPath.
	return exec.Command(bashPath, "-i"), nil
}

func bashQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
