package ui

import "strings"

type messages struct {
	you                  string
	thinking             string
	reasoningSummary     string
	risk                 string
	selectCommand        string
	selectSingle         string
	invalidSelection     string
	timeout              string
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
			selectCommand:        "选择 [1-%d]（回车 = 1，e数字 = 编辑，n = 拒绝）：",
			selectSingle:         "回车放入终端，e 编辑，n 拒绝：",
			invalidSelection:     "无效选择。",
			timeout:              "（长时间未输入，默认拒绝）",
			riskDestructive:      "可能删除或覆盖数据",
			riskPrivilegedAction: "需要提权或影响系统状态",
		}
	}
	return messages{
		you:                  "You: ",
		thinking:             "AskShell is thinking…",
		reasoningSummary:     "Reasoning summary:",
		risk:                 "Risk: %s\n",
		selectCommand:        "Choose [1-%d] (Enter = 1, eN = edit, n = reject): ",
		selectSingle:         "Enter to accept, e to edit, n to reject: ",
		invalidSelection:     "Invalid selection.",
		timeout:              "(no input; rejecting)",
		riskDestructive:      "may delete or overwrite data",
		riskPrivilegedAction: "requires privileges or affects system state",
	}
}
