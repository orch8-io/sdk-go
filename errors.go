package orch8

import "fmt"

// Orch8Error represents an error response from the Orch8 API.
type Orch8Error struct {
	Status int
	Body   string
	Path   string
}

func (e *Orch8Error) Error() string {
	return fmt.Sprintf("orch8 API error %d on %s: %s", e.Status, e.Path, e.Body)
}
