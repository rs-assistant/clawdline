package cloudops

// A paired browser can ask a machine it already controls to carry a pairing
// offer to a second machine. The offer reaches the helper inside the encrypted
// Cloud command; the Cloud API never receives the plaintext. The helper's own
// pairing route checks the offer's account and starts only the fixed task.
func init() {
	register(
		op{name: "pair-agent-start"},
		op{name: "pair-agent-status", read: true},
	)
}
