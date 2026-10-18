package facade

import (
	"errors"
	"testing"
)

type versionedApplications struct {
	Applications
	observed bool
}

func (h *versionedApplications) Observe(string, int64) (ApplicationPage, error) {
	h.observed = true
	return ApplicationPage{Outcome: ApplicationOutcomePage, Applications: []ApplicationEntry{{
		Scope: ScopeLocal, Descriptor: ApplicationDescriptor{Name: "self", Title: "Self"},
		Instances: []ApplicationInstance{}, SelfRegistered: true,
	}}}, nil
}

func TestApplicationsVersionSeparatesPublishedClosedRecord(t *testing.T) {
	handler := &versionedApplications{}
	dispatcher := &ApplicationsDispatcher{Handler: handler}
	contract, _, _ := dispatcher.DescribeService()
	if contract != "abstraction.facade/applications@2" || dispatcher.ServiceContract() != contract {
		t.Fatalf("changed application record advertised as %q", contract)
	}
	// A published @1 client must receive a service refusal before its closed
	// three-field ApplicationEntry decoder can be given the new field.
	legacy := oaServiceFrame{Version: 1, Service: "abstraction.facade/applications@1", Method: "Observe", Arguments: Raw(`{"cursor":"","wait_ms":0}`)}
	frame, err := dispatcher.ExchangeFrame(encOAServiceFrame(nil, &legacy, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = serviceResponse(frame, legacy.Service, legacy.Method)
	var refusal *ServiceError
	if !errors.As(err, &refusal) || refusal.Code != ServiceErrorCodeUnknownService || handler.observed {
		t.Fatalf("legacy reader admission: observed=%v error=%v", handler.observed, err)
	}
	page, err := NewApplicationsClient(dispatcher).Observe("", 0)
	if err != nil || !handler.observed || len(page.Applications) != 1 || !page.Applications[0].SelfRegistered {
		t.Fatalf("versioned record round trip: %+v, %v", page, err)
	}
}
