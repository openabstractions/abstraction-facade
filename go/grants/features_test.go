package grants

import (
	"errors"
	"reflect"
	"testing"
)

// The picture feature set is pinned: complete on its one host and the image
// output's write and read, nothing more.
func TestPictureFeatureRulesArePinned(t *testing.T) {
	rules, err := FeatureRules(Picture, For{Hosts: []string{"lemonade-image"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{"abstraction.inference/complete", "host:lemonade-image"},
		{"abstraction.storage/content.write", "output:image"},
		{"abstraction.storage/content.read", "output:image"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("picture rules %+v, want %+v", rules, want)
	}
	for _, wrong := range []For{
		{},
		{Hosts: []string{"a", "b"}},
		{Hosts: []string{"a"}, Outputs: []string{"image"}},
		{Hosts: []string{"a"}, Registries: []string{"hf"}},
		{Hosts: []string{"a"}, Credentials: []string{"key"}},
		{Hosts: []string{"host:a"}},
	} {
		if _, err := FeatureRules(Picture, wrong); !errors.Is(err, ErrBundle) {
			t.Fatalf("picture accepted %+v: %v", wrong, err)
		}
	}
	if _, err := FeatureRules("everything", For{Hosts: []string{"a"}}); !errors.Is(err, ErrBundle) {
		t.Fatalf("an unknown feature: %v", err)
	}
}

// The chat feature set is pinned: complete on its one host and the router's
// inventory read, nothing more; no route, no job, no output profile.
func TestChatFeatureRulesArePinned(t *testing.T) {
	rules, err := FeatureRules(Chat, For{Hosts: []string{"ollama"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{
		{"abstraction.inference/complete", "host:ollama"},
		{"abstraction.router/inventory.read", "abstraction.router/inventory"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("chat rules %+v, want %+v", rules, want)
	}
	for _, wrong := range []For{{}, {Hosts: []string{"a", "b"}}, {Hosts: []string{"a"}, Outputs: []string{"image"}},
		{Hosts: []string{"a"}, Credentials: []string{"key"}}, {Hosts: []string{"host:a"}}} {
		if _, err := FeatureRules(Chat, wrong); !errors.Is(err, ErrBundle) {
			t.Fatalf("chat accepted %+v: %v", wrong, err)
		}
	}
	if names := FeatureNames(); !reflect.DeepEqual(names, []string{Picture, Chat}) {
		t.Fatalf("feature names %q", names)
	}
}
