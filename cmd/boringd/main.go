package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/alebeck/boring/internal/daemon"
	"github.com/alebeck/boring/internal/ipc"
	"github.com/alebeck/boring/internal/log"
	"github.com/alebeck/boring/internal/tunnel"
)

var tunnels = make(map[string]*tunnel.Tunnel)
var mutex sync.RWMutex

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Daemon called with args: %v", os.Args[1:])
	}

	initLogger(os.Args[2])
	log.Infof("Daemon starting with args: %v", os.Args[1:])

	l, err := setupListener()
	if err != nil {
		log.Fatalf("Failed to setup listener: %v", err)
	}
	defer l.Close()
	go handleCleanup(l)

	// Start handling incoming connections
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Errorf("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

// Logic for socket cleanup on TERM/INT signal. On
// SIGKILL and other abrupt interruptions, the socket
// file is likely not cleaned up, and has to be deleted
// manually. TODO: Detect & fix this state in the CLI.
func handleCleanup(l net.Listener) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	log.Infof("Received signal: %s. Cleaning up...", <-sig)

	l.Close()

	mutex.Lock()
	for _, t := range tunnels {
		t.Close()
	}

	os.Exit(0)
}

func initLogger(path string) {
	logFile, err := os.OpenFile(path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	log.SetOutput(logFile)
}

func setupListener() (net.Listener, error) {
	return net.Listen("unix", daemon.SOCK)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Receive command
	var cmd daemon.Command
	if err := ipc.Receive(&cmd, conn); err != nil {
		log.Errorf("Could not receive command: %v", err)
		return
	}

	// Execute command
	switch cmd.Kind {
	case daemon.Open:
		openTunnel(conn, cmd.Tunnel)
	case daemon.Close:
		closeTunnel(conn, cmd.Tunnel)
	case daemon.List:
		listTunnels(conn)
	default:
		unknownCmd(conn, cmd.Kind)
	}
}

func respond(conn net.Conn, err *error) {
	resp := daemon.Response{Success: true}
	if *err != nil {
		resp = daemon.Response{Success: false, Error: (*err).Error()}
	}
	if err := ipc.Send(resp, conn); err != nil {
		log.Errorf("could not send response: %v", err)
	}
}

func openTunnel(conn net.Conn, t tunnel.Tunnel) {
	// TODO check if already running, i.e. whether its in the map
	var err error
	defer respond(conn, &err)

	if err = t.Open(); err != nil {
		return
	}

	mutex.Lock()
	tunnels[t.Name] = &t
	mutex.Unlock()

	// Register closing logic
	go func() {
		<-t.Closed
		mutex.Lock()
		delete(tunnels, t.Name)
		mutex.Unlock()
		log.Infof("Closed tunnel %s", t.Name)
	}()
}

func closeTunnel(conn net.Conn, q tunnel.Tunnel) {
	var err error
	defer respond(conn, &err)

	mutex.RLock()
	t, ok := tunnels[q.Name]
	mutex.RUnlock()
	if !ok {
		err = fmt.Errorf("tunnel not running")
		return
	}

	if err = t.Close(); err != nil {
		err = fmt.Errorf("could not close tunnel: %v", err)
		return
	}
	mutex.Lock()
	delete(tunnels, t.Name)
	mutex.Unlock()
}

func listTunnels(conn net.Conn) {
	m := make(map[string]tunnel.Tunnel)
	mutex.RLock()
	for n, t := range tunnels {
		m[n] = *t
	}
	mutex.RUnlock()

	resp := daemon.Response{Success: true, Tunnels: m}
	if err := ipc.Send(resp, conn); err != nil {
		log.Errorf("could not send response: %v", err)
	}
}

func unknownCmd(conn net.Conn, k daemon.CommandKind) {
	err := fmt.Errorf("unknown command: %v", k)
	respond(conn, &err)
}