package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// remoteCommand keeps task identity visible before a network request. A caller
// can reuse the printed IDs after an uncertain response without opening a
// second target Session.
func remoteCommand(args []string) {
	if len(args) == 0 {
		remoteUsage()
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "machines":
		if len(args) != 1 {
			remoteUsage()
			os.Exit(2)
		}
		err = remoteRequest(http.MethodGet, "/v1/remote/machines", nil)
	case "inspect", "projects", "sessions":
		err = remoteInspect(args[1:])
	case "dispatch":
		err = remoteDispatch(args[1:])
	case "inbox":
		if len(args) != 2 {
			remoteUsage()
			os.Exit(2)
		}
		var session string
		session, err = remoteSession()
		if err == nil {
			err = remoteRequest(http.MethodGet, "/v1/remote/inbox/"+url.PathEscape(args[1])+"?session_id="+url.QueryEscape(session), nil)
		}
	case "show":
		if len(args) != 2 {
			remoteUsage()
			os.Exit(2)
		}
		var session string
		session, err = remoteSession()
		if err == nil {
			err = remoteRequest(http.MethodGet, "/v1/remote/tasks/"+url.PathEscape(args[1])+"?session_id="+url.QueryEscape(session), nil)
		}
	case "send", "finish":
		err = remoteEffect(args[0], args[1:])
	case "ack":
		if len(args) != 2 {
			remoteUsage()
			os.Exit(2)
		}
		var session string
		session, err = remoteSession()
		if err == nil {
			err = remoteRequest(http.MethodPost, "/v1/remote/tasks/"+url.PathEscape(args[1])+"/ack", map[string]any{"session_id": session})
		}
	case "retry":
		if len(args) != 2 {
			remoteUsage()
			os.Exit(2)
		}
		err = remoteRequest(http.MethodPost, "/v1/remote/tasks/"+url.PathEscape(args[1])+"/retry", map[string]any{})
	default:
		remoteUsage()
		os.Exit(2)
	}
	if err != nil {
		fail(err)
	}
}

func remoteUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.inspect", "       clawdline remote inspect --machine ID [--pair ID] (Projects, Sessions and assistants)"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.machines", "usage: clawdline remote machines"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.dispatch", "       clawdline remote dispatch --machine ID [--pair ID] --project ID --assistant NAME --title TITLE --instructions-file PATH [--task-id UUID --request-id UUID]"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.show", "       clawdline remote show TASK-ID"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.send", "       clawdline remote send TASK-ID TEXT [--request-id UUID]"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.finish", "       clawdline remote finish TASK-ID TEXT [--request-id UUID]"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.retry", "       clawdline remote retry TASK-ID"))
	fmt.Fprintln(os.Stderr, cliCopy("remote", "usage.ack", "       clawdline remote ack TASK-ID"))
}

func remoteSession() (string, error) {
	id, _, err := conversationFromEnv(os.Getenv)
	if err != nil || id == "" {
		return "", errors.New(cliCopy("remote", "error.session", "a current assistant Session is required"))
	}
	return id, nil
}

func remoteDispatch(args []string) error {
	fs := flag.NewFlagSet("remote dispatch", flag.ContinueOnError)
	machine := fs.String("machine", "", "target machine")
	pair := fs.String("pair", "", "pair")
	project := fs.String("project", "", "target local project")
	assistant := fs.String("assistant", "", "assistant")
	title := fs.String("title", "", "task title")
	file := fs.String("instructions-file", "", "instructions file")
	taskID := fs.String("task-id", "", "retry task ID")
	requestID := fs.String("request-id", "", "retry request ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *machine == "" || *project == "" || *assistant == "" || *title == "" || *file == "" {
		return errors.New(cliCopy("remote", "error.dispatch_fields", "machine, project, assistant, title and instructions file are required"))
	}
	if (*taskID == "") != (*requestID == "") {
		return errors.New(cliCopy("remote", "error.retry_ids", "task ID and request ID must be supplied together"))
	}
	text, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	if len(text) == 0 || len(text) > 512<<10 {
		return errors.New(cliCopy("remote", "error.instructions_size", "instructions must be between 1 and 524288 bytes"))
	}
	session, err := remoteSession()
	if err != nil {
		return err
	}
	if *taskID == "" {
		*taskID, *requestID = newUUID(), newUUID()
	}
	fmt.Printf(cliCopy("remote", "dispatch_ids", "task %s request %s; reuse both IDs if the reply is uncertain\n"), *taskID, *requestID)
	return remoteRequest(http.MethodPost, "/v1/remote/tasks", map[string]any{
		"task_id": *taskID, "request_id": *requestID, "source_session": session,
		"machine_id": *machine, "pair_id": *pair, "project_id": *project,
		"assistant": *assistant, "title": *title, "instructions": string(text),
	})
}

func remoteEffect(action string, args []string) error {
	if len(args) < 2 {
		return errors.New(cliCopy("remote", "error.effect_fields", "task ID and text are required"))
	}
	fs := flag.NewFlagSet("remote "+action, flag.ContinueOnError)
	requestID := fs.String("request-id", "", "retry request ID")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(args[1]) == "" {
		return errors.New(cliCopy("remote", "error.effect_fields", "task ID and text are required"))
	}
	session, err := remoteSession()
	if err != nil {
		return err
	}
	if *requestID == "" {
		*requestID = newUUID()
	}
	fmt.Printf(cliCopy("remote", "event_id", "request %s; reuse this ID if the reply is uncertain\n"), *requestID)
	path := "/v1/remote/tasks/" + url.PathEscape(args[0]) + "/messages"
	if action == "finish" {
		path = "/v1/remote/tasks/" + url.PathEscape(args[0]) + "/finish"
	}
	return remoteRequest(http.MethodPost, path, map[string]any{"session_id": session, "request_id": *requestID, "text": args[1]})
}

func remoteRequest(method, path string, body any) error {
	req, err := daemonRequest(method, path, body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	res, err := (&http.Client{Timeout: 35 * time.Second}).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(refusalText(res))
	}
	var value any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&value); err != nil {
		return err
	}
	output, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}

func remoteInspect(args []string) error {
	fs := flag.NewFlagSet("remote inspect", flag.ContinueOnError)
	machine := fs.String("machine", "", "target machine")
	pair := fs.String("pair", "", "pair (selected automatically when unambiguous)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *machine == "" {
		return errors.New(cliCopy("remote", "error.machine", "a target machine is required"))
	}
	session, err := remoteSession()
	if err != nil {
		return err
	}
	return remoteRequest(http.MethodPost, "/v1/remote/inspect", map[string]any{"machine_id": *machine, "pair_id": *pair, "source_session": session})
}
