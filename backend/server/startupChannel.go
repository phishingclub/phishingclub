package server

// StartupMessage is the status of the server startup
type StartupMessage struct {
	Success bool
	Error   error
}

// NewStartupMessage creates a new StartupMessage
func NewStartupMessage(
	success bool,
	err error,
) StartupMessage {
	return StartupMessage{
		Success: success,
		Error:   err,
	}
}

func NewStartupMessageChannel() chan StartupMessage {
	return make(chan StartupMessage, 1)
}
