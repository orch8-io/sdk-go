package orch8

import (
	"fmt"
	"net/http"
	"strings"
)

// Orch8Error represents an error response from the Orch8 API.
type Orch8Error struct {
	Status int
	Body   string
	Path   string
}

func (e *Orch8Error) Error() string {
	body := e.Body
	if len(body) > 500 {
		body = body[:500] + "..."
	}
	return fmt.Sprintf("orch8 API error %d on %s: %s", e.Status, e.Path, body)
}

// IsNotFound reports whether the error is an HTTP 404.
func (e *Orch8Error) IsNotFound() bool {
	return e.Status == http.StatusNotFound
}

// IsRateLimited reports whether the error is an HTTP 429.
func (e *Orch8Error) IsRateLimited() bool {
	return e.Status == http.StatusTooManyRequests
}

// IsServerError reports whether the error is an HTTP 5xx.
func (e *Orch8Error) IsServerError() bool {
	return e.Status >= 500
}

// IsJSON returns true when the response body looks like JSON rather than HTML.
func (e *Orch8Error) IsJSON() bool {
	return strings.HasPrefix(strings.TrimSpace(e.Body), "{")
}
