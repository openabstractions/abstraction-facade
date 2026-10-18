package grants

import (
	"fmt"
	"strings"

	inference "github.com/openabstractions/abstraction-inference/go"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

// Feature rule sets name the exact rules one application feature needs, and
// nothing a bundle adds for other uses of the same capability. A product that
// grants an app a feature at install time writes one of these; the bundles
// above stay what `rights grant --for` writes for a person's own choice.
// Each set is pinned by a test, so a change to it is a reviewed change.
const (
	// Picture is an app making pictures with one image host through
	// image@1 and reading back the pictures it made (inference INF-S7). It
	// takes no router read, no route and no job submission.
	Picture = "picture"
	// Chat is an app chatting with models on one host through chat@1 or
	// chat@2 (complete and its stream) and reading the router's inventory to
	// list them. It takes no route, no job submission and no output profile.
	Chat = "chat"
)

// FeatureNames lists the feature rule sets.
func FeatureNames() []string { return []string{Picture, Chat} }

// FeatureRules returns the exact rules of a feature, in the order they are
// written.
//
//   - picture: complete on the one host, then content.write and content.read
//     on output:image.
//   - chat: complete on the one host, then inventory.read on the router's
//     inventory.
func FeatureRules(feature string, f For) ([]Rule, error) {
	switch feature {
	case Picture, Chat:
		if len(f.Hosts) != 1 || len(f.Registries) != 0 || len(f.Credentials) != 0 || len(f.Outputs) != 0 {
			return nil, fmt.Errorf("%w: %s names exactly one host and nothing else", ErrBundle, feature)
		}
		host := f.Hosts[0]
		if host == "" || len(host) > 64 || strings.ContainsAny(host, " \t\r\n,:") {
			return nil, fmt.Errorf("%w: %q is not a host name", ErrBundle, host)
		}
		if feature == Chat {
			return []Rule{
				{inference.ActionComplete, inference.ResourceHost(host)},
				{routerservice.ActionInventory, routerservice.ResourceInventory},
			}, nil
		}
		return []Rule{
			{inference.ActionComplete, inference.ResourceHost(host)},
			{ContentWriteAction, "output:image"},
			{ContentReadAction, "output:image"},
		}, nil
	}
	return nil, fmt.Errorf("%w: no feature called %q; name %s", ErrBundle, feature, strings.Join(FeatureNames(), " or "))
}
