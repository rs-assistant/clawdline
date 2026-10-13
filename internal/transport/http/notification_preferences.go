package http

import "github.com/sainteye/clawdline/internal/adapters/nextconfig"

// notificationEnabled reads the person's choice when a notification is due.
// Missing new keys inherit the older controls so an update does not silently
// change the behavior of an existing installation.
func (s *Server) notificationEnabled(key string) bool {
	values, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return notificationDefault(key)
	}
	if on, ok := values.Bool(key); ok {
		return on
	}
	switch key {
	case "push_on_waiting":
		if on, ok := values.Bool("orchestrator_agent_notify"); ok {
			return on
		}
	case "push_on_app_release_success", "push_on_cloud_release_success", "push_on_release_failure":
		if on, ok := values.Bool("push_on_deploy"); ok {
			return on
		}
	}
	return notificationDefault(key)
}

func notificationDefault(key string) bool {
	switch key {
	case "push_on_app_release_success", "push_on_cloud_release_success", "push_on_release_failure":
		return false
	default:
		return true
	}
}
