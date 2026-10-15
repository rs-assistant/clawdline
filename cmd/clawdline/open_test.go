package main

import (
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/install"
)

// The line `clawdline setup` ends with for an install on another port is
// "Run clawdline open --port 7790"; `open` must accept exactly that.
func TestOpenAcceptsThePortSetupPrints(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_PORT", "")
	printOnly, port, err := parseOpenArgs(strings.Fields(install.PortFlag(7790)))
	if err != nil || port != 7790 || printOnly {
		t.Fatalf("open%s = print %v, port %d, err %v; want port 7790", install.PortFlag(7790), printOnly, port, err)
	}
	printOnly, port, err = parseOpenArgs([]string{"--print", "--port", "7790"})
	if err != nil || port != 7790 || !printOnly {
		t.Fatalf("open --print --port 7790 = print %v, port %d, err %v", printOnly, port, err)
	}
}

func TestOpenWithoutAPortKeepsTheEnvironmentAndTheDefault(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_PORT", "")
	if _, port, err := parseOpenArgs(nil); err != nil || port != 7727 {
		t.Fatalf("open = port %d, err %v; want 7727", port, err)
	}
	t.Setenv("CLAWDLINE_NEXT_PORT", "7801")
	if _, port, err := parseOpenArgs([]string{"--print"}); err != nil || port != 7801 {
		t.Fatalf("CLAWDLINE_NEXT_PORT=7801 open --print = port %d, err %v; want 7801", port, err)
	}
}

func TestOpenRefusesWhatIsNotAPort(t *testing.T) {
	for _, args := range [][]string{{"--port", "70000"}, {"--port", "-1"}, {"--port", "x"}, {"extra"}} {
		if _, _, err := parseOpenArgs(args); err == nil {
			t.Errorf("open %v was accepted", args)
		}
	}
}
