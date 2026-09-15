package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerProgramScopeIsStableAndRequiresProvenIdentity(t *testing.T) {
	program, err := filepath.Abs(filepath.Join("bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := OwnerProgramScope("windows", "S-1-5-21-1", program)
	if err != nil || !strings.HasPrefix(scope, "owner-program@1:") {
		t.Fatal(scope, err)
	}
	again, _ := OwnerProgramScope("windows", "S-1-5-21-1", filepath.Join(program, "..", "tool"))
	moved, _ := OwnerProgramScope("windows", "S-1-5-21-1", program+"2")
	otherUser, _ := OwnerProgramScope("windows", "S-1-5-21-2", program)
	if again != scope || moved == scope || otherUser == scope {
		t.Fatal("scope must follow cleaned account and program identity")
	}
	for _, c := range [][3]string{{"", "S-1", program}, {"windows", "", program}, {"other", "1", program}, {"posix", "1000", "relative/tool"}} {
		if _, err := OwnerProgramScope(c[0], c[1], c[2]); err == nil {
			t.Fatal("accepted unproven identity", c)
		}
	}
}
