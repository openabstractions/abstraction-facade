// Package legacy explicitly selects the deprecated embedded adoption facade.
// Discover may open local stores, run downloads in-process and select file logs.
// New applications use the parent facade package and resolved service clients.
//
// Deprecated: migrate to abstraction-facade/go Resolve* methods. This package
// preserves the old provider selection semantics for existing adopters.
package legacy

import (
	"strings"

	download "github.com/openabstractions/abstraction-download/go"
	job "github.com/openabstractions/abstraction-job/go"
	logging "github.com/openabstractions/abstraction-logging/go"
	storage "github.com/openabstractions/abstraction-storage/go"
)

// Machine is what this machine can do, discovered once.
//
// Accessors, not fields, and each returns an interface: what is behind them is a
// binding chosen by the machine, and an application that could reach past them
// would be back to holding a *FileStore and a directory path.
type Machine interface {
	// Jobs is durable work of every kind on this machine.
	Jobs() job.Store

	// Download is bytes in motion.
	Download() download.Client

	// Storage is bytes at rest: what this machine already has, and where new
	// bytes should go. It is what makes "do not download that again" possible.
	// A store that is also Local or Writable says so through those
	// capabilities.
	Storage() storage.Store

	// Log is where this program's records go — a service if one is configured,
	// a file otherwise. The same argument as the download tiers: presence is
	// the configuration.
	Log(program string) logging.Sink

	// Bindings names what is actually underneath, for a diagnostic line. Never
	// a branch.
	Bindings() []string
}

// Discover reads what this machine has and returns a working abstraction.
//
// Nothing is configured per application. There is no path, no hostname and no
// flag in the caller — the same property SLF4J has, where a library that logs
// gets a logger and never learns which sink is attached.
func Discover() (Machine, error) {
	r, err := download.Discover()
	if err != nil {
		return nil, err
	}
	// Foreign stores first: bytes another tool already holds and verified beat
	// bytes we would have to fetch. Ours is appended by whoever configures one.
	stores := storage.New(storage.Discover()...)
	return &machine{runner: r, downloads: download.NewClient(r, download.WithStorage(stores)), stores: stores}, nil
}

func (m *machine) Storage() storage.Store { return m.stores }

type machine struct {
	runner    *download.Runner
	downloads download.Client
	stores    *storage.Stores
}

func (m *machine) Jobs() job.Store { return m.runner.Store }

func (m *machine) Download() download.Client { return m.downloads }

func (m *machine) Log(program string) logging.Sink { return logging.Auto(program) }

func (m *machine) Bindings() []string {
	out := []string{"jobs: file"}
	if _, ok := m.runner.Store.(job.Scratch); !ok {
		out[0] = "jobs: service"
	}
	out = append(out, "download: "+m.fetching())
	return append(out, "storage: "+strings.Join(m.stores.Names(), ", "))
}

// fetching separates the two ways bytes end up moving in this process, because
// the fixes are different and a status line that says only "here" hides which
// one applies: install the system downloader, or link the tier.
func (m *machine) fetching() string {
	where := m.downloads.Where()
	if where != "here" {
		return where
	}
	linked := download.RegisteredTiers()
	if len(linked) == 0 {
		return "here (no system downloader, no tier linked)"
	}
	return "here (no system downloader; linked and unavailable: " + strings.Join(linked, ", ") + ")"
}
