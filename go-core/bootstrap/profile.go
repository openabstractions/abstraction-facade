package bootstrap

import "sync"

// ProfileView is how this process sees the account's profile folders. A process
// descended from an MSIX packaged app with file virtualization on sees a
// per-package copy of new files under AppData: a runtime started there, or a
// state file written there, is private to that package
// (research/packaged-activation/DECISION.md, section 3).
type ProfileView struct {
	// Virtualized reports that new files under AppData land in a package's
	// private copy.
	Virtualized bool
	// Family is that package's family name when Virtualized.
	Family string
}

// String is "real" or "virtualized(<family>)", as status reports it.
func (v ProfileView) String() string {
	if v.Virtualized {
		return "virtualized(" + v.Family + ")"
	}
	return "real"
}

var profileView struct {
	once sync.Once
	view ProfileView
	err  error
}

// CurrentProfileView probes this process's profile view once and returns the
// same answer afterwards. On Windows the probe creates and deletes one empty
// file under %LOCALAPPDATA%; elsewhere the view is always real. An error means
// the view is unknown, and a caller about to write state or activate a runtime
// treats it as a refusal.
func CurrentProfileView() (ProfileView, error) {
	profileView.once.Do(func() { profileView.view, profileView.err = probeProfileView() })
	return profileView.view, profileView.err
}
