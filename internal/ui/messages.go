package ui

import "strings"

type messages struct {
	you                  string
	thinking             string
	reasoningSummary     string
	risk                 string
	selectCommand        string
	invalidSelection     string
	selectAction         string
	riskDestructive      string
	riskPrivilegedAction string
}

func messagesFor(language string) messages {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "zh" || strings.HasPrefix(language, "zh-") || strings.HasPrefix(language, "zh_") {
		return messages{
			you:                  "你：",
			thinking:             "AskShell 正在思考…",
			reasoningSummary:     "分析摘要：",
			risk:                 "风险：%s\n",
			selectCommand:        "选择命令 [1-%d]，或输入 n/否：",
			invalidSelection:     "无效选择。",
			selectAction:         "操作：[y/是] 放入终端  [n/否] 拒绝：",
			riskDestructive:      "可能删除或覆盖数据",
			riskPrivilegedAction: "需要提权或影响系统状态",
		}
	}
	return messages{
		you:                  "You: ",
		thinking:             "AskShell is thinking…",
		reasoningSummary:     "Reasoning summary:",
		risk:                 "Risk: %s\n",
		selectCommand:        "Select command [1-%d], or n/no to reject: ",
		invalidSelection:     "Invalid selection.",
		selectAction:         "Action: [y/yes] put in terminal  [n/no] reject: ",
		riskDestructive:      "may delete or overwrite data",
		riskPrivilegedAction: "requires privileges or affects system state",
	}
}
