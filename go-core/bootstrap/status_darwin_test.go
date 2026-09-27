package bootstrap

import (
	"strings"
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

func TestLaunchdEvidence(t *testing.T) {
	for _, test := range []struct {
		input string
		want  wire.BootstrapState
	}{
		{"service = {\n\tstate = running\n}", wire.BootstrapStateRunning},
		{"service = {\n\tstate = spawn scheduled\n}", wire.BootstrapStateStarting},
		{"service = {\n\tstate = waiting\n}", wire.BootstrapStateInstalled},
		{"service = {\n\t\tstate = running\n}", wire.BootstrapStateUnknown},
		{"garbled", wire.BootstrapStateUnknown},
	} {
		if got := observeLaunchd(test.input); got.State != test.want {
			t.Fatal(got, test.want)
		}
	}
}

func validRuntimeAgent(executable string) string {
	var services strings.Builder
	for service := range installedXPCServices {
		services.WriteString("<key>" + installedXPCPrefix + service + "</key><true/>")
	}
	return `<plist><dict><key>Label</key><string>` + runtimeAgent + `</string>` +
		`<key>ProgramArguments</key><array><string>` + executable + `</string><string>serve</string><string>runtime</string><string>--xpc</string></array>` +
		`<key>MachServices</key><dict>` + services.String() + `</dict>` +
		`<key>KeepAlive</key><true/></dict></plist>`
}

func TestRuntimeAgentRequiresExactXPCRegistration(t *testing.T) {
	const executable = "/Users/test/.local/bin/openabstractions"
	valid := validRuntimeAgent(executable)
	if err := ValidateRuntimeAgent([]byte(valid), executable); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"missing xpc flag": strings.Replace(valid, "<string>--xpc</string>", "", 1),
		"disabled service": strings.Replace(valid, "<true/>", "<false/>", 1),
		"missing service":  strings.Replace(valid, "<key>"+installedXPCPrefix+"runtime-v1</key><true/>", "", 1),
		"extra service":    strings.Replace(valid, "</dict><key>KeepAlive", "<key>com.openabstractions.extra-v1</key><true/></dict><key>KeepAlive", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateRuntimeAgent([]byte(invalid), executable); err == nil {
				t.Fatal("invalid registration accepted")
			}
		})
	}
}
