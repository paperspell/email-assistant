package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"

	"golang.org/x/term"
)

// exitInterrupted is what shells report for a command ended by Ctrl+C:
// 128 + SIGINT.
const exitInterrupted = 130

// exitOnInterrupt makes Ctrl+C end an interactive command at once. Before it,
// SIGINT only cancelled the command's context, which the prompts never look
// at: a wizard carried on with default answers and failed at its final save
// with "context canceled". The terminal is put back first — a password
// prompt turns echo off — and the process exits with exitInterrupted.
//
// The daemon calls release to take SIGINT back for its graceful shutdown.
func exitOnInterrupt() (release func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	done := make(chan struct{})
	go handleInterrupt(sig, done, terminalRestorer(), os.Exit)

	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sig)
			close(done)
		})
	}
}

// handleInterrupt waits for the first interrupt and ends the process, unless
// done is closed first.
func handleInterrupt(sig <-chan os.Signal, done <-chan struct{}, restore func(), exit func(int)) {
	select {
	case <-sig:
		restore()
		fmt.Fprintln(os.Stderr, "\nInterrupted.")
		exit(exitInterrupted)
	case <-done:
	}
}

// terminalRestorer snapshots the terminal the prompts read from, so it can be
// put back if the process is interrupted inside a password prompt. That is
// stdin when it is a terminal, otherwise /dev/tty, which promptPassword falls
// back to. Without a terminal there is nothing to restore.
func terminalRestorer() func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			return func() {}
		}
		// Left open for the life of the process: the descriptor is what
		// restoring goes through.
		fd = int(tty.Fd())
	}
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() {
		if err := term.Restore(fd, state); err != nil {
			fmt.Fprintln(os.Stderr, "Could not restore the terminal; run `reset` if typing is not echoed:", err)
		}
	}
}
