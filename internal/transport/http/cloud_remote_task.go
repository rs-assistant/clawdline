package http

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// PeerTaskStartOpen is called only after target admission and a durable
// receipt claim. The target resolves the Project locally and saves its own
// task record before starting any Agent process.
func (s *Server) PeerTaskStartOpen(ctx context.Context, start agenthandoff.TaskStart,
	raw []byte) (string, string, error) {
	if s.broker == nil || s.cfg.Dir == "" || start.TargetMachineID != s.executionMachine() {
		return "", "", errors.New("peer_task_target_unavailable")
	}
	body, err := agenthandoff.ParseTaskBody(raw)
	if err != nil {
		return "", "", err
	}
	projectDir, ok := s.terminalProjectDir(ctx, start.TargetProjectID)
	if !ok || !filepath.IsAbs(projectDir) {
		return "", "", errors.New("peer_task_project_unavailable")
	}
	info, err := os.Stat(projectDir)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("peer_task_project_unavailable")
	}
	st, err := peerstore.Open(filepath.Join(s.cfg.Dir, cloudkeys.DirName))
	if err != nil {
		return "", "", err
	}
	defer st.Close()
	if _, err := st.RemoteTask(ctx, start.TaskID); err == nil {
		return "", "", errors.New("peer_task_opening_already_claimed")
	} else if !errors.Is(err, peerstore.ErrNotFound) {
		return "", "", err
	}
	if err := st.SaveRemoteTask(ctx, peerstore.RemoteTask{Start: start, Body: string(raw),
		State: "accepted", At: time.Now().UTC()}); err != nil {
		return "", "", err
	}
	terminalID, err := s.broker.OpenRemoteTask(ctx, start.TaskID, projectDir, body)
	if err != nil {
		return "", "", err
	}
	// The terminal opened, but its current process still needs a fresh kernel
	// pin. An uncertain observation never turns into a claimed opening.
	inv := s.freshReading(ctx)
	items := make([]session.Session, 0, len(inv.Sessions))
	lives := make([]swiftstore.Live, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if item.IsAssistant() {
			items = append(items, item)
			lives = append(lives, liveOf(item))
		}
	}
	generation := s.observeExecutions(ctx, inv, items, lives)[terminalID]
	if generation == "" {
		return "", "", errors.New("peer_task_opening_unverified")
	}
	if err := st.MarkRemoteTaskOpened(context.WithoutCancel(ctx), start.TaskID, terminalID, generation); err != nil {
		return "", "", err
	}
	return terminalID, generation, nil
}
