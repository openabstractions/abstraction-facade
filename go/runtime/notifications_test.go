package runtime

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	wire "github.com/openabstractions/abstraction-asks/go/abstraction/asks/api"
	"github.com/openabstractions/abstraction-identity/listen"
)

// ASK-N1 operation_end: the runtime posts a held notification once its
// operation is terminal, leaves it held while the operation runs or is held
// by no provider, and reports a provider it could not read.
func TestPostEndedPostsOnlyEndedOperations(t *testing.T) {
	book, err := asks.LoadApplicationBook(filepath.Join(t.TempDir(), "application.json"))
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(t.TempDir(), "poster.exe")
	now := time.Now()
	hold := func(key, operation string) string {
		q := wire.NotificationRequest{RequestRef: wire.RequestRef{Key: key, ReplayUntilMs: now.Add(time.Hour).UnixMilli()}, Title: "Done",
			ExpiresMs: now.Add(30 * time.Minute).UnixMilli(), OperationID: operation, Delivery: wire.NotificationDeliveryOperationEnd}
		r, _, err := book.PostNotificationFrom("scope", program, q, listen.Seen{}, asks.Publisher{}, nil)
		if err != nil || r.Outcome != wire.NotificationOutcomeScheduled {
			t.Fatal(r, err)
		}
		return r.ID
	}
	ended, running, foreign, broken := hold("a", "op-ended"), hold("b", "op-running"), hold("c", "op-foreign"), hold("d", "op-broken")
	var reported []error
	postEnded(book, func(id string) (bool, bool, error) {
		switch id {
		case "op-ended":
			return true, true, nil
		case "op-running":
			return false, true, nil
		case "op-broken":
			return false, true, errors.New("unreadable record")
		}
		return false, false, nil
	}, func(err error) { reported = append(reported, err) })
	state := func(id string) wire.NotificationState {
		r, found, err := book.NotificationRecord(id)
		if err != nil || !found {
			t.Fatal(id, found, err)
		}
		return r.State
	}
	if state(ended) != wire.NotificationStatePosted {
		t.Fatal("an ended operation's notification is posted")
	}
	for _, id := range []string{running, foreign, broken} {
		if state(id) != wire.NotificationStateScheduled {
			t.Fatal("a notification whose operation has not ended stays held", id)
		}
	}
	if len(reported) != 1 {
		t.Fatal("an unreadable operation is reported once", reported)
	}
}
