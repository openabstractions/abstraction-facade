module github.com/openabstractions/abstraction-facade/go

go 1.26.0

require (
	github.com/openabstractions/abstraction-facade/go-core v0.0.0 // development: publish core and pin its real version before release
	github.com/openabstractions/abstraction-asks/go v0.3.0
	github.com/openabstractions/abstraction-rights/go v0.2.0
	github.com/openabstractions/abstraction-config/go v0.3.1-0.20260912072725-eaa28e1dcad4
	github.com/openabstractions/abstraction-download/go v0.4.1
	github.com/openabstractions/abstraction-identity v0.2.1-0.20260911225957-3aa75da4cda3
	github.com/openabstractions/abstraction-job/go v0.4.3-0.20260912073823-0c5fe0c03af5
	github.com/openabstractions/abstraction-model/go v0.3.0
	github.com/openabstractions/abstraction-logging/go v0.3.1-0.20260912072712-ccf893212f23
	github.com/openabstractions/abstraction-router/go v0.0.0-20260911213816-49809b228933
	github.com/openabstractions/abstraction-storage/go v0.2.0
)

require golang.org/x/sys v0.47.0 // indirect

require (
	github.com/openabstractions/abstraction-cas/go v0.2.0 // indirect
	github.com/openabstractions/abstraction-watch/go v0.2.0 // indirect
)
