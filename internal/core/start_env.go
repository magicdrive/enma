package core

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"github.com/magicdrive/enma/internal/common"
)

// startEnv is what a runner's Start sets up around its work, the same in every
// mode: the pid file, the log file and the signal handler. The pid file is
// owned by Start: it is removed when Start returns an error and when the signal
// handler exits the process. Nothing removes it after SIGKILL or a crash. The
// same goes for what is registered with atExit.
type startEnv struct {
	pidPath  string
	logFile  *os.File
	mu       sync.Mutex
	cleanups []func()
}

// openStartEnv writes the pid file and sends the log to the log file as well as
// stdout. A log file that cannot be opened is reported on stderr and the run
// goes on logging to stdout only, as before.
func openStartEnv(pidPath, logPath string) (*startEnv, error) {
	env := &startEnv{pidPath: pidPath}
	if pidPath != "" {
		if err := common.CreatePidFile(pidPath); err != nil {
			return nil, err
		}
	}
	if logPath != "" {
		f, err := openLogFile(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  cannot open the log file (logging to stdout only): %v\n", err)
		} else {
			env.logFile = f
			log.SetOutput(teeWriter{os.Stdout, f})
		}
	}
	keepRunningOnBrokenPipe()
	return env, nil
}

// teeWriter writes to every writer, whatever the others do: the log file keeps
// getting the log when stdout is gone (a closed pipe), and the other way round.
// A failed write is not reported; logging never stops enma.
type teeWriter []io.Writer

func (t teeWriter) Write(p []byte) (int, error) {
	for _, w := range t {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

// openLogFile opens path for appending, creating it and its directories. A new
// log file is readable by its owner only (0600, before the umask): the log
// names files, jobs and errors of the project. An existing file keeps its mode.
func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}

// releasePid removes the pid file if it still holds this process's pid.
func (e *startEnv) releasePid() {
	if e == nil || e.pidPath == "" {
		return
	}
	if err := common.DeletePidFile(e.pidPath); err != nil {
		log.Printf("failed delete pidfile.: %s\n", err.Error())
	}
}

// atExit registers f to run when Start returns an error or the signal handler
// exits the process.
func (e *startEnv) atExit(f func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cleanups = append(e.cleanups, f)
}

// runAtExit runs what atExit registered, once.
func (e *startEnv) runAtExit() {
	e.mu.Lock()
	cleanups := e.cleanups
	e.cleanups = nil
	e.mu.Unlock()
	for _, f := range cleanups {
		f()
	}
}

// failed is deferred by Start: when Start returns an error, the pid file it
// wrote is removed (and what atExit registered runs).
func (e *startEnv) failed(err *error) {
	if *err != nil {
		e.runAtExit()
		e.releasePid()
	}
}

// stopSignals are the signals that stop enma.
func stopSignals() []os.Signal {
	if runtime.GOOS == "windows" {
		return []os.Signal{os.Interrupt, syscall.SIGTERM} // os.Kill cannot be caught
	}
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}

// handleSignals stops the runner on the first stop signal: shutdown, then exit
// 0 (1 when the shutdown is incomplete). A second signal during the shutdown
// kills everything at once (exit 1). The pid file is removed before exiting.
func (e *startEnv) handleSignals(stopping string, shutdown func() error, forceKill func()) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, stopSignals()...)
	go func() {
		<-signalChan
		log.Println(stopping + " (send the signal again to force)")
		done := make(chan error, 1)
		go func() { done <- shutdown() }()
		code := 0
		select {
		case err := <-done:
			if err != nil {
				log.Printf("❌ Shutdown incomplete: %v", err)
				code = 1
			}
		case <-signalChan:
			log.Println("⚡ Forced shutdown")
			forceKill()
			code = 1
		}
		e.runAtExit()
		e.releasePid()
		os.Exit(code)
	}()
}
