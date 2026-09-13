package bootstrap

import (
	"context"
	"encoding/xml"
	"errors"
	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const runtimeAgent = "com.openabstractions.jobd"

func hideStatusCommand(*exec.Cmd) {}
func observeInstalled(ctx context.Context) wire.BootstrapObservation {
	home, err := os.UserHomeDir()
	if err != nil {
		return statusEvidence("unknown", "user home unavailable")
	}
	raw, err := readAgent(filepath.Join(home, "Library", "LaunchAgents", runtimeAgent+".plist"))
	if err != nil {
		return statusEvidence("unknown", "current-user LaunchAgent registration unavailable")
	}
	if err = ValidateRuntimeAgent(raw, filepath.Join(home, ".local", "bin", "openabstractions")); err != nil {
		return statusEvidence("unknown", err.Error())
	}
	out, err := statusCommand(ctx, "/bin/launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+runtimeAgent)
	if err != nil {
		return statusEvidence("installed", "validated current-user LaunchAgent exists; supervision could not be observed")
	}
	return observeLaunchd(out)
}
func observeLaunchd(out string) wire.BootstrapObservation {
	// Only a single state line at the service dictionary level is accepted.
	state := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "\tstate = ") {
			continue
		}
		if state != "" {
			return statusEvidence("unknown", "ambiguous launchd state")
		}
		state = strings.TrimSpace(strings.TrimPrefix(line, "\tstate = "))
	}
	switch state {
	case "running":
		return statusEvidence("running", "current-user launchd job is running; capability readiness is separate")
	case "spawn scheduled", "spawning":
		return statusEvidence("starting", "current-user launchd job is starting")
	case "not running", "waiting":
		return statusEvidence("installed", "current-user launchd job is registered but not running")
	default:
		return statusEvidence("unknown", "unrecognized launchd state response")
	}
}

// ValidateRuntimeAgent recognizes the exact installed runtime command before
// activation or installation observation accepts the registration.
func ValidateRuntimeAgent(raw []byte, executable string) error {
	d := xml.NewDecoder(strings.NewReader(string(raw)))
	bad := func() error { return errors.New("start: invalid or ambiguous installed LaunchAgent dictionary") }
	next := func() (xml.Token, error) {
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch v := t.(type) {
			case xml.Comment, xml.ProcInst, xml.Directive:
				continue
			case xml.CharData:
				if strings.TrimSpace(string(v)) != "" {
					return nil, bad()
				}
				continue
			}
			return t, nil
		}
	}
	expect := func(name string, end bool) error {
		t, e := next()
		if e != nil {
			return e
		}
		if end {
			v, ok := t.(xml.EndElement)
			if !ok || v.Name != (xml.Name{Local: name}) {
				return bad()
			}
		} else {
			v, ok := t.(xml.StartElement)
			if !ok || v.Name != (xml.Name{Local: name}) {
				return bad()
			}
		}
		return nil
	}
	text := func(el xml.StartElement) (string, error) {
		var b strings.Builder
		for {
			t, e := d.Token()
			if e != nil {
				return "", e
			}
			switch v := t.(type) {
			case xml.CharData:
				b.Write(v)
			case xml.Comment:
			case xml.EndElement:
				if v.Name != el.Name {
					return "", bad()
				}
				return b.String(), nil
			default:
				return "", bad()
			}
		}
	}
	if err := expect("plist", false); err != nil {
		return err
	}
	if err := expect("dict", false); err != nil {
		return err
	}
	seen := map[string]bool{}
	label, program := "", ""
	var args []string
	for {
		t, e := next()
		if e != nil {
			return e
		}
		if end, ok := t.(xml.EndElement); ok {
			if end.Name != (xml.Name{Local: "dict"}) {
				return bad()
			}
			break
		}
		keyEl, ok := t.(xml.StartElement)
		if !ok || keyEl.Name != (xml.Name{Local: "key"}) {
			return bad()
		}
		key, e := text(keyEl)
		if e != nil {
			return e
		}
		if seen[key] {
			return bad()
		}
		seen[key] = true
		t, e = next()
		if e != nil {
			return e
		}
		value, ok := t.(xml.StartElement)
		if !ok {
			return bad()
		}
		switch key {
		case "Label", "Program":
			if value.Name != (xml.Name{Local: "string"}) {
				return bad()
			}
			v, e := text(value)
			if e != nil {
				return e
			}
			if key == "Label" {
				label = v
			} else {
				program = v
			}
		case "ProgramArguments":
			if value.Name != (xml.Name{Local: "array"}) {
				return bad()
			}
			for {
				t, e := next()
				if e != nil {
					return e
				}
				if end, ok := t.(xml.EndElement); ok {
					if end.Name != value.Name {
						return bad()
					}
					break
				}
				el, ok := t.(xml.StartElement)
				if !ok || el.Name != (xml.Name{Local: "string"}) {
					return bad()
				}
				v, e := text(el)
				if e != nil {
					return e
				}
				args = append(args, v)
			}
		default:
			if err := d.Skip(); err != nil {
				return err
			}
		}
	}
	if err := expect("plist", true); err != nil {
		return err
	}
	if _, err := next(); err != io.EOF {
		return bad()
	}
	if label != runtimeAgent || len(args) != 3 || args[0] != executable || args[1] != "serve" || args[2] != "runtime" || (seen["Program"] && program != executable) {
		return errors.New("start unsupported: installed LaunchAgent does not host the shared runtime; update the package")
	}
	return nil
}
