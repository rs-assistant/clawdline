package capacity

import "testing"

func TestConsoleExecutionRecoveryIsRegistered(t *testing.T) {
	for _, row := range Register() {
		if row.Name == "console.execution_recovery_attempts" {
			if row.Limit != 1 {
				t.Fatalf("execution recovery policy: %+v", row)
			}
			return
		}
	}
	t.Fatal("automatic execution recovery limit is not registered")
}
