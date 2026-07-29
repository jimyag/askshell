package assistant

import (
	"regexp"
	"strings"
)

var (
	destructiveCommand = regexp.MustCompile(
		`(?i)(^|[;&|()\n]\s*)((command|env|sudo|doas)\s+|xargs(\s+-[^[:space:]]+)*\s+)?(rm|rmdir|unlink|shred|truncate|mkfs(\.[[:alnum:]]+)?|wipefs)(\s|$)`,
	)
	overwriteCommand = regexp.MustCompile(
		`(?i)(^|[;&|()\n]\s*)((command|env|sudo|doas)\s+|xargs(\s+-[^[:space:]]+)*\s+)?(dd|cp|mv)(\s|$)`,
	)
	privilegedCommand = regexp.MustCompile(
		`(?i)(^|[;&|()\n]\s*)((command|env)\s+)?(sudo|doas|su|shutdown|reboot|halt|poweroff)(\s|$)`,
	)
)

func ClassifyRisk(command string) Risk {
	switch {
	case destructiveCommand.MatchString(command),
		overwriteCommand.MatchString(command),
		strings.Contains(command, "diskutil erase"),
		strings.Contains(command, "> /dev/"),
		containsOverwriteRedirect(command):
		return RiskDestructive
	case privilegedCommand.MatchString(command):
		return RiskPrivileged
	default:
		return RiskNone
	}
}

func containsOverwriteRedirect(command string) bool {
	var quote byte
	escaped := false
	for index := 0; index < len(command); index++ {
		value := command[index]
		if escaped {
			escaped = false
			continue
		}
		if quote == '\'' {
			if value == quote {
				quote = 0
			}
			continue
		}
		if value == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if value == quote {
				quote = 0
			}
			continue
		}
		if value == '\'' || value == '"' {
			quote = value
			continue
		}
		if value != '>' {
			continue
		}
		if index+1 < len(command) && command[index+1] == '>' {
			index++
			continue
		}
		if index > 0 && command[index-1] == '>' {
			continue
		}
		return true
	}
	return false
}
