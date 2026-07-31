package shell

import (
	"fmt"
	"os/exec"
	"strings"
)

func FishIntegration(binary string) string {
	script := `
if test "$_ASKSHELL_SESSION_OWNER_PID" != "$fish_pid"
    set -g _ASKSHELL_SESSION_OWNER_PID "$fish_pid"
    if not set -q ASKSHELL_SESSION_ID
        set -gx ASKSHELL_SESSION_ID "fish-$fish_pid-"(random)"-"(random)
    end
end

if not set -q ASKSHELL_FISH_INTEGRATION
    set -g ASKSHELL_FISH_INTEGRATION 1
    set -gx ASKSHELL_ACTIVE 1
    set -gx ASKSHELL_BACKEND fish
    set -g _ASKSHELL_BIN __ASKSHELL_BINARY__

    function _askshell_error
        switch "$ASKSHELL_LANG"
            case 'zh' 'zh-*' 'zh_*'
                switch "$argv[1]"
                    case result-dir
                        echo "askshell: 无法创建临时目录" >&2
                end
            case '*'
                switch "$argv[1]"
                    case result-dir
                        echo "askshell: cannot create temporary directory" >&2
                end
        end
    end

    function _askshell_query
        set -e _ASKSHELL_NATIVE_COMMAND
        if test -n "$ASKSHELL_SOCKET"
            "$_ASKSHELL_BIN" event cancel >/dev/null 2>&1
        end
        set -l query (string join ' ' -- $argv)
        set -l temp_root /tmp
        if test -n "$TMPDIR"
            set temp_root "$TMPDIR"
        end
        set -l result_dir (command mktemp -d "$temp_root/askshell.XXXXXXXX")
        if test $status -ne 0 -o -z "$result_dir"
            _askshell_error result-dir
            return 1
        end

        "$_ASKSHELL_BIN" ask --result-dir "$result_dir" -- "$query"
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
            _askshell_error result-dir
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
                if test -n "$suggested"
                    commandline -r -- "$suggested"
                end
            end
        end
        return $fix_status
    end

    function _askshell_record_preexec --on-event fish_preexec
        set -g _ASKSHELL_NATIVE_COMMAND "$argv[1]"
        if test -n "$ASKSHELL_SOCKET"
            "$_ASKSHELL_BIN" event start -- "$argv[1]" >/dev/null 2>&1
        end
    end

    function _askshell_record_postexec --on-event fish_postexec
        set -l exit_code $status
        if set -q _ASKSHELL_NATIVE_COMMAND
            set -l native_command "$_ASKSHELL_NATIVE_COMMAND"
            set -e _ASKSHELL_NATIVE_COMMAND
            set -l recorded 0
            if test -n "$ASKSHELL_SOCKET"
                "$_ASKSHELL_BIN" event finish --exit-code "$exit_code" >/dev/null 2>&1; or set recorded $status
            else
                "$_ASKSHELL_BIN" record --exit-code "$exit_code" -- "$native_command"
            end
            if test $exit_code -ne 0; and test $recorded -eq 0; and test -n "$ASKSHELL_SOCKET"
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
            set -l query (string sub -s 2 -- "$command_name")
            _askshell_query "$query" $argv[2..-1]
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
	return strings.ReplaceAll(script, "__ASKSHELL_BINARY__", fishQuote(binary))
}

func FishCommand() (*exec.Cmd, error) {
	fishPath, err := exec.LookPath("fish")
	if err != nil {
		return nil, fmt.Errorf("find fish: %w", err)
	}
	//nolint:gosec // fishPath was resolved through exec.LookPath.
	return exec.Command(fishPath, "-i"), nil
}

func fishQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}
