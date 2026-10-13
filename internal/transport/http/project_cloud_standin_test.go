package http

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func projectCloudStandIn(t *testing.T) *standIn {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(home, "swift"))
	state := filepath.Join(home, "state")
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{Dir: state, Port: 7757}, store: st, icons: icons,
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(state), Dir: state}}
	server.projectReaders().places.Fixture = []string{project}
	places := server.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatalf("fixture places: %v", places)
	}
	handler := server.Handler()
	local, machine, err := server.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return &standIn{server: server, handler: handler, place: places[0].ID, machine: machine,
		bridge: cloudops.Bridge{MachineID: "mac-01", Router: cloudtransport.Router{Handler: handler, Authorize: cloudtransport.LocalAuthorizer(local, machine)}, AllowCommands: func() bool { return true }}}
}
