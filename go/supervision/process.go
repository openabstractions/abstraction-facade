// Package supervision starts provider processes with an owned process tree.
package supervision

import (
	"errors"
	"os"
	"time"
)

var ErrUnsupported = errors.New("supervision: process-tree containment unavailable on this platform")

// ErrInherit is Start's refusal of an Options.Inherit list: a handle that is
// zero, closed, not inheritable or named twice (FAC-P1).
var ErrInherit = errors.New("supervision: inherited handle refused")

// Options describes a foreign child. Callers retain ownership of standard files.
// ForceTimeout bounds descendant confirmation after Stop or natural parent exit.
//
// Inherit names further handles the child receives at the same values,
// beside its standard handles (FAC-P1). Each MUST be an open, inheritable
// handle of this process, named once; Start refuses any other before it
// creates anything. The caller keeps ownership and may close them once Start
// returns, since the child holds its own copies. The child inherits no other
// handle of this process.
type Options struct {
	Executable string
	// Args uses Windows CommandLineToArgvW quoting. Programs with another
	// parser, such as cmd.exe and batch files, require WindowsCmdLine.
	Args []string
	// WindowsCmdLine is the full Windows command line, including argv[0],
	// passed unchanged to CreateProcess. It requires empty Args. Executable
	// still selects the application; the command line cannot select another
	// executable. Start is unsupported outside Windows.
	WindowsCmdLine        string
	Env                   []string
	Dir                   string
	Stdin, Stdout, Stderr *os.File
	Inherit               []uintptr
	ForceTimeout          time.Duration
	// ProcessMemoryLimit, when not zero, is the committed memory each
	// process of the tree may hold, in bytes; Windows sets it on the job
	// (JOB_OBJECT_LIMIT_PROCESS_MEMORY) and a process asking for more is
	// refused the allocation.
	ProcessMemoryLimit uint64
	// Container, when not empty, is the string form of one AppContainer's
	// SID (S-1-15-2- and seven numbers): the child starts inside that
	// AppContainer with ContainerCapabilitySID as its one capability, in the
	// same CreateProcess that puts it in the job (FAC-P3). Its profile must exist, and Env must name
	// LOCALAPPDATA, which Windows requires of a contained start. Start
	// refuses any other SID with ErrContainer before it creates anything,
	// and never starts the child uncontained instead.
	Container string
	// InheritConsole keeps this process's console for a user-owned interactive
	// child. The default creates a hidden child, as provider launches require.
	// It creates no new console and changes no process-tree ownership.
	InheritConsole bool
}

// ContainerCapabilityName names the one capability every contained start
// carries (FAC-P3). Windows derives a capability SID from any name
// (DeriveCapabilitySidsFromName); this one is OA's own and is no network,
// device or broker capability. The container reaches through it only what an
// access list names it for.
//
// TEMPORARY EXCEPTION (TODO.md "Temporary exception: sandbox root
// attributes"): it exists so that one machine-wide entry on the system
// drive's root and one on the profiles folder, written by the per-machine
// installer, let every OA container read those two folders' attributes. An
// application whose start resolves its folder by reading every ancestor's
// attributes needs that. Remove it with those entries once that application's
// native path resolution is released and pinned.
const ContainerCapabilityName = "openabstractions.sandbox"

// ContainerCapabilitySID is the SID Windows derives from
// ContainerCapabilityName (TestContainerCapabilitySID).
const ContainerCapabilitySID = "S-1-15-3-1024-2690631898-1187214994-1895821874-1646876578-2465084366-4249851427-1120737608-701649861"

// ErrContainer is Start's refusal of an Options.Container that is not one
// AppContainer's SID, or of a contained start whose environment does not
// name LOCALAPPDATA (FAC-P3).
var ErrContainer = errors.New("supervision: contained start refused")
