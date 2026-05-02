package remotebrowser

import (
	"context"
	"encoding/base64"
	"time"
)

// RunEvent is an event emitted during script execution.
type RunEvent struct {
	Type    string `json:"type"`              // "event", "log", "error", "done", "capture", "screenshot", "info", "submit"
	Key     string `json:"key,omitempty"`     // for type=event/screenshot (label)
	Value   any    `json:"value,omitempty"`   // for type=event/capture/screenshot/submit (base64 data URI or arbitrary data)
	URL     string `json:"url,omitempty"`     // for type=screenshot (page URL at capture time)
	Message string `json:"message,omitempty"` // for type=log/error/info
	Data    any    `json:"data,omitempty"`    // for type=log: optional second arg from log(msg, data)
	Time    string `json:"time"`
}

// channelEmitter sends events to a buffered channel. All methods are safe to
// call from multiple goroutines; channel sends are already goroutine-safe.
type channelEmitter struct {
	events chan RunEvent
}

func newChannelEmitter(events chan RunEvent) *channelEmitter {
	return &channelEmitter{events: events}
}

func (e *channelEmitter) emit(key string, value any) {
	e.send(RunEvent{
		Type:  "event",
		Key:   key,
		Value: value,
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) log(msg string, data ...any) {
	evt := RunEvent{
		Type:    "log",
		Message: msg,
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if len(data) > 0 {
		evt.Data = data[0]
	}
	e.send(evt)
}

func (e *channelEmitter) errorf(msg string) {
	e.send(RunEvent{
		Type:    "error",
		Message: msg,
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) screenshot(label string, buf []byte, pageURL string) {
	e.send(RunEvent{
		Type:  "screenshot",
		Key:   label,
		Value: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf),
		URL:   pageURL,
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) capture(data interface{}) {
	e.send(RunEvent{
		Type:  "capture",
		Value: data,
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) info(msg string) {
	e.send(RunEvent{
		Type:    "info",
		Message: msg,
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) submitData(data interface{}) {
	e.send(RunEvent{
		Type:  "submit",
		Value: data,
		Time:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *channelEmitter) done() {
	e.send(RunEvent{
		Type: "done",
		Time: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// send delivers evt to the channel, dropping silently if the buffer is full.
func (e *channelEmitter) send(evt RunEvent) {
	select {
	case e.events <- evt:
	default:
	}
}

// sendMust delivers evt to the channel, blocking until space is available or
// ctx is cancelled. Use only for events where a silent drop would corrupt
// session state (e.g. keep_alive).
func (e *channelEmitter) sendMust(ctx context.Context, evt RunEvent) {
	select {
	case e.events <- evt:
	case <-ctx.Done():
	}
}
