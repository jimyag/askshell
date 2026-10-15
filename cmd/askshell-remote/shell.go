package main

import "strings"

// The remote integrations mirror internal/shell's zsh/fish scripts but keep
// only what works over the control socket: every code path on a remote has
// one, so the record fallback and its socket checks are dropped.
func zshIntegration(binary string) string {
	script := `
if [[ -z "${ASKSHELL_ZSH_INTEGRATION:-}" ]]; then
  typeset -g ASKSHELL_ZSH_INTEGRATION=1
  typeset -gx ASKSHELL_ACTIVE=1
  typeset -gx ASKSHELL_BACKEND=zsh
  typeset -g _ASKSHELL_BIN=__BINARY__

  function _askshell_query() {
    unset _ASKSHELL_NATIVE_COMMAND
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

  function _askshell_fix_failed() {
    local result_dir
    result_dir="$(command mktemp -d "${TMPDIR:-/tmp}/askshell.XXXXXXXX")" || return 1
    "$_ASKSHELL_BIN" fix --result-dir "$result_dir"
    local fix_status=$?
    local action="reject"
    local suggested=""
    [[ -r "$result_dir/action" ]] && action="$(<"$result_dir/action")"
    [[ -r "$result_dir/command" ]] && suggested="$(<"$result_dir/command")"
    command rm -rf -- "$result_dir"
    if (( fix_status == 0 )) && [[ "$action" == "execute" || "$action" == "edit" ]] && [[ -n "$suggested" ]]; then
      print -z -- "$suggested"
    fi
    return "$fix_status"
  }

  function _askshell_record_preexec() {
    typeset -g _ASKSHELL_NATIVE_COMMAND="$1"
    "$_ASKSHELL_BIN" event start -- "$1" >/dev/null 2>&1
  }

  function _askshell_record_native_command() {
    local exit_code=$?
    if [[ -n "${_ASKSHELL_NATIVE_COMMAND+x}" ]]; then
      unset _ASKSHELL_NATIVE_COMMAND
      local recorded=0
      "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1 || recorded=$?
      if (( exit_code != 0 && recorded == 0 )); then
        _askshell_fix_failed
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
`
	return strings.ReplaceAll(script, "__BINARY__", quote(binary))
}

func fishIntegration(binary string) string {
	script := `
if not set -q ASKSHELL_FISH_INTEGRATION
    set -g ASKSHELL_FISH_INTEGRATION 1
    set -gx ASKSHELL_ACTIVE 1
    set -gx ASKSHELL_BACKEND fish
    set -g _ASKSHELL_BIN __BINARY__

    function _askshell_query
        set -e _ASKSHELL_NATIVE_COMMAND
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

    function _askshell_fix_failed
        set -l temp_root /tmp
        if test -n "$TMPDIR"
            set temp_root "$TMPDIR"
        end
        set -l result_dir (command mktemp -d "$temp_root/askshell.XXXXXXXX")
        if test $status -ne 0 -o -z "$result_dir"
            return 1
        end
        "$_ASKSHELL_BIN" fix --result-dir "$result_dir"
        set -l fix_status $status
        set -l action reject
        set -l suggested
        if test -r "$result_dir/action"
            set action (string trim -- (command cat "$result_dir/action"))
        end
        if test -r "$result_dir/command"
            set suggested (string collect <"$result_dir/command")
        end
        command rm -rf -- "$result_dir"
        if test $fix_status -eq 0
            if contains -- "$action" execute edit
                test -n "$suggested"; and commandline -r -- "$suggested"
            end
        end
        return $fix_status
    end

    function _askshell_record_preexec --on-event fish_preexec
        set -g _ASKSHELL_NATIVE_COMMAND "$argv[1]"
        "$_ASKSHELL_BIN" event start -- "$argv[1]" >/dev/null 2>&1
    end

    function _askshell_record_postexec --on-event fish_postexec
        set -l exit_code $status
        if set -q _ASKSHELL_NATIVE_COMMAND
            set -e _ASKSHELL_NATIVE_COMMAND
            set -l recorded 0
            "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1; or set recorded $status
            if test $exit_code -ne 0; and test $recorded -eq 0
                _askshell_fix_failed
            end
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
`
	return strings.ReplaceAll(script, "__BINARY__", quote(binary))
}
