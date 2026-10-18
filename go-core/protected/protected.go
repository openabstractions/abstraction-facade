// Package protected carries a protected operation: one act the operating
// system allows only an administrator, performed for the person after their
// yes in OA's prompt and the platform's own administrator prompt
// (abstraction-facade CONTRACT.md FAC-H1..FAC-H4, VISION 2026-10-02).
//
// Three parties use it. The runtime's Broker binds each act to the system-access
// question it admitted and hands the frozen request to the helper once the
// person said once (FAC-H2). The operator presenter launches the installed
// helper through the platform's prompt (Launch, FAC-H3). The helper, elevated,
// claims the request, performs the act and returns the result (RunHelper,
// FAC-H1). The owning capability supplies the act and checks its result; this
// package knows no act's content.
package protected

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// EndpointService is the per-account endpoint the runtime listens on for
	// the helper (bootstrap.Endpoint).
	EndpointService = "system-access-v1"
	// OperationSecurityLogPage reads one bounded page of the Windows Security
	// log (abstraction-logging LOG-N1).
	OperationSecurityLogPage = "security-log-page"
	// ClaimWindow is how long after the person's once the helper may claim.
	ClaimWindow = 120 * time.Second
	// HoldWindow is how long a fetched result waits for its owner.
	HoldWindow = 120 * time.Second
	// HelperDeadline bounds the helper's whole run.
	HelperDeadline = 30 * time.Second
	// MaxTickets bounds the open tickets of one runtime.
	MaxTickets = 16
	// MaxFrame bounds every frame either end sends.
	MaxFrame = 1 << 20
	// maxRequest bounds a frozen request, so a claim reply fits a frame.
	maxRequest = 64 << 10
	// maxResult bounds a result, so a completion fits a frame once encoded.
	maxResult = 512 << 10
	// ticketAge bounds how long an asked ticket waits for its question: the
	// question's 15 pending minutes (ASK-A7) and both windows.
	ticketAge = 20 * time.Minute
)

// Failures a helper reports instead of a result.
const (
	// FailureDenied: the platform refused the act even elevated.
	FailureDenied = "denied"
	// FailureFailed: the act could not be performed.
	FailureFailed = "failed"
)

var (
	// ErrUnavailable: this installation, layout or platform has no trusted
	// helper (FAC-H4).
	ErrUnavailable = errors.New("protected: no trusted helper on this installation")
	// ErrCancelled: the person refused the platform's prompt.
	ErrCancelled = errors.New("protected: the administrator prompt was refused")
	// ErrRefused: the runtime or the helper's own checks refused the act.
	ErrRefused = errors.New("protected: refused")
)

var operations = map[string]bool{OperationSecurityLogPage: true}

// ValidOperation reports whether op is in the compiled catalogue.
func ValidOperation(op string) bool { return operations[op] }

// ValidTicket reports whether t is a ticket's spelling: 1..128 lowercase hex
// characters, the Book's question ID.
func ValidTicket(t string) bool {
	if len(t) == 0 || len(t) > 128 {
		return false
	}
	for _, c := range []byte(t) {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// HelperArguments is the helper's whole command line after its image name
// (FAC-H1). It refuses an operation outside the catalogue and a ticket that
// is not one.
func HelperArguments(operation, ticket string) ([]string, error) {
	if !ValidOperation(operation) || !ValidTicket(ticket) {
		return nil, fmt.Errorf("%w: operation %q or ticket outside the closed grammar", ErrRefused, operation)
	}
	return []string{operation, "--ticket", ticket}, nil
}

// ParseHelperArguments reads the helper's command line, refusing every other
// form.
func ParseHelperArguments(args []string) (operation, ticket string, err error) {
	if len(args) != 3 || args[1] != "--ticket" || !ValidOperation(args[0]) || !ValidTicket(args[2]) {
		return "", "", fmt.Errorf("%w: usage: <operation> --ticket <ticket>", ErrRefused)
	}
	return args[0], args[2], nil
}

const version = 1

const (
	kindClaim    = "claim"
	kindComplete = "complete"
)

// message is what the helper sends.
type message struct {
	Version   int    `json:"version"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	Ticket    string `json:"ticket"`
	Result    []byte `json:"result,omitempty"`
	Failure   string `json:"failure,omitempty"`
}

// reply is what the runtime answers.
type reply struct {
	Version int    `json:"version"`
	Refused string `json:"refused,omitempty"`
	Request []byte `json:"request,omitempty"`
}

func decodeStrict(frame []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(frame))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return errors.New("protected: trailing data")
	}
	return nil
}
