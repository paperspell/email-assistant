package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleInterrupt_RestoresTerminalThenExits(t *testing.T) {
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	var steps []string
	code := -1

	handleInterrupt(sig, make(chan struct{}),
		func() { steps = append(steps, "restore") },
		func(c int) { steps = append(steps, "exit"); code = c })

	// Терминал возвращается до выхода: иначе после Ctrl+C в вопросе о пароле
	// эхо в терминале осталось бы выключенным.
	assert.Equal(t, []string{"restore", "exit"}, steps)
	assert.Equal(t, 130, code)
}

func TestHandleInterrupt_ReleasedDoesNothing(t *testing.T) {
	done := make(chan struct{})
	close(done)
	called := false

	handleInterrupt(make(chan os.Signal), done,
		func() { called = true },
		func(int) { called = true })

	assert.False(t, called, "демон забрал SIGINT себе — выходить нельзя")
}

func TestExitOnInterrupt_ReleaseTwiceIsSafe(t *testing.T) {
	release := exitOnInterrupt()

	release()
	assert.NotPanics(t, release)
}
