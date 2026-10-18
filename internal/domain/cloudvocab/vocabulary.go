// Package cloudvocab is the one table of every operation a paired device can
// ask this machine for over Clawdline Cloud: the word on the wire, the body
// shapes it is accepted in, the local route it becomes, whether it runs as the
// device and behind the write switch, and what the console does with it.
//
// The table is a JSON file embedded here rather than a Go literal, because the
// console has to read the same bytes without a generator in between: a test in
// `web/console` reads `vocabulary.json` directly, and the Go side embeds it.
// This package imports nothing but the standard library, so a domain or
// transport package can depend on it without pulling the bridge or an adapter
// with it.
//
// Nothing at run time reads the file itself. It is held to the bridge's
// catalog by `TestTheVocabularyTableIsTheBridgesCatalog` (internal/app/cloudops)
// and to the console's route parsing by `vocabulary.test.ts`
// (web/console/src/cloud). The console's carry lists are generated from each
// row's `console_*` fields into `web/console/src/cloud/carry-generated.ts`.
// Each row's `decode` is the machine's decoder as data: `tools/contract-gen`
// generates the Go decoders the bridge serves from it, and
// `TestGeneratedDecodersKeepTheGolden` holds them to what the hand-written
// decoders they replaced answered.
package cloudvocab

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed vocabulary.json
var vocabularyJSON []byte

// Kind is whether an operation is effect-free.
type Kind string

const (
	Read    Kind = "read"
	Command Kind = "command"
)

// Console is what the hosted console does with a word (`carry.ts`).
type Console string

const (
	// Carried words are asked for by the console.
	Carried Console = "carried"
	// Deferred words are answered by the machine and deliberately not asked.
	Deferred Console = "deferred"
	// NoMachineRoute words are known on the wire with nothing behind them here.
	NoMachineRoute Console = "no_machine_route"
)

// Seam is which part of the console turns a local route into the word.
type Seam string

const (
	// Writer is `writeRoute` in `relay-writer.ts`.
	Writer Seam = "writer"
	// Reader is one of `RelayReader`'s own cases in `relay-reader.ts`.
	Reader Seam = "reader"
	// Client is the copied `cloud-client.js`, asking on its own with no route.
	Client Seam = "client"
)

// Operation is one word of the vocabulary.
type Operation struct {
	Op        string  `json:"op"`
	Kind      Kind    `json:"kind"`
	WriteGate bool    `json:"write_gate"`
	Console   Console `json:"console"`
	// ConsoleRoute is the word's `CARRIED` entry in `carry-generated.ts`.
	ConsoleRoute string `json:"console_route,omitempty"`
	ConsoleSeam  Seam   `json:"console_seam,omitempty"`
	// ConsoleParsedAs is the word `writeRoute` names for ConsoleRoute when
	// that is not Op: one console route standing for two words.
	ConsoleParsedAs string `json:"console_parsed_as,omitempty"`
	// ConsoleSentence is what the console says instead of asking for a word
	// it does not carry: why it is missing and where the thing can be done.
	// Every deferred and unrouted word has one; a carried word has none.
	ConsoleSentence string `json:"console_sentence,omitempty"`
	// ConsoleNote is the reason a person reading `carry-generated.ts` needs
	// beside the word, emitted there as a comment and read by nothing else.
	ConsoleNote string `json:"console_note,omitempty"`
	// Local is nil for a word with no local route behind it.
	Local *Local `json:"local,omitempty"`
	// Special names each way this word is handled outside decode-then-route.
	Special []string `json:"special,omitempty"`
	// Example holds a value for every body field any shape names.
	Example map[string]any `json:"example"`
	// Shapes is every body key set (without `type`) the word's decoder
	// accepts. In production a wrapper may require more (Wrapper).
	Shapes []Shape `json:"shapes"`
	// Decode is how the decoder reads those bodies.
	Decode *Decode `json:"decode"`
}

// Local is the route an operation becomes on this machine.
type Local struct {
	Method string `json:"method"`
	// Device is whether the request runs as the paired device
	// (`X-Clawdline-Actor: device`) rather than as the machine.
	Device bool `json:"device,omitempty"`
	// Headers are the other headers the route itself sets.
	Headers []string `json:"headers,omitempty"`
}

// Shape is one accepted body and the local request it becomes.
type Shape struct {
	Body []string `json:"body"`
	// Path is the local path template: `{field}` is one segment carrying that
	// body field, `{field*}` a field that may span segments.
	Path  string   `json:"path,omitempty"`
	Query []string `json:"query,omitempty"`
	// LocalBody is the local request body's keys; nil is no body at all.
	LocalBody *[]string `json:"local_body,omitempty"`
	// Example replaces the operation's example for a shape whose values
	// differ from it.
	Example map[string]any `json:"example,omitempty"`
}

type table struct {
	Version     int          `json:"version"`
	Wrappers    []Wrapper    `json:"wrappers"`
	Idempotency *Idempotency `json:"idempotency"`
	Operations  []Operation  `json:"operations"`
}

// minConsoleSentence is the shortest console sentence that can say both what is
// missing and where it can be done instead.
const minConsoleSentence = 40

// Version is the table format this package reads.
const Version = 1

// Operations is the whole table, sorted by word. It decodes the embedded file
// strictly and refuses a table that contradicts itself, so a caller never sees
// a partial vocabulary.
func Operations() ([]Operation, error) {
	return parse(vocabularyJSON)
}

func parse(raw []byte) ([]Operation, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var t table
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("cloudvocab: %w", err)
	}
	if t.Version != Version {
		return nil, fmt.Errorf("cloudvocab: table version %d, this reader knows %d", t.Version, Version)
	}
	if err := validate(t.Operations); err != nil {
		return nil, err
	}
	if err := validateWrappers(t); err != nil {
		return nil, err
	}
	return t.Operations, nil
}

// Wrappers is the production rules around the decoders, and the idempotency
// key every local request that is not a read carries.
func Wrappers() ([]Wrapper, Idempotency, error) {
	dec := json.NewDecoder(bytes.NewReader(vocabularyJSON))
	dec.UseNumber()
	var t table
	if err := dec.Decode(&t); err != nil {
		return nil, Idempotency{}, fmt.Errorf("cloudvocab: %w", err)
	}
	if err := validateWrappers(t); err != nil {
		return nil, Idempotency{}, err
	}
	return t.Wrappers, *t.Idempotency, nil
}

func validate(ops []Operation) error {
	if len(ops) == 0 {
		return fmt.Errorf("cloudvocab: the table is empty")
	}
	if !sort.SliceIsSorted(ops, func(i, j int) bool { return ops[i].Op < ops[j].Op }) {
		return fmt.Errorf("cloudvocab: operations are not sorted by word")
	}
	for i, o := range ops {
		if o.Op == "" {
			return fmt.Errorf("cloudvocab: row %d has no word", i)
		}
		if i > 0 && ops[i-1].Op == o.Op {
			return fmt.Errorf("cloudvocab: %s is listed twice", o.Op)
		}
		if o.Kind != Read && o.Kind != Command {
			return fmt.Errorf("cloudvocab: %s has kind %q", o.Op, o.Kind)
		}
		if o.Kind == Read && o.WriteGate {
			return fmt.Errorf("cloudvocab: %s is a read behind the write switch", o.Op)
		}
		switch o.Console {
		case Carried:
			if o.ConsoleRoute == "" {
				return fmt.Errorf("cloudvocab: %s is carried with no console route", o.Op)
			}
			if o.ConsoleSeam != Writer && o.ConsoleSeam != Reader && o.ConsoleSeam != Client {
				return fmt.Errorf("cloudvocab: %s has console seam %q", o.Op, o.ConsoleSeam)
			}
			if o.ConsoleSentence != "" {
				return fmt.Errorf("cloudvocab: %s is carried and says why it is not", o.Op)
			}
		case Deferred, NoMachineRoute:
			if o.ConsoleRoute != "" || o.ConsoleSeam != "" || o.ConsoleParsedAs != "" {
				return fmt.Errorf("cloudvocab: %s is not carried and names a console route", o.Op)
			}
			// The sentence is what a person reads out of a refusal, so it has
			// to say something they can act on.
			if len(o.ConsoleSentence) < minConsoleSentence {
				return fmt.Errorf("cloudvocab: %s is not carried and its console sentence %q says too little to act on", o.Op, o.ConsoleSentence)
			}
		default:
			return fmt.Errorf("cloudvocab: %s has console %q", o.Op, o.Console)
		}
		if o.ConsoleParsedAs != "" && o.ConsoleSeam != Writer {
			return fmt.Errorf("cloudvocab: %s names a parsed word outside the writer", o.Op)
		}
		for _, tag := range o.Special {
			if !validSpecial(tag) {
				return fmt.Errorf("cloudvocab: %s has special %q, which is not one of cloudvocab.Specials", o.Op, tag)
			}
		}
		if len(o.Shapes) == 0 {
			return fmt.Errorf("cloudvocab: %s has no accepted body", o.Op)
		}
		named := map[string]bool{}
		seen := map[string]bool{}
		for _, s := range o.Shapes {
			if !sort.StringsAreSorted(s.Body) {
				return fmt.Errorf("cloudvocab: %s has an unsorted shape %v", o.Op, s.Body)
			}
			key := fmt.Sprint(s.Body)
			if seen[key] {
				return fmt.Errorf("cloudvocab: %s lists shape %v twice", o.Op, s.Body)
			}
			seen[key] = true
			example := o.Example
			if s.Example != nil {
				example = s.Example
			}
			for _, field := range s.Body {
				if field == "type" {
					return fmt.Errorf("cloudvocab: %s names `type` in a shape", o.Op)
				}
				if _, ok := example[field]; !ok {
					return fmt.Errorf("cloudvocab: %s shape %v has no example for %s", o.Op, s.Body, field)
				}
				named[field] = true
			}
			if (s.Path != "") != (o.Local != nil) {
				return fmt.Errorf("cloudvocab: %s shape %v and its local route disagree", o.Op, s.Body)
			}
		}
		for field := range o.Example {
			if !named[field] {
				return fmt.Errorf("cloudvocab: %s has an example for %s, which no shape names", o.Op, field)
			}
		}
		if err := validateDecode(o); err != nil {
			return err
		}
	}
	return nil
}
