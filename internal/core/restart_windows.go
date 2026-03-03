package main

import (
	"fmt"

	lm "github.com/maelmoreau21/JellyGate/logmessages"
)

func (app *appContext) HardRestart() error {
	return fmt.Errorf(lm.FailedHardRestartWindows)
}
