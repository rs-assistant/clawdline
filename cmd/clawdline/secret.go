package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/sainteye/clawdline/internal/app"
)

var secretHandoffNoteID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// secret run keeps the value out of this command's arguments, terminal
// output and the agent's transcript. The chosen tool receives it on stdin.
func secretCommand(args []string) {
	if len(args) == 0 || args[0] != "run" {
		secretUsage()
	}
	fs := flag.NewFlagSet("secret run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port")
	if err := fs.Parse(args[1:]); err != nil {
		secretUsage()
	}
	rest := fs.Args()
	if len(rest) < 3 || rest[1] != "--" {
		secretUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	code := runSecret(os.Stdout, os.Stderr, b, rest[0], rest[2:], os.Getenv)
	os.Exit(code)
}

func secretUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "secret.usage", "usage: clawdline secret run [--port n] <note-id> -- <command> [args...]"))
	os.Exit(2)
}

func runSecret(stdout, stderr io.Writer, b *broker, noteID string, argv []string, getenv func(string) string) int {
	conversation, _, err := conversationFromEnv(getenv)
	if err != nil || conversation == "" || len(argv) == 0 {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.session_required", "Secret Handoff needs a live Session and a command."))
		return 2
	}
	if !secretHandoffNoteID.MatchString(noteID) {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.note_invalid", "The Secret Handoff Note ID is invalid."))
		return 2
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.command_unavailable", "The command was not found. The secret was not claimed."))
		return 2
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.receiver_failed", "The local Secret Handoff receiver could not start. The value was not claimed."))
		return 1
	}
	defer listener.Close()
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.receiver_failed", "The local Secret Handoff receiver could not start. The value was not claimed."))
		return 1
	}
	arrived := make(chan []byte, 1)
	go receiveSecretHandoff(listener.(*net.TCPListener), nonce, arrived)
	body, _ := json.Marshal(map[string]any{"conversation_id": conversation,
		"callback_port": listener.Addr().(*net.TCPAddr).Port, "nonce": base64.RawURLEncoding.EncodeToString(nonce)})
	req, err := http.NewRequest(http.MethodPost, b.base+"/v1/work/v2/agent/human-interventions/"+noteID+"/claim", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.request_failed", "The handoff request could not be prepared."))
		return 1
	}
	req.Header.Set("X-Clawdline-Orchestrator", b.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	var response []byte
	if err == nil {
		defer res.Body.Close()
		response, _ = io.ReadAll(io.LimitReader(res.Body, 4096))
	}
	var value []byte
	select {
	case value = <-arrived:
	default:
	}
	if len(value) == 0 && err == nil && res.StatusCode == http.StatusOK {
		select {
		case value = <-arrived:
		case <-time.After(time.Duration(app.MaxSecretHandoffDeliverySeconds) * time.Second):
		}
	}
	if len(value) == 0 {
		if err == nil && res.StatusCode != http.StatusOK {
			code, _ := (answer{Status: res.StatusCode, Body: response}).refusal()
			if code == "" {
				code = "secret_handoff_unavailable"
			}
			fmt.Fprintln(stderr, cliCopy("misc", "secret.claim_refused", "Secret Handoff was refused:"), code)
		} else {
			fmt.Fprintln(stderr, cliCopy("misc", "secret.response_invalid", "The handoff response was invalid. Check whether the value was consumed."))
		}
		return 1
	}
	defer func() {
		for i := range value {
			value[i] = 0
		}
	}()
	command := exec.Command(argv[0], argv[1:]...)
	command.Stdin = bytes.NewReader(value)
	// A consumer that echoes the value must not turn this command into a
	// transcript leak. Its status is enough for the agent to proceed.
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "secret.command_failed", "The tool failed after the one-time handoff was consumed."))
		return 1
	}
	fmt.Fprintln(stdout, cliCopy("misc", "secret.command_succeeded", "The tool completed with the one-time Secret Handoff."))
	return 0
}

// The daemon writes only to this short-lived loopback listener, authenticated
// by an unpredictable nonce. A bad local connection does not steal the slot.
func receiveSecretHandoff(listener *net.TCPListener, nonce []byte, arrived chan<- []byte) {
	deadline := time.Now().Add(time.Duration(app.MaxSecretHandoffDeliverySeconds) * time.Second)
	for time.Now().Before(deadline) {
		_ = listener.SetDeadline(deadline)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.SetDeadline(deadline)
		presented := make([]byte, len(nonce))
		_, err = io.ReadFull(conn, presented)
		if err != nil || subtle.ConstantTimeCompare(presented, nonce) != 1 {
			_ = conn.Close()
			continue
		}
		var size [4]byte
		_, err = io.ReadFull(conn, size[:])
		length := binary.BigEndian.Uint32(size[:])
		if err != nil || length == 0 || length > app.MaxSecretHandoffBytes {
			_ = conn.Close()
			return
		}
		value := make([]byte, length)
		_, err = io.ReadFull(conn, value)
		if err == nil {
			_, err = conn.Write([]byte{1})
		}
		_ = conn.Close()
		if err == nil {
			arrived <- value
		} else {
			for i := range value {
				value[i] = 0
			}
		}
		return
	}
}
