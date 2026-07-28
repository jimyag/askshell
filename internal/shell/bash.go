package shell

import (
	"fmt"
	"os/exec"
	"strings"
)

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
          builtin history -s "$suggested"
          case "${ASKSHELL_LANG:-en}" in
            zh|zh-*|zh_*) printf 'askshell: 命令已加入历史记录，按上方向键进行编辑\n' ;;
            *) printf 'askshell: command added to history; press Up to edit it\n' ;;
          esac
        fi
        ;;
    esac
  }

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
