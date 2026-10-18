package grants

import rights "github.com/openabstractions/abstraction-rights/go"

// RuntimeAccountDefaults are the installed runtime's five account-wide
// defaults (rights RIGHTS-R22; VISION 2026-10-07): registering itself as an
// application, asking its own questions, announcing that it runs, posting
// notifications, and download jobs. Each names an action defined here.
func RuntimeAccountDefaults() []rights.AccountDefault {
	return []rights.AccountDefault{
		{Name: rights.DefaultRegister, Action: rights.ActionApplicationRegisterSelf, Resource: rights.ResourceAccount},
		{Name: rights.DefaultAsk, Action: rights.ActionQuestionAsk, Resource: rights.ResourceAccount},
		{Name: rights.DefaultAnnounce, Action: rights.ActionApplicationAnnounce, EveryResource: true},
		{Name: rights.DefaultNotify, Action: NotificationPostAction, Resource: NotificationPostResource},
		{Name: rights.DefaultDownload, Action: rights.ActionJobSubmit, Resource: rights.ResourceDownloadJobs},
	}
}
