package tunnel

import (
	"strings"
	"testing"
	"time"
)

// A typo is off: the only way to a tunnel is to have written the word. The
// removed quick tunnel is off too, and is told apart so the settings page can
// say why.
func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"quick": Off, " Quick ": Off, " Named ": Named, "off": Off, "": Off, "quik": Off, "on": Off, "true": Off,
	} {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{"quick": true, " QUICK ": true, "named": false, "off": false, "": false} {
		if got := Retired(in); got != want {
			t.Errorf("Retired(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRefusal(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         Mode
		paired       bool
		remote       bool
		tunnel, host string
		hostOK       bool
		want         string // a fragment of the refusal, or "" for none
	}{
		{"off never refuses", Off, false, false, "", "", true, ""},
		{"nothing paired, named", Named, false, true, "t", "a.example.com", true, "no paired device"},
		{"remote off", Named, true, false, "t", "a.example.com", true, "Remote access is off"},
		{"a hostname that is not one", Named, true, true, "t", "a b", false, "not a hostname"},
		{"named without a name", Named, true, true, "", "a.example.com", true, "remote_tunnel_name"},
		{"named with a flag for a name", Named, true, true, "--url", "a.example.com", true, "not a tunnel name"},
		{"named with YAML in its name", Named, true, true, "a\nservice: x", "a.example.com", true, "not a tunnel name"},
		{"named without a hostname", Named, true, true, "t", "", true, "remote_hostname"},
		{"named, whole", Named, true, true, "clawdline", "a.example.com", true, ""},
	} {
		got := Refusal(tc.mode, tc.paired, tc.remote, tc.tunnel, tc.host, tc.hostOK)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHostname(t *testing.T) {
	for in, want := range map[string]struct {
		host string
		ok   bool
	}{
		"":                              {"", true},
		"  Mac.Example.com ":            {"mac.example.com", true},
		"https://mac.example.com/":      {"mac.example.com", true},
		"HTTP://mac.example.com":        {"mac.example.com", true},
		"mac.example.com.":              {"mac.example.com", true},
		"mac.example.com\n  - service:": {"", false},
		"mac example.com":               {"", false},
		"-mac.example.com":              {"", false},
		"mac.example.com:8443":          {"", false},
		`"mac.example.com"`:             {"", false},
	} {
		host, ok := Hostname(in)
		if ok != want.ok || (ok && host != want.host) {
			t.Errorf("Hostname(%q) = %q, %v; want %q, %v", in, host, ok, want.host, want.ok)
		}
	}
}

// `--config` is on every command line, pointing at this daemon's file: without
// it cloudflared reads the person's own ~/.cloudflared/config.yml, whose
// `tunnel:` key starts somebody else's tunnel.
func TestArgumentsAlwaysCarryOurConfig(t *testing.T) {
	const cfg = "/state/dir/cloudflared.yml"
	named := Arguments(Plan{Mode: Named, Name: "clawdline", Hostname: "a.example.com", Port: 7817}, cfg)
	want := "tunnel --config /state/dir/cloudflared.yml --no-autoupdate --metrics 127.0.0.1:0 --grace-period 2s run clawdline"
	if got := strings.Join(named, " "); got != want {
		t.Errorf("named:\n got %s\nwant %s", got, want)
	}
	// The options are cloudflared's `tunnel` options, so they come before `run`.
	for _, args := range [][]string{named} {
		at := index(args, "--config")
		if at < 0 || args[at+1] != cfg {
			t.Errorf("no --config %s in %v", cfg, args)
		}
		if run := index(args, "run"); run >= 0 && run < at {
			t.Errorf("--config after run: %v", args)
		}
	}
}

func index(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestConfigFile(t *testing.T) {
	named := ConfigFile(Plan{Mode: Named, Name: "clawdline", Hostname: "a.example.com", Port: 7817}, "/h/.cloudflared/id.json")
	for _, want := range []string{
		`tunnel: "clawdline"`,
		`credentials-file: "/h/.cloudflared/id.json"`,
		`  - hostname: "a.example.com"`,
		`    service: "http://127.0.0.1:7817"`,
		"      connectTimeout: 30s",
		"  - service: http_status:404\n",
	} {
		if !strings.Contains(named, want) {
			t.Errorf("named file lacks %q:\n%s", want, named)
		}
	}
	// The catch-all is the last rule: a tunnel that answers for names it was
	// never asked about is an open proxy.
	if !strings.HasSuffix(named, "  - service: http_status:404\n") {
		t.Errorf("the 404 is not last:\n%s", named)
	}
}

// Real lines, captured from cloudflared 2026.6.1 by the Swift app; the
// connection id in `registered` is replaced by a fixture's.
const (
	startLine   = "2026-08-18T09:31:14Z INF Starting tunnel tunnelID=0b1c2d3e"
	registered  = "2026-08-18T09:31:18Z INF Registered tunnel connection connIndex=0 connection=c0000001-0000-4000-8000-000000000001 event=0 ip=2606:4700:a8::5 location=tpe01 protocol=quic"
	unregLine   = "2026-08-18T09:40:01Z INF Unregistered tunnel connection connIndex=0 event=0 ip=2606:4700:a8::5"
	badNameLine = "error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels"
)

func TestRegisteredConnection(t *testing.T) {
	if loc, ok := RegisteredConnection(registered); !ok || loc != "tpe01" {
		t.Errorf("registered: %q %v", loc, ok)
	}
	if _, ok := RegisteredConnection(unregLine); ok {
		t.Error("a tunnel closing read as a tunnel opening")
	}
	if loc, ok := RegisteredConnection("INF Registered tunnel connection connIndex=1"); !ok || loc != "?" {
		t.Errorf("no location: %q %v", loc, ok)
	}
	if _, ok := RegisteredConnection(startLine); ok {
		t.Error("starting is not a connection")
	}
}

func TestComplaint(t *testing.T) {
	for line, want := range map[string]string{
		registered: "",
		"2026-08-18T09:31:18Z WRN Cannot determine default origin certificate path":   "",
		"2026-08-18T09:31:18Z ERR Failed to dial a quic connection error=\"timeout\"": `Failed to dial a quic connection error="timeout"`,
		badNameLine: badNameLine,
		"   ":       "",
	} {
		if got := Complaint(line); got != want {
			t.Errorf("Complaint(%q) = %q, want %q", line, got, want)
		}
	}
	if got := Complaint(strings.Repeat("é", 300)); len(got) > complaintLimit || !strings.HasPrefix(strings.Repeat("é", 300), got) {
		t.Errorf("a long complaint is cut on a character: %d bytes", len(got))
	}
}

func TestRedacted(t *testing.T) {
	if got := Redacted("https://box.example.com"); got != "https://….example.com" {
		t.Errorf("redacted: %q", got)
	}
}

func TestBackoff(t *testing.T) {
	for attempt, want := range map[int]time.Duration{0: time.Second, 1: time.Second, 2: 2 * time.Second, 4: 8 * time.Second, 9: time.Minute} {
		if got := Backoff(attempt); got != want {
			t.Errorf("Backoff(%d) = %s, want %s", attempt, got, want)
		}
	}
}
