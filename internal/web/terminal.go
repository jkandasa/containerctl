package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

type wsMsg struct {
	Type  string   `json:"type"`
	Data  string   `json:"data,omitempty"`
	Code  int      `json:"code,omitempty"`
	Msg   string   `json:"msg,omitempty"`
	Names []string `json:"names,omitempty"`
}

type cmdMsg struct {
	Cmd string `json:"cmd"`
}

// connState holds per-WebSocket-connection mutable state.
type connState struct {
	activeFile string // current stack file; starts as s.cfg.StackFile, changed by "use"
}

// cmdHelp maps each allowed command to its one-line description shown by "help".
var cmdHelp = map[string]string{
	"apply":        "reconcile host to desired state defined in the stack file",
	"check-update": "check container images for available updates",
	"clear":        "clear the terminal screen",
	"diff":         "show what apply would change without making any changes",
	"disable":      "persistently disable a container (survives apply)",
	"down":         "stop and remove managed containers",
	"edit":         "open the active stack file in the browser editor (vim keybindings)",
	"enable":       "re-enable a previously disabled container",
	"exec":         "run a non-interactive command in a container  e.g. exec <name> env",
	"help":         "show this help",
	"images":       "list local images  (flag: --unused)",
	"logs":         "stream container logs  (flags: --follow  --tail N)",
	"networks":     "list networks  (flag: --unused)",
	"prune":        "remove unused resources  (flags: --images --volumes --networks --all --dry-run --force)",
	"pull":         "pull images without reconciling",
	"restart":      "stop, remove, recreate, and start containers",
	"start":        "start a stopped managed container",
	"status":       "show state and sync status of managed containers",
	"stop":         "transiently stop a container (apply will restart it)",
	"upgrade":      "force-pull and recreate a container",
	"use":          "switch active stack file  e.g. use /path/to/other/stack.yaml",
	"version":      "print binary version and runtime info",
	"volumes":      "list local volumes  (flag: --unused)",
}

var allowedCmds = func() map[string]bool {
	m := make(map[string]bool, len(cmdHelp))
	for k := range cmdHelp {
		m[k] = true
	}
	return m
}()

func promptString(file string) string {
	if file == "" {
		return "containerctl> "
	}
	return fmt.Sprintf("containerctl [%s]> ", filepath.Base(file))
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	state := &connState{activeFile: s.cfg.StackFile}

	var (
		mu      sync.Mutex
		running bool
		cancel  context.CancelFunc
	)

	send := func(msg wsMsg) {
		mu.Lock()
		defer mu.Unlock()
		_ = conn.WriteJSON(msg)
	}

	finishCmd := func() {
		mu.Lock()
		running = false
		mu.Unlock()
	}

	// Seed the client's prompt and container name list.
	send(wsMsg{Type: "prompt", Data: promptString(state.activeFile)})
	send(wsMsg{Type: "names", Names: s.getContainerNames(state.activeFile)})

	for {
		var m cmdMsg
		if err := conn.ReadJSON(&m); err != nil {
			return
		}

		if m.Cmd == "__interrupt__" {
			mu.Lock()
			if cancel != nil {
				cancel()
			}
			mu.Unlock()
			continue
		}

		mu.Lock()
		if running {
			mu.Unlock()
			send(wsMsg{Type: "error", Msg: "command already running"})
			continue
		}
		running = true
		mu.Unlock()

		parts := strings.Fields(strings.TrimSpace(m.Cmd))
		if len(parts) == 0 {
			finishCmd()
			send(wsMsg{Type: "done"})
			continue
		}

		name := parts[0]

		// ── built-in commands (no subprocess) ────────────────────────────────

		if name == "help" {
			if len(parts) > 1 {
				// "help apply" → run as subprocess: containerctl apply --help
				subcmd := parts[1]
				if !allowedCmds[subcmd] {
					send(wsMsg{Type: "error", Msg: fmt.Sprintf("unknown command %q; type help for available commands", subcmd)})
					finishCmd()
					continue
				}
				ctx, cancelFn := context.WithCancel(r.Context())
				mu.Lock()
				cancel = cancelFn
				mu.Unlock()
				go func(subcmd, activeFile string) {
					defer func() {
						cancelFn()
						mu.Lock()
						running = false
						cancel = nil
						mu.Unlock()
					}()
					code := s.execCommand(ctx, activeFile, []string{subcmd, "--help"}, func(data string) {
						send(wsMsg{Type: "output", Data: data})
					})
					send(wsMsg{Type: "done", Code: code})
				}(subcmd, state.activeFile)
				continue
			}
			// No argument: show the command table.
			cmds := make([]string, 0, len(cmdHelp))
			for k := range cmdHelp {
				cmds = append(cmds, k)
			}
			sort.Strings(cmds)
			maxLen := 0
			for _, c := range cmds {
				if len(c) > maxLen {
					maxLen = len(c)
				}
			}
			var sb strings.Builder
			sb.WriteString("Available commands:\r\n\r\n")
			for _, c := range cmds {
				fmt.Fprintf(&sb, "  %-*s  %s\r\n", maxLen, c, cmdHelp[c])
			}
			sb.WriteString("\r\nTip: type \"help <command>\" or \"<command> --help\" for detailed flags.\r\n")
			send(wsMsg{Type: "output", Data: sb.String()})
			finishCmd()
			send(wsMsg{Type: "done"})
			continue
		}

		if name == "clear" {
			send(wsMsg{Type: "clear"})
			finishCmd()
			send(wsMsg{Type: "done"})
			continue
		}

		if name == "edit" {
			if !s.cfg.EditEnabled {
				send(wsMsg{Type: "error", Msg: "edit is disabled; set serve.edit.enabled: true in your stack.yaml and restart"})
				finishCmd()
				send(wsMsg{Type: "done"})
				continue
			}
			send(wsMsg{Type: "edit", Data: state.activeFile})
			finishCmd()
			continue
		}

		if name == "use" {
			if !s.cfg.UseEnabled {
				send(wsMsg{Type: "error", Msg: "use is disabled; set serve.use.enabled: true in your stack.yaml and restart"})
				finishCmd()
				send(wsMsg{Type: "done"})
				continue
			}
			if len(parts) < 2 {
				send(wsMsg{Type: "error", Msg: "usage: use <path-to-stack.yaml>"})
				finishCmd()
				continue
			}
			abs, err := filepath.Abs(parts[1])
			if err != nil || !fileExists(abs) {
				send(wsMsg{Type: "error", Msg: fmt.Sprintf("file not found: %s", parts[1])})
				finishCmd()
				continue
			}
			state.activeFile = abs
			send(wsMsg{Type: "output", Data: fmt.Sprintf("Active stack: %s\r\n", abs)})
			send(wsMsg{Type: "prompt", Data: promptString(abs)})
			send(wsMsg{Type: "names", Names: s.getContainerNames(abs)})
			finishCmd()
			send(wsMsg{Type: "done"})
			continue
		}

		if !allowedCmds[name] {
			send(wsMsg{Type: "error", Msg: fmt.Sprintf("unknown command %q; type help for available commands", name)})
			finishCmd()
			continue
		}

		if name == "exec" {
			if !s.cfg.ExecEnabled {
				send(wsMsg{Type: "error", Msg: "exec is disabled; set serve.exec.enabled: true in your stack.yaml and restart"})
				finishCmd()
				send(wsMsg{Type: "done"})
				continue
			}
			containerName := execContainerName(parts)
			if !s.execContainerAllowed(containerName) {
				send(wsMsg{Type: "error", Msg: fmt.Sprintf("%q is not in the exec allowlist; add it under serve.exec.allowed in your stack.yaml and restart", containerName)})
				finishCmd()
				send(wsMsg{Type: "done"})
				continue
			}
			if isInteractiveExec(parts) {
				// Open a dedicated PTY-based WebSocket for this interactive session.
				params := url.Values{"name": {containerName}, "file": {state.activeFile}}
				if len(parts) > 2 {
					params.Set("cmd", strings.Join(parts[2:], " "))
				}
				send(wsMsg{Type: "exec_open", Data: params.Encode()})
				finishCmd()
				continue
			}
			// Non-interactive exec falls through to subprocess dispatch below.
		}

		// ── subprocess dispatch ───────────────────────────────────────────────

		ctx, cancelFn := context.WithCancel(r.Context())
		mu.Lock()
		cancel = cancelFn
		mu.Unlock()

		go func(parts []string, activeFile string) {
			defer func() {
				cancelFn()
				mu.Lock()
				running = false
				cancel = nil
				mu.Unlock()
			}()
			code := s.execCommand(ctx, activeFile, parts, func(data string) {
				send(wsMsg{Type: "output", Data: data})
			})
			send(wsMsg{Type: "done", Code: code})
		}(parts, state.activeFile)
	}
}

func (s *Server) handleLogsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	name := r.URL.Query().Get("name")
	if name == "" {
		_ = conn.WriteJSON(wsMsg{Type: "error", Msg: "name query parameter required"})
		return
	}

	// Honour an explicit ?file= param; fall back to the server default.
	activeFile := s.cfg.StackFile
	if f := r.URL.Query().Get("file"); f != "" {
		if abs, err := filepath.Abs(f); err == nil {
			activeFile = abs
		}
	}

	args := []string{"logs", name}
	if r.URL.Query().Get("follow") == "true" {
		args = append(args, "--follow")
	}
	if tail := r.URL.Query().Get("tail"); tail != "" {
		args = append(args, "--tail", tail)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Any incoming message (or close) interrupts the log stream.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	var mu sync.Mutex
	send := func(msg wsMsg) {
		mu.Lock()
		defer mu.Unlock()
		_ = conn.WriteJSON(msg)
	}

	code := s.execCommand(ctx, activeFile, args, func(data string) {
		send(wsMsg{Type: "output", Data: data})
	})
	send(wsMsg{Type: "done", Code: code})
}

func (s *Server) execCommand(ctx context.Context, activeFile string, args []string, write func(string)) int {
	fullArgs := append(s.buildGlobalFlags(activeFile, args), args...)
	cmd := exec.CommandContext(ctx, s.cfg.Executable, fullArgs...)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		write(fmt.Sprintf("error: %v\r\n", err))
		_ = pw.Close()
		_ = pr.Close()
		return 1
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				write(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	_ = pw.Close()
	wg.Wait()
	_ = pr.Close()

	if waitErr != nil {
		if ctx.Err() != nil {
			return 130
		}
		if ee, ok := waitErr.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// buildGlobalFlags constructs the flags that precede every subprocess invocation.
// It omits --file if the user already supplied -f or --file in their args.
func (s *Server) buildGlobalFlags(activeFile string, userArgs []string) []string {
	var flags []string
	if activeFile != "" && !hasFileFlag(userArgs) {
		flags = append(flags, "--file", activeFile)
	}
	if s.cfg.RuntimeName != "" {
		flags = append(flags, "--runtime", s.cfg.RuntimeName)
	}
	if s.cfg.Socket != "" {
		flags = append(flags, "--socket", s.cfg.Socket)
	}
	if s.cfg.Project != "" {
		flags = append(flags, "--project", s.cfg.Project)
	}
	return flags
}

func hasFileFlag(args []string) bool {
	for _, a := range args {
		if a == "--file" || a == "-f" {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// interactiveShells is the set of shell binary names that require a TTY.
var interactiveShells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "ash": true,
	"fish": true, "ksh": true, "csh": true, "tcsh": true, "dash": true,
}

// isInteractiveExec returns true when the exec invocation is almost certainly
// going to open an interactive shell that needs real stdin — which the web
// terminal cannot provide in its subprocess model.
//
//   exec <name>              → no command specified; defaults to /bin/sh
//   exec <name> bash         → bare shell, no -c flag
//   exec <name> /bin/bash    → same with full path
func isInteractiveExec(parts []string) bool {
	// parts[0]="exec"  parts[1]=<name>  parts[2+]=<cmd args...>
	if len(parts) < 3 {
		// No command given → default /bin/sh → interactive.
		return true
	}
	// Walk args after the container name to find the command (first non-flag).
	for i := 2; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "-") {
			continue
		}
		if interactiveShells[filepath.Base(parts[i])] {
			// Shell name found — interactive unless -c follows.
			for j := i + 1; j < len(parts); j++ {
				if parts[j] == "-c" {
					return false // e.g. bash -c "echo hello" — non-interactive
				}
			}
			return true
		}
		return false // first non-flag arg is not a shell → non-interactive command
	}
	return false
}

// execContainerName extracts the container name from exec parts for use in
// help messages; returns "<name>" as a fallback.
func execContainerName(parts []string) string {
	if len(parts) >= 2 {
		return parts[1]
	}
	return "<name>"
}

// execContainerAllowed reports whether the server configuration permits an
// interactive exec session into the named container. An empty allowlist means
// all containers are permitted when exec is enabled.
func (s *Server) execContainerAllowed(containerName string) bool {
	if len(s.cfg.ExecAllowed) == 0 {
		return true
	}
	for _, n := range s.cfg.ExecAllowed {
		if n == containerName {
			return true
		}
	}
	return false
}

// execInputMsg is received from the client over /ws/exec.
type execInputMsg struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

// handleExecWS runs a containerctl exec session inside a PTY and bridges it
// to the browser via WebSocket. The client sends execInputMsg frames; the
// server sends wsMsg frames of type "output" and "done".
func (s *Server) handleExecWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if !s.cfg.ExecEnabled {
		_ = conn.WriteJSON(wsMsg{Type: "error", Msg: "exec is disabled; set serve.exec.enabled: true in your stack.yaml and restart"})
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		_ = conn.WriteJSON(wsMsg{Type: "error", Msg: "name required"})
		return
	}
	if !s.execContainerAllowed(name) {
		_ = conn.WriteJSON(wsMsg{Type: "error", Msg: fmt.Sprintf("%q is not in the exec allowlist; add it under serve.exec.allowed in your stack.yaml and restart", name)})
		return
	}

	activeFile := s.cfg.StackFile
	if f := r.URL.Query().Get("file"); f != "" {
		if abs, err := filepath.Abs(f); err == nil {
			activeFile = abs
		}
	}

	execArgs := []string{"exec", name}
	if shellStr := r.URL.Query().Get("cmd"); shellStr != "" {
		execArgs = append(execArgs, strings.Fields(shellStr)...)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	fullArgs := append(s.buildGlobalFlags(activeFile, execArgs), execArgs...)
	cmd := exec.CommandContext(ctx, s.cfg.Executable, fullArgs...)

	// Parse the initial terminal size from query params so the subprocess
	// sees the correct dimensions from the very first TIOCGWINSZ call.
	// This is what allows vi/vim to open in full-screen mode.
	rows, cols := uint16(24), uint16(80)
	if rowStr, colStr := r.URL.Query().Get("rows"), r.URL.Query().Get("cols"); rowStr != "" && colStr != "" {
		var r2, c2 uint16
		fmt.Sscan(rowStr, &r2)
		fmt.Sscan(colStr, &c2)
		if r2 > 0 && c2 > 0 {
			rows, cols = r2, c2
		}
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		_ = conn.WriteJSON(wsMsg{Type: "error", Msg: "exec failed: " + err.Error()})
		return
	}
	defer ptmx.Close()

	var mu sync.Mutex
	send := func(msg wsMsg) {
		mu.Lock()
		defer mu.Unlock()
		_ = conn.WriteJSON(msg)
	}

	// PTY → WebSocket: runs until the process exits (ptmx returns EIO).
	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				send(wsMsg{Type: "output", Data: string(buf[:n])})
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY: cancel ctx (kills subprocess) on disconnect.
	go func() {
		for {
			var m execInputMsg
			if err := conn.ReadJSON(&m); err != nil {
				cancel()
				return
			}
			switch m.Type {
			case "input":
				_, _ = ptmx.Write([]byte(m.Data))
			case "resize":
				if m.Rows > 0 && m.Cols > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Rows: m.Rows, Cols: m.Cols})
				}
			}
		}
	}()

	// Wait for the subprocess to exit (signalled by the PTY reader closing).
	<-ptyDone

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}
	send(wsMsg{Type: "done", Code: exitCode})
}

// getContainerNames runs "status --output json" against the given stack file
// and returns the logical container names. Used to seed client Tab completion.
func (s *Server) getContainerNames(activeFile string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"status", "--output", "json"}
	fullArgs := append(s.buildGlobalFlags(activeFile, args), args...)
	out, err := exec.CommandContext(ctx, s.cfg.Executable, fullArgs...).Output()
	if err != nil {
		return nil
	}
	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}
