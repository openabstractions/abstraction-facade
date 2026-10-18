package runtime

import (
	"context"
	"errors"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	asksservice "github.com/openabstractions/abstraction-asks/go/application"
	identity "github.com/openabstractions/abstraction-identity"
)

// operationOwner confirms that the job host accepted operationID in the
// scope it binds for this caller (ASK-N1). A program the job policy refuses
// owns no operation.
func (h *jobHost) operationOwner(_ context.Context, peer *identity.Peer, operationID string) error {
	scope, err := h.authorize(peer)
	if err != nil {
		return asksservice.ErrOperationNotOwned
	}
	owned, err := h.provider.OperationOwnedBy(operationID, scope)
	if err != nil {
		return err
	}
	if !owned {
		return asksservice.ErrOperationNotOwned
	}
	return nil
}

// endedOperationsInterval is how often the runtime looks for scheduled
// notifications whose operation ended.
var endedOperationsInterval = 3 * time.Second

// postEndedOperations posts each operation_end notification once the job host
// reports its operation terminal (ASK-N1). A notification whose operation this
// host does not hold stays scheduled until it expires.
func (h *Host) postEndedOperations(ctx context.Context) {
	ticker := time.NewTicker(endedOperationsInterval)
	defer ticker.Stop()
	for {
		h.postEndedOnce()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Host) postEndedOnce() {
	postEnded(h.questionBook, h.jobs.provider.OperationEnded, h.report)
}

// postEnded posts each scheduled notification whose operation ended.
func postEnded(book *asks.Book, ended func(operationID string) (bool, bool, error), report func(error)) {
	scheduled, err := book.ScheduledNotifications()
	if err != nil {
		report(err)
		return
	}
	for _, n := range scheduled {
		done, found, err := ended(n.OperationID)
		if err != nil {
			report(err)
			continue
		}
		if !found || !done {
			continue
		}
		if _, err := book.PostScheduledNotification(n.ID); err != nil {
			report(err)
		}
	}
}

func (h *Host) report(err error) {
	if err != nil && h.onError != nil && !errors.Is(err, context.Canceled) {
		h.onError(err)
	}
}
