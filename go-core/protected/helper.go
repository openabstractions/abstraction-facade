package protected

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/go/trustedpath"
	"github.com/openabstractions/abstraction-identity/listen"
)

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// HelperFile is the helper's file name (FAC-H1).
var HelperFile = executable("openabstractions-system")

// runtimeFiles are the runtime images the helper may talk to, the windowless
// link the service template starts first.
var runtimeFiles = []string{executable("openabstractionsw"), executable("openabstractions")}

// TrustedHelper returns the helper beside image, the running runtime or
// Panel, when identity ID-D2's executable-folder mode accepts it with the
// machine's owners (no other account can add a file beside it): only an
// administrator can have written it. Any other layout is ErrUnavailable.
func TrustedHelper(image string) (string, error) {
	if !filepath.IsAbs(image) {
		return "", fmt.Errorf("%w: %q is not an absolute image path", ErrUnavailable, image)
	}
	helper := filepath.Join(filepath.Dir(image), HelperFile)
	if err := trustedpath.CheckExecutable(helper, trustedpath.MachineOwners()); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return helper, nil
}

// ServerExpectations is the helper's check of the runtime it talks to
// (identity ID-H2): each installed runtime image beside helper that ID-D2
// accepts, run by self's account.
func ServerExpectations(helper string, self identity.User) ([]listen.ServerExpectation, error) {
	if self.Kind != "windows" || self.SID == "" || !filepath.IsAbs(helper) {
		return nil, fmt.Errorf("%w: no Windows principal or absolute helper path", ErrUnavailable)
	}
	principal := identity.User{Kind: "windows", SID: self.SID}
	var out []listen.ServerExpectation
	for _, name := range runtimeFiles {
		program := filepath.Join(filepath.Dir(helper), name)
		if trustedpath.CheckFile(program, trustedpath.MachineOwners()) == nil {
			out = append(out, listen.ServerExpectation{Principal: principal, Program: program})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no installed runtime beside the helper", ErrUnavailable)
	}
	return out, nil
}

// Act performs one operation's frozen request and returns its result, or a
// failure (FailureDenied, FailureFailed) and no result.
type Act func(ctx context.Context, request []byte) (result []byte, failure string)

// RunHelper is the helper's exchange with the runtime (FAC-H1): one claim,
// the act, one completion. Each server expectation is tried in order until
// one is the runtime listening at endpoint; nothing is sent to a server that
// fails its expectation. ctx carries the helper's deadline.
func RunHelper(ctx context.Context, endpoint string, servers []listen.ServerExpectation, operation, ticket string, act Act) error {
	if !ValidOperation(operation) || !ValidTicket(ticket) || act == nil {
		return fmt.Errorf("%w: operation, ticket or act", ErrRefused)
	}
	claim, err := json.Marshal(message{Version: version, Kind: kindClaim, Operation: operation, Ticket: ticket})
	if err != nil {
		return err
	}
	var client listen.FrameClient
	var answer []byte
	err = errors.New("protected: no server expectation")
	for i := range servers {
		client = listen.FrameClient{Endpoint: endpoint, Server: &servers[i], MaxFrame: MaxFrame}
		answer, err = client.ExchangeFrameContext(ctx, claim)
		if !errors.Is(err, listen.ErrServerUntrusted) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("protected: claim: %w", err)
	}
	var r reply
	if err := decodeStrict(answer, &r); err != nil || r.Version != version {
		return fmt.Errorf("protected: malformed claim reply")
	}
	if r.Refused != "" {
		return fmt.Errorf("%w: %s", ErrRefused, r.Refused)
	}
	if len(r.Request) == 0 || len(r.Request) > maxRequest {
		return fmt.Errorf("%w: no request", ErrRefused)
	}
	result, failure := act(ctx, r.Request)
	switch {
	case failure == "" && len(result) > 0 && len(result) <= maxResult:
	case failure == FailureDenied || failure == FailureFailed:
		result = nil
	default:
		result, failure = nil, FailureFailed
	}
	done, err := json.Marshal(message{Version: version, Kind: kindComplete, Operation: operation, Ticket: ticket, Result: result, Failure: failure})
	if err != nil {
		return err
	}
	if answer, err = client.ExchangeFrameContext(context.WithoutCancel(ctx), done); err != nil {
		return fmt.Errorf("protected: complete: %w", err)
	}
	r = reply{}
	if err := decodeStrict(answer, &r); err != nil || r.Version != version {
		return fmt.Errorf("protected: malformed completion reply")
	}
	if r.Refused != "" {
		return fmt.Errorf("%w: %s", ErrRefused, r.Refused)
	}
	if failure != "" {
		return fmt.Errorf("protected: the act reported %s", failure)
	}
	return nil
}

// OwnImage is this process's own image path.
func OwnImage() (string, error) {
	image, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(image), nil
}
