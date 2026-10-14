package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/contract"
)

var errProjectNotListed = errors.New("project_not_listed")

type projectNotListedError struct{ path string }

func (e projectNotListedError) Error() string {
	return fmt.Sprintf(cliCopy("misc", "project.refresh_unknown", "this directory is not in the current Project list: %s"), e.path)
}
func (e projectNotListedError) Is(target error) bool { return target == errProjectNotListed }

// runProjectRefresh asks the running daemon for its current Project and
// Session presentations. Neither request starts a server or deploys anything.
func runProjectRefresh(args []string, out io.Writer) error {
	if len(args) > 1 {
		return errors.New(cliCopy("misc", "project.refresh_usage_error", "usage: clawdline project refresh [directory]"))
	}
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	path = filepath.Clean(path)
	b, err := openBroker(0)
	if err != nil {
		return err
	}
	return refreshProject(path, out, func(route string) (answer, error) {
		return b.request(http.MethodGet, route, nil, nil, "")
	})
}

func refreshProject(path string, out io.Writer, request func(string) (answer, error)) error {
	a, err := request("/v1/places")
	if err != nil {
		return err
	}
	if !a.ok() {
		code, message := a.refusal()
		return fmt.Errorf("%s: %s (%d)", code, message, a.Status)
	}
	var list contract.StartPlaceList
	if err := json.Unmarshal(a.Body, &list); err != nil {
		return fmt.Errorf(cliCopy("misc", "project.refresh_invalid_project", "invalid Project answer: %w"), err)
	}
	if list.Places == nil {
		return errors.New(cliCopy("misc", "project.refresh_missing_places", "the daemon did not return a Project list"))
	}
	var chosen *contract.StartPlace
	for i := range list.Places {
		if filepath.Clean(list.Places[i].Path) == path {
			chosen = &list.Places[i]
			break
		}
	}
	if chosen == nil {
		return projectNotListedError{path}
	}
	if chosen.Icon == nil || chosen.Setup == nil {
		return errors.New(cliCopy("misc", "project.refresh_missing_state", "the daemon did not return this Project's display state"))
	}
	// The Session route overlays current icons onto its held rows, so this read
	// also checks that the Session presentation can be served now.
	a, err = request("/v1/sessions?parts=list")
	if err != nil {
		return err
	}
	if !a.ok() {
		code, message := a.refusal()
		return fmt.Errorf("%s: %s (%d)", code, message, a.Status)
	}
	var sessions struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	if json.Unmarshal(a.Body, &sessions) != nil || sessions.Sessions == nil {
		return errors.New(cliCopy("misc", "project.refresh_invalid_session", "invalid Session answer"))
	}
	setup := chosen.Setup
	_, err = fmt.Fprintf(out, cliCopy("misc", "project.refresh_output", "%s\nicon: %s (%s)\nrepo: %s\ndeploy: %s (%s)\nservers: %s (%d)\nsync: %s\nunify: %s\nstatus: refreshed\n"),
		path, chosen.Icon.Accent, setup.Icon, chosen.Repo, setup.Deploy, setup.DeployActivity,
		setup.Servers, setup.ServerCount, setup.Sync, setup.Unify)
	return err
}
