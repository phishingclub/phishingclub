package utils

import "time"

func RFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func NowRFC3339UTC() string {
	return RFC3339UTC(time.Now())
}
