package shell

import (
	"fmt"
	"os/exec"
	"strings"
)

func ZshIntegration(binary string) string {
	script := `
if [[ "${_ASKSHELL_SESSION_OWNER_PID:-}" != "$$" ]]; then
  typeset -g _ASKSHELL_SESSION_OWNER_PID="$$"
  if [[ -z "${ASKSHELL_SESSION_ID:-}" ]]; then
    typeset -gx ASKSHELL_SESSION_ID="zsh-$$-$RANDOM-$RANDOM"
  fi
fi

if [[ -z "${ASKSHELL_ZSH_INTEGRATION:-}" ]]; then
  typeset -g ASKSHELL_ZSH_INTEGRATION=1
  typeset -gx ASKSHELL_ACTIVE=1
  typeset -gx ASKSHELL_BACKEND=zsh
  typeset -g _ASKSHELL_BIN=__ASKSHELL_BINARY__

  function _askshell_error() {
    case "${ASKSHELL_LANG:-en}" in
      zh|zh-*|zh_*)
        case "$1" in
          result-dir) print -u2 "askshell: 无法创建临时目录" ;;
        esac
        ;;
      *)
        case "$1" in
          result-dir) print -u2 "askshell: cannot create temporary directory" ;;
        esac
        ;;
    esac
  }

  function _askshell_query() {
    unset _ASKSHELL_NATIVE_COMMAND
    if [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
      "$_ASKSHELL_BIN" event cancel >/dev/null 2>&1
    fi
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
          print -z -- "$suggested"
        fi
        ;;
    esac
  }

  function _askshell_record_preexec() {
    typeset -g _ASKSHELL_NATIVE_COMMAND="$1"
    if [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
      "$_ASKSHELL_BIN" event start -- "$1" >/dev/null 2>&1
    fi
  }

  function _askshell_record_native_command() {
    local exit_code=$?
    if [[ -n "${_ASKSHELL_NATIVE_COMMAND+x}" ]]; then
      local command="$_ASKSHELL_NATIVE_COMMAND"
      unset _ASKSHELL_NATIVE_COMMAND
      if [[ -n "${ASKSHELL_SOCKET:-}" ]]; then
        "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1
      else
        "$_ASKSHELL_BIN" record --exit-code "$exit_code" -- "$command"
      fi
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
  fi
fi
`
	return strings.ReplaceAll(script, "__ASKSHELL_BINARY__", zshQuote(binary))
}

func ZshCommand() (*exec.Cmd, error) {
	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		return nil, fmt.Errorf("find zsh: %w", err)
	}
	//nolint:gosec // zshPath was resolved through exec.LookPath.
	return exec.Command(zshPath, "-i"), nil
}

func zshQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
