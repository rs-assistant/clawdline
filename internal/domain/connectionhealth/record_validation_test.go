package connectionhealth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Every string a caller can put in a record is an enum, a location code or
// an opaque id, and the stamps are the journal's. A caller that tries to
// carry anything else — free text in the event, kind or rule, or its own
// schema, boot, event id, sequence or time — is refused by field name, the
// value is never repeated, and the refusal takes no sequence number.
func TestARecordCarriesNoCallerTextAndNoForgedStamp(t *testing.T) {
	const secret = "Bearer sk-live-SECRET /Users/someone/.ssh/id_ed25519"
	for field, r := range map[string]Record{
		"event":        {Event: Event(secret)},
		"event ":       {Event: ""},
		"event  ":      {Event: EventBoot},            // a caller cannot forge a boot boundary
		"event   ":     {Event: EventTransitionsLost}, // nor a loss
		"kind":         {Event: EventOverflow, Kind: Kind(secret)},
		"rule":         {Event: EventOverflow, Rule: secret},
		"schema":       {Event: EventOverflow, Schema: secret},
		"boot_id":      {Event: EventOverflow, BootID: "boot-b"},
		"event_id":     {Event: EventOverflow, EventID: "boot-a-99"},
		"producer_seq": {Event: EventOverflow, Seq: 99},
		"time":         {Event: EventOverflow, Wall: time.Unix(1, 0)},
		"time ":        {Event: EventOverflow, MonoNS: 1},
		"build":        {Event: EventOverflow, Build: "abc123"},
		"source":       {Event: EventOverflow, Source: Source(secret)},
		"stage":        {Event: EventOverflow, Stage: Stage(secret)},
		"class":        {Event: EventOverflow, Class: Class(secret)},
		"location":     {Event: EventOverflow, Location: "internal/adapters/cloud/transport.go:445"},
		"correlation":  {Event: EventOverflow, Correlation: secret},
	} {
		c := newFakeClock()
		j, err := NewJournal(c, "boot-a", "abc123")
		if err != nil {
			t.Fatal(err)
		}
		err = j.Append(r)
		want := strings.TrimSpace(field)
		if CodeOf(err) != CodeInvalidField || !strings.HasSuffix(err.Error(), ": "+want) {
			t.Errorf("%s: %v", want, err)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/Users") {
			t.Errorf("%s: the refusal repeats the value: %v", want, err)
		}
		if err := j.Append(Record{Event: EventOverflow}); err != nil {
			t.Fatal(err)
		}
		p, _ := j.Read(Query{})
		if len(p.Records) != 2 || p.Records[1].Seq != 2 || p.Coverage.Refused != 1 {
			t.Errorf("%s: refusal took a sequence number or was not counted: %+v", want, p.Coverage)
		}
		b, _ := json.Marshal(p)
		if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "boot-b") || strings.Contains(string(b), ".go:") {
			t.Errorf("%s: caller text reached the page: %s", want, b)
		}
	}
}

// A record with only its event is valid; the journal stamps the rest.
func TestOmittedFieldsAreValidAndStampsAreGenerated(t *testing.T) {
	c := newFakeClock()
	j, err := NewJournal(c, "boot-a", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	c.advance(2 * time.Second)
	full := Record{
		Event: EventStallDetected, Source: SourceDaemon, Stage: StageHandlerStarted, Class: ClassRead,
		Kind: Finite, Location: "cloud.link.read_handler", PhaseID: 7, Correlation: "trace-1",
		AgeNS: 1, DeadlineNS: 2, Count: 3, Rule: RuleVersion,
	}
	for _, r := range []Record{{Event: EventOverflow}, full} {
		if err := j.Append(r); err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
	}
	p, _ := j.Read(Query{})
	boot, bare := p.Records[0], p.Records[1]
	if boot.Event != EventBoot || boot.Build != "abc123" || boot.Seq != 1 {
		t.Errorf("boot record %+v", boot)
	}
	if bare.Schema != RecordSchema || bare.BootID != "boot-a" || bare.Seq != 2 || bare.EventID != "boot-a-2" ||
		bare.MonoNS != int64(2*time.Second) || !bare.Wall.Equal(c.Now().Wall) || bare.Build != "" {
		t.Errorf("stamps %+v", bare)
	}
	if got := p.Records[2]; got.Location != full.Location || got.Kind != Finite || got.Rule != RuleVersion {
		t.Errorf("fields lost %+v", got)
	}
}
