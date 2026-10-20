package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

// runSquadCatalog uses only the published Agent routes and the existing
// machine credential. No browser/local-person credential is read.
func runSquadCatalog(stdout, stderr io.Writer, args []string, b *broker) int {
	usage := cliCopy("core", "squad.catalog_usage", "usage: clawdline squad <catalog|definition|scopes|settings|save|bind|icon> [--body-file FILE|-] [--key KEY] [--id ID] [--version VERSION] [--expected-version N] [--place-id ID|--scope-id ID] [--json] [--port N]; see clawdline guide squad")
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	fs := flag.NewFlagSet("squad", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("body-file", "", cliCopy("core", "squad.file_help", "JSON request file, or - for stdin"))
	key := fs.String("key", "", cliCopy("core", "squad.key_help", "stable Idempotency-Key for this write and its retries"))
	id := fs.String("id", "", "ID")
	version := fs.String("version", "", "version")
	expected := fs.Int64("expected-version", -1, "expected_version")
	place := fs.String("place-id", "", "place_id")
	scope := fs.String("scope-id", "", "scope_id")
	raw := fs.Bool("json", false, "JSON")
	port := fs.Int("port", 0, "port")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	method, path := http.MethodGet, "/v1/squad/"+args[0]
	query := url.Values{}
	var body any
	switch args[0] {
	case "catalog", "scopes":
	case "definition":
		if *id == "" || strings.ContainsAny(*id, "/?#%") {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		path = "/v1/squad/definitions/" + *id
		if *version != "" {
			query.Set("version", *version)
		}
	case "settings":
		if *place != "" {
			query.Set("place_id", *place)
		}
		if *scope != "" {
			query.Set("scope_id", *scope)
		}
	case "save", "bind", "icon":
		if *file == "" || *key == "" {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		var reader io.Reader = os.Stdin
		if *file != "-" {
			f, err := os.Open(*file)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			defer f.Close()
			reader = f
		}
		data, err := io.ReadAll(io.LimitReader(reader, squad.MaxSquadRequestBytes+1))
		if err != nil || len(data) > squad.MaxSquadRequestBytes || !json.Valid(data) {
			fmt.Fprintln(stderr, cliCopy("core", "squad.invalid_json", "clawdline squad: bad_request: supply one JSON request within the Squad request limit"))
			return 2
		}
		body = json.RawMessage(data)
		method, path = http.MethodPost, "/v1/squad/catalog"
		if args[0] == "bind" {
			method, path = http.MethodPut, "/v1/squad/settings"
		}
		if args[0] == "icon" {
			if *id == "" || *version == "" || *expected < 0 {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			var icon squad.Icon
			if json.Unmarshal(data, &icon) != nil {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			// Read the entire versioned definition, retaining names, attribution,
			// body and pinned references. Settings are stored separately and untouched.
			if b == nil {
				b, err = openBroker(*port)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			answer, err := b.request(http.MethodGet, "/v1/squad/catalog", nil, nil, "")
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if !answer.ok() {
				if *raw {
					fmt.Fprintln(stdout, string(answer.Body))
					return 1
				}
				return report(stdout, stderr, "squad", answer)
			}
			var catalog squad.Catalog
			if err = json.Unmarshal(answer.Body, &catalog); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			found := false
			for _, definition := range catalog.Definitions {
				if definition.DefinitionID == *id {
					if definition.Builtin || !squad.ValidCustomID(*id) {
						return squadCLIRefusal(stdout, stderr, *raw, "builtin_read_only")
					}
					definition.Icon = icon
					definition.Version = *version
					definition.Digest = ""
					body = map[string]any{"kind": "definition", "expected_version": *expected, "entity": map[string]any{"definition": definition}}
					found = true
					break
				}
			}
			if !found {
				return squadCLIRefusal(stdout, stderr, *raw, "unknown_definition")
			}
		}
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if fs.NArg() != 0 || (*place != "" && *scope != "") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	if b == nil {
		b, err = openBroker(*port)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if *key != "" {
		fmt.Fprintln(stderr, "Idempotency-Key:", *key)
	}
	a, err := b.request(method, path, query, body, *key)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *raw {
		fmt.Fprintln(stdout, string(a.Body))
		if !a.ok() {
			return 1
		}
		return 0
	}
	return report(stdout, stderr, "squad", a)
}

func squadCLIRefusal(stdout, stderr io.Writer, raw bool, code string) int {
	body, _ := json.Marshal(map[string]string{"error": code})
	if raw {
		fmt.Fprintln(stdout, string(body))
		return 1
	}
	return report(stdout, stderr, "squad", answer{Status: http.StatusBadRequest, Body: body})
}
