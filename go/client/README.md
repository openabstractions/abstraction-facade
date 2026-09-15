# Go facade client

Package `client` binds an application to capability services through the
installed runtime. `Discover` authenticates the runtime from installation
evidence; `NewVerified` takes an explicit endpoint and server expectation.
`ResolveJobs` and `ResolveJobOperations` return a `JobsClient` for recoverable
job acceptance, observation and result reads.

## Who owns a job

The runtime files every accepted submission under a caller scope built from two
facts it observes on the connection:

- the authenticated account: the Windows SID or the POSIX UID;
- the absolute path of the calling program's executable, as the operating system
  reports it for the running process.

Request fields, process IDs and code signatures play no part. Identity keys,
receipts, observation and result bytes are visible only inside the scope that
submitted them. `runtime.OwnerProgramScope` defines the scope value.

| Event | Scope | Earlier work |
| --- | --- | --- |
| The program restarts, or the machine reboots | Same | A new process reconciles, observes and reads it |
| The runtime is upgraded or reinstalled | Same | Accepted work continues under the same receipts |
| The program is reinstalled at the same path | Same | A process started after the reinstall reaches it |
| Another executable in the same account calls | Different | Unreachable from that executable |
| The program is moved, or reinstalled at a different path | Different | Unreachable from the new path; reachable from the old path |
| Another account calls | Different | Unreachable |

Reconciling an identity the caller's scope has never accepted returns
`definitely_not_accepted` and seals that identity in the caller's scope. A moved
program that reconciles its old identities seals them at the new path. The
receipts at the old path are unchanged, and the work they name keeps running.

On Linux the path comes from `/proc/<pid>/exe` with symlinks resolved: a launcher
symlink into a versioned directory scopes the versioned target. A process whose
executable was replaced or deleted while it ran cannot prove its path and is
refused until it restarts. An interpreted application is scoped by its
interpreter executable, and every application of one account on that interpreter
shares a scope.

## Keeping continuity across reinstall

1. Install the executable at one absolute path per account and reinstall to that
   path. Keep version numbers out of it, including out of symlink targets.
2. Before `Submit`, persist the request identity (key and history epoch), the
   complete submission and `JobsClient.Binding()`. Store them outside the
   installation directory, where reinstall leaves them.
3. After a restart or reinstall, restore the binding and continue with the saved
   identity. `RestoreJobs` authenticates the saved endpoint through the machine's
   trust, refuses a different logical owner and never resubmits.

```go
jobs, err := client.Discover().RestoreJobs(ctx, saved.Binding)
if err != nil {
	return err
}
result, err := jobs.Reconcile(ctx, saved.Identity)
// accepted: ObserveWork and CopyResult with saved.Identity
```

`saved` is the application's own persisted record. A reinstall that must change
the executable path gives up the unfinished work of the old path: read results
before moving, or run the old executable once more to collect them. The runtime
has no transfer between program scopes. `acceptanceprovider.MigrateLegacy` is an
operator tool for unowned legacy job directories and assigns no work between
program scopes.
