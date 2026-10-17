package session

import "strings"

// ReadStartupModel reads Codex's own startup card before a first turn has
// written a model to the transcript. Arbitrary mentions are not evidence.
func ReadStartupModel(screen string, assistant Assistant) string {
	model, _ := ReadStartupSettings(screen, assistant)
	return model
}

// ReadStartupSettings returns only settings explicitly printed in the card.
func ReadStartupSettings(screen string, assistant Assistant) (string, string) {
	if assistant != AssistantCodex {
		return "", ""
	}
	card := false
	for _, line := range strings.Split(Plain(screen), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "│ >_ OpenAI Codex (") {
			card = true
			continue
		}
		if card && strings.HasPrefix(line, "╰") {
			card = false
		}
		if !card || !strings.HasPrefix(line, "│ model:") || !strings.Contains(line, "/model to change") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "│ model:"))
		if len(fields) > 0 && fields[0] != "/model" {
			effort := ""
			if len(fields) > 1 {
				switch fields[1] {
				case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
					effort = fields[1]
				}
			}
			return fields[0], effort
		}
	}
	return "", ""
}
