"""Typed service facade with shared native bootstrap; no provider fallback."""
import dataclasses
import sys
import time
from abstraction.ipc import FrameTransport, Library, FrameError, TIMEOUT, CANCELLED, ServerExpectation
import abstraction.facade as wire
from abstraction.facade import Scope

RUNTIME_UNAVAILABLE = "runtime_unavailable"


def unsupported_platform():
    """Return "android" when this process runs on Android, otherwise None.

    "macos" (sys.platform "darwin") is a declared platform too, but returns
    None here: macOS proceeds to installed-runtime selection rather than this
    early refusal (RESOLUTION.md, "Unsupported platforms").
    """
    if sys.platform == "android" or hasattr(sys, "getandroidapilevel"):
        return "android"
    return None


class ResolutionError(RuntimeError):
    """A resolve call produced no usable service.

    status is the resolver's refusal word, invalid_resolution or
    unsupported_transport after validation, or runtime_unavailable when no
    runtime could be selected or reached. capability and contract name the
    request; looked_for names the installed runtime or the explicit endpoint.
    platform names a platform the runtime declares unsupported, and is None
    otherwise. A transport failure is chained as __cause__.
    """

    def __init__(self, status, capability=None, contract=None, looked_for=None, *, platform=None):
        message = "service resolution: " + status
        if contract is not None:
            message += ": %s (capability %s) at %s" % (contract, capability, looked_for)
        if platform is not None:
            message += ": no supported OpenAbstractions runtime exists for " + platform
        super().__init__(message)
        self.status, self.capability, self.contract, self.looked_for = status, capability, contract, looked_for
        self.platform = platform


class Binding(FrameTransport):
    """One resolved service: the transport a capability client calls through,
    and the reference naming the provider that answered (CONTRACT.md FAC-B4).

    A binding built from a caller-retained endpoint carries no reference.
    A new waiting policy or call scope keeps the selected reference.
    """

    def __init__(self, library, endpoint, *, reference=None, **options):
        super().__init__(library, endpoint, **options)
        self._reference = reference

    @property
    def reference(self):
        """The provider the runtime selected, with the capability, contract,
        guarantees, scope, transport and endpoint it answered with, or None.

        Read-only: each read returns an independent copy, and the reference
        grants nothing."""
        if self._reference is None:
            return None
        return dataclasses.replace(self._reference, guarantees=list(self._reference.guarantees))

    def with_waiting(self, *, deadline=None, cancellation=None):
        return Binding(self.library, self.endpoint.decode("utf-8"), reference=self._reference,
                       timeout=self.timeout, deadline=deadline, cancellation=cancellation,
                       max_frame=self.max_frame, server=self.server, sessions=self.sessions)

    def call_scope(self):
        deadline = self.deadline if self.deadline is not None else time.monotonic() + self.timeout
        return Binding(self.library, self.endpoint.decode("utf-8"), reference=self._reference,
                       timeout=self.timeout, deadline=deadline, cancellation=self.cancellation,
                       max_frame=self.max_frame, server=self.server, sessions=self.sessions)


def reference(binding):
    """The reference the runtime returned for one resolved binding, or None.

    binding is a Binding, or a capability client the facade built from one.
    The reference names the provider that answered and grants nothing
    (CONTRACT.md FAC-B4)."""
    held = binding
    for _ in range(4):
        if isinstance(held, Binding):
            return held.reference
        following = getattr(held, "_transport", None)
        if following is None:
            following = getattr(held, "_binding", None)
        if following is None:
            return None
        held = following
    return None


class Machine:
    def __init__(self, endpoint=None, library=None, *, timeout=5.0, deadline=None, cancellation=None,
                 server=None, provider_trust=None):
        """Default discovery verifies the selected installed runtime and providers.

        Explicit endpoints retain compatibility unless server is supplied.
        provider_trust may select independent configured trust for other hosts.
        On a platform the runtime declares unsupported, default discovery loads
        no native library and every resolve raises runtime_unavailable naming it.
        """
        self._platform = unsupported_platform() if endpoint is None and server is None else None
        if self._platform is not None and library is None and cancellation is None:
            if provider_trust is not None and not callable(provider_trust):
                raise ValueError("provider_trust must be callable")
            self._library = None
            self._options = dict(timeout=timeout, deadline=deadline, cancellation=cancellation)
            self._endpoint, self._server, self._provider_trust = endpoint, server, provider_trust
            return
        if library is None:
            library = cancellation._library if cancellation is not None else Library()
        if provider_trust is not None and not callable(provider_trust):
            raise ValueError("provider_trust must be callable")
        self._library = library
        self._options = dict(timeout=timeout, deadline=deadline, cancellation=cancellation)
        self._endpoint, self._server, self._provider_trust = endpoint, server, provider_trust
        # Validate options without selecting, connecting or consuming a call budget.
        FrameTransport(library, endpoint if endpoint is not None else "validation", server=server, **self._options)

    def resolve_storage(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Resolve one fixed content reader through the shared transport."""
        from abstraction.storage.content.client import Client
        return Client(self._bind("abstraction.storage", "abstraction.storage/content-reader@1", guarantees, scope))

    def resolve_storage_writer(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Resolve one content writer; every call remains subject to its write policy."""
        from abstraction.storage.content.client import Writer
        return Writer(self._bind("abstraction.storage", "abstraction.storage/content-writer@1", guarantees, scope))

    def resolve_log(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Return the generated SinkClient at one validated, fixed endpoint."""
        import abstraction.logging as logging
        return logging.SinkClient(self._bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope))

    def resolve_config(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Read existing provider settings with explicit per-call run overrides."""
        import abstraction.config as config
        return config.ConfigReaderClient(self._bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope))

    def resolve_config_editor(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Edit user settings with the service's compare-and-replace revision."""
        import abstraction.config as config
        return config.ConfigEditorClient(self._bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope))

    def resolve_asks(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Admit and observe questions in the caller's bound scope."""
        import abstraction.asks.api as asks
        return asks.QuestionApplicationClient(self._bind("abstraction.asks", "abstraction.asks/application@1", guarantees, scope))

    def resolve_asks_operator(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """List, answer and retire questions; every call remains subject to the host's operator policy."""
        import abstraction.asks.api as asks
        return asks.QuestionOperatorClient(self._bind("abstraction.asks", "abstraction.asks/operator@1", guarantees, scope))

    def resolve_credentials(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Register, rotate, revoke, list and audit credentials by name; no call returns secret bytes."""
        import abstraction.credentials.api as credentials
        return credentials.HolderClient(self._bind("abstraction.credentials", "abstraction.credentials/holder@1", guarantees, scope))

    def resolve_credentials_applier(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Apply a credential for one request; only a program the host designated as an enforcer is served."""
        import abstraction.credentials.api as credentials
        return credentials.ApplierClient(self._bind("abstraction.credentials", "abstraction.credentials/applier@1", guarantees, scope))

    def resolve_inference(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Complete and stream model calls the runtime performs; no key or endpoint reaches this program."""
        from abstraction.inference.client import Chat
        return Chat(self._bind("abstraction.inference", "abstraction.inference/chat@1", guarantees, scope))

    def resolve_inference_operator(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Hosts, gateway window keys and the inference audit; the runtime decides host.manage, key.issue or audit.read per call."""
        import abstraction.inference.api as inference
        return inference.OperatorClient(self._bind("abstraction.inference", "abstraction.inference/operator@1", guarantees, scope))

    def resolve_registry(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """The runtime's provider declarations, abstraction.facade/registry@1; the runtime decides provider.manage per call."""
        return wire.RegistryClient(self._bind("abstraction.facade", "abstraction.facade/registry@1", guarantees, scope))

    def resolve_applications(self, *, guarantees=(), scope: Scope = Scope.LOCAL):
        """Bind the local identity-scoped application directory and activation API."""
        if not isinstance(scope, Scope) or scope not in (Scope.ANY, Scope.LOCAL):
            raise ResolutionError("unmet_requirements", "abstraction.facade",
                                  "abstraction.facade/applications@1", scope)
        return wire.ApplicationsClient(self._bind("abstraction.facade",
            "abstraction.facade/applications@1", guarantees, Scope.LOCAL))

    def describe_endpoint(self, endpoint):
        """Call abstraction.facade/endpoint@1 Describe on one local endpoint: the services it hosts and each one's readiness.

        The description's program is the provider's own claim and grants nothing."""
        if not isinstance(endpoint, str) or not endpoint or "\0" in endpoint:
            raise ValueError("invalid endpoint")
        return wire.EndpointClient(FrameTransport(self._library, endpoint, **self._options)).describe()

    def resolve_model(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Look up portable download requests; forbidden and unavailable are typed lookup outcomes."""
        import abstraction.model.api as model
        return model.ModelResolverClient(self._bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope))

    def resolve_router(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Read host inventory and pick hosts; policy refusals raise ServiceError codes forbidden or policy_unavailable."""
        import abstraction.router as router
        return router.RouterClient(self._bind("abstraction.router", "abstraction.router/router@1", guarantees, scope))

    def resolve_jobs(self, *, guarantees=(), scope: Scope = Scope.ANY):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Jobs
        return Jobs(self._bind("abstraction.job", "abstraction.job/acceptance@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_job_operations(self, *, guarantees=(), scope: Scope = Scope.ANY):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Jobs
        return Jobs(self._bind("abstraction.job", "abstraction.job/operations@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_job_inventory(self, *, guarantees=(), scope: Scope = Scope.ANY):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Inventory
        return Inventory(self._bind("abstraction.job", "abstraction.job/inventory@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_log_reader(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Return bounded history at one fixed endpoint; gaps require explicit restart."""
        return LogHistory(self._bind("abstraction.logging", "abstraction.logging/reader@1", guarantees, scope))

    def resolve_rights(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Point-in-time decisions for the bound caller; decide_for needs trusted-enforcer authority at the service."""
        return Rights(self._bind("abstraction.rights", "abstraction.rights/authorization@1", guarantees, scope))

    def resolve_rights_operator(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Conditional policy administration; every call remains subject to the host's operator authorization."""
        return RightsOperator(self._bind("abstraction.rights", "abstraction.rights/operator@1", guarantees, scope))

    def resolve_config_observer(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Latest-snapshot long-poll configuration observation; each call's budget is extended by its wait_ms."""
        return ConfigObserver(self._bind("abstraction.config", "abstraction.config/observer@1", guarantees, scope))

    def resolve_log_observer(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Long-poll history observation; each call's budget is extended by its wait_ms."""
        return LogObserver(self._bind("abstraction.logging", "abstraction.logging/observer@1", guarantees, scope))

    def resolve_storage_changes(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Observe added and removed objects; every call remains subject to the observe and read policies."""
        from abstraction.storage.content.client import Changes
        return Changes(self._bind("abstraction.storage", "abstraction.storage/content-changes@1", guarantees, scope))

    def resolve_service(self, contract, *, guarantees=(), scope: Scope = Scope.ANY):
        """Bind one exact versioned contract identity and return the Binding.

        The capability is the contract's own prefix, as
        abstraction.facade/resolver@1 names it. The binding carries the
        reference naming the provider that answered; the capability accessors
        above bind the same way and construct their typed client."""
        capability, separator, profile = contract.partition("/") if isinstance(contract, str) else ("", "", "")
        if not capability or not separator or not profile:
            raise ValueError("invalid resolution requirements")
        return self._bind(capability, contract, guarantees, scope)

    def resolve_resource_table(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Read who holds a scarce resource on this machine.

        Every read remains subject to abstraction.resource/table.read on resource
        account, which narrows a refused caller to its own program's rows rather
        than refusing the call (abstraction-resource CONTRACT.md RES-T4).
        """
        import abstraction.resource as resource
        return resource.TableClient(self._bind("abstraction.resource", "abstraction.resource/table@1", guarantees, scope))

    def resolve_resource_leases(self, *, guarantees=(), scope: Scope = Scope.ANY):
        """Hold a scarce resource, and be asked for it back.

        Every Acquire remains subject to abstraction.resource/hold on the resource
        asked for, which refuses a program with no rule rather than narrowing
        anything (RES-L1).
        """
        import abstraction.resource as resource
        return resource.LeasesClient(self._bind("abstraction.resource", "abstraction.resource/leases@1", guarantees, scope))

    def _bind(self, capability, contract, guarantees, scope):
        guarantees = list(guarantees)
        if not isinstance(scope, Scope) or any(
                not isinstance(g, str) or not g for g in guarantees) or len(set(guarantees)) != len(guarantees):
            raise ValueError("invalid resolution requirements")
        if self._platform is not None:
            raise ResolutionError(RUNTIME_UNAVAILABLE, capability, contract, "the installed runtime",
                                  platform=self._platform)
        request = wire.ResolveRequest(capability=capability,
                                      contracts=[contract],
                                      guarantees=guarantees, scope=scope)
        end = self._options["deadline"]
        if end is None:
            end = time.monotonic() + self._options["timeout"]
        options = dict(self._options, deadline=end)
        server, endpoint = self._server, self._endpoint

        def refused(status):
            if self._endpoint is not None:
                looked_for = "the explicit endpoint " + self._endpoint
            else:
                looked_for = "the installed runtime" + ("" if endpoint is None else " at " + endpoint)
            return ResolutionError(status, capability, contract, looked_for)
        try:
            if endpoint is None and server is None:
                server = self._library.select_runtime(**options)
            if endpoint is None:
                endpoint = self._library.runtime_endpoint()
            # The resolver endpoint serves sessions (FRAMING.md "Sessions"); the
            # shared library keeps its connection for the next resolution.
            result = wire.ResolverClient(FrameTransport(self._library, endpoint, server=server, sessions=True,
                                                        **options)).resolve(request)
        except FrameError as error:
            # The caller's own cancellation stays a transport outcome.
            if error.status == CANCELLED:
                raise
            raise refused(RUNTIME_UNAVAILABLE) from error
        ref = result.reference
        if result.status != "resolved":
            if ref is not None:
                raise refused("invalid_resolution")
            raise refused(result.status)
        if (ref is None or not ref.provider or not ref.endpoint or "\0" in ref.endpoint
                or ref.capability != request.capability or ref.contract not in request.contracts
                or ref.scope not in (Scope.LOCAL, Scope.REMOTE) or (scope != Scope.ANY and ref.scope != scope)
                or len(set(ref.guarantees)) != len(ref.guarantees)
                or any(not g for g in ref.guarantees)
                or not set(guarantees).issubset(ref.guarantees)):
            raise refused("invalid_resolution")
        if ref.scope not in (Scope.LOCAL, Scope.REMOTE) or ref.transport != "oa-framed-local@1":
            raise refused("unsupported_transport")
        if self._provider_trust is not None:
            server = self._provider_trust(ref)
            if not isinstance(server, ServerExpectation):
                raise ValueError("provider_trust must return independent server expectation")
        if time.monotonic() >= end:
            raise FrameError(TIMEOUT, "resolution deadline expired")
        return Binding(self._library, ref.endpoint, reference=ref, server=server,
                       max_frame=2*1024*1024 if capability == "abstraction.job" else 1024*1024, **self._options)


_EVALUATED = ("permitted", "denied", "not_granted", "unknown_action")


def _rights_text(value, limit):
    import unicodedata
    return (isinstance(value, str) and 0 < len(value.encode("utf-8")) <= limit
            and not any(unicodedata.category(c) == "Cc" for c in value))


def _rights_rule(rule):
    import os.path
    s = rule.subject
    return (_rights_text(s.account, 128) and _rights_text(s.program, 4096)
            and os.path.isabs(s.program) and os.path.normpath(s.program) == s.program
            and _rights_text(rule.action, 128) and _rights_text(rule.resource, 1024)
            and type(rule.permit) is bool)


class Rights:
    """Point-in-time decisions. Evaluated outcomes carry the observed policy revision; refusals carry none."""

    def __init__(self, transport):
        import abstraction.rights.api as rights
        self._client = rights.AuthorizationClient(transport)

    @staticmethod
    def _checked(decision):
        if decision.outcome not in _EVALUATED + ("invalid", "forbidden", "unavailable"):
            raise ValueError("unknown decision outcome")
        if (decision.outcome in _EVALUATED) != bool(decision.policy_revision):
            raise ValueError("inconsistent decision revision")
        return decision

    def decide(self, action, resource):
        return self._checked(self._client.decide(action, resource))

    def decide_for(self, subject, action, resource):
        return self._checked(self._client.decide_for(subject, action, resource))


class RightsOperator:
    """Conditional administration at an expected revision. A lost reply is uncertain and is never retried."""

    def __init__(self, transport):
        import abstraction.rights.api as rights
        self._codec = rights
        self._client = rights.AuthorizationOperatorClient(transport)

    def list_policy(self, cursor, limit):
        if (not isinstance(cursor, str) or len(cursor.encode("utf-8")) > 256
                or type(limit) is not int or not 1 <= limit <= 64):
            raise ValueError("invalid policy range")
        page = self._client.list_policy(cursor, limit)
        if page.outcome != "page":
            if page.revision or page.catalog or page.rules or page.next or page.complete:
                raise ValueError("malformed policy refusal")
            return page
        if (not _rights_text(page.revision, 128) or not 1 <= len(page.catalog) <= 64
                or len(page.rules) > limit or len(page.next.encode("utf-8")) > 256
                or page.complete != (page.next == "")
                or (not page.complete and (not page.rules or page.next == cursor))):
            raise ValueError("malformed policy page")
        if any(not _rights_text(a, 128) for a in page.catalog) or page.catalog != sorted(set(page.catalog)):
            raise ValueError("invalid catalogue")
        seen = set()
        for rule in page.rules:
            key = (rule.subject.account, rule.subject.program, rule.action, rule.resource)
            if not _rights_rule(rule) or rule.action not in page.catalog or key in seen:
                raise ValueError("invalid policy rule")
            seen.add(key)
        return page

    def _edit(self, result, subject, action, resource, permit):
        observed = result.outcome in ("applied", "conflict")
        if result.outcome not in ("applied", "conflict", "invalid", "forbidden", "unavailable"):
            raise ValueError("unknown edit outcome")
        if observed != _rights_text(result.revision, 128) or (not observed and result.current is not None):
            raise ValueError("malformed edit revision")
        current = result.current
        if current is not None and (not _rights_rule(current) or current.subject.account != subject.account
                                    or current.subject.program != subject.program
                                    or current.action != action or current.resource != resource):
            raise ValueError("mismatched current rule")
        if result.outcome == "applied" and ((permit is not None) != (current is not None)
                                            or (permit is not None and current.permit != permit)):
            raise ValueError("malformed applied state")
        return result

    def set_rule(self, expected_revision, rule):
        if not _rights_text(expected_revision, 128) or not _rights_rule(rule):
            raise ValueError("invalid policy edit")
        result = self._client.set_rule(expected_revision, rule)
        return self._edit(result, rule.subject, rule.action, rule.resource, rule.permit)

    def revoke_rule(self, expected_revision, subject, action, resource):
        probe = self._codec.PolicyRule(subject=subject, action=action, resource=resource, permit=False)
        if not _rights_text(expected_revision, 128) or not _rights_rule(probe):
            raise ValueError("invalid policy revoke")
        result = self._client.revoke_rule(expected_revision, subject, action, resource)
        return self._edit(result, subject, action, resource, None)


class ConfigObserver:
    """Explicit run overrides bind the cursor; changed overrides need an empty cursor.

    snapshot carries a new cursor; unchanged and refusals keep the supplied
    cursor and carry no snapshot.
    """

    def __init__(self, transport):
        import abstraction.config as config
        self._codec = config
        self._transport = transport

    def observe(self, overrides, cursor, wait_ms):
        if (not isinstance(cursor, str) or len(cursor.encode("utf-8")) > 512
                or type(wait_ms) is not int or not 0 <= wait_ms <= 30000):
            raise ValueError("invalid observation bounds")
        for value in (overrides.nas_store, overrides.store, overrides.log_sink, overrides.log_service):
            if len(value.encode("utf-8")) > 4096:
                raise ValueError("oversized override")
        transport = self._transport
        if transport.deadline is None:
            transport = transport.with_waiting(deadline=time.monotonic() + transport.timeout + wait_ms / 1000,
                                               cancellation=transport.cancellation)
        result = self._codec.ConfigObserverClient(transport).observe(overrides, cursor, wait_ms)
        if result.outcome == "snapshot":
            if (result.snapshot is None or not result.cursor or result.cursor == cursor
                    or len(result.cursor.encode("utf-8")) > 512):
                raise ValueError("malformed observation snapshot")
        elif (result.snapshot is not None or result.cursor != cursor
                or (result.outcome == "unchanged" and cursor == "")):
            raise ValueError("observation refusal changed cursor")
        return result


def _history_bounds(cursor, max_records, max_bytes):
    if (not isinstance(cursor, str) or type(max_records) is not int
            or type(max_bytes) is not int or not 1 <= max_records <= 256
            or not 1 <= max_bytes <= 65536):
        raise ValueError("invalid history cursor or limits")


class LogHistory:
    def __init__(self, transport):
        import abstraction.logging as logging
        self._codec = logging
        self._client = logging.HistoryReaderClient(transport)

    def read(self, cursor, max_records, max_bytes):
        _history_bounds(cursor, max_records, max_bytes)
        return self._check(self._client.read(cursor, max_records, max_bytes), cursor, max_records, max_bytes,
                           ("gap", "unavailable", "invalid_request", "record_too_large", "corrupt"))

    def _check(self, page, cursor, max_records, max_bytes, refusals):
        if page.outcome == "page":
            if (len(page.records) > max_records or not page.next
                    or (page.records and page.next == cursor)
                    or (not page.records and not page.at_end)
                    or sum(len(self._codec.encode(r)) for r in page.records) > max_bytes):
                raise ValueError("inconsistent history page")
        elif (page.outcome not in refusals
                or page.records or page.next != cursor or page.at_end):
            raise ValueError("inconsistent history refusal")
        return page


class LogObserver(LogHistory):
    """Long-poll observation with the reader's bounds; forbidden and policy_unavailable raise ServiceError."""

    def __init__(self, transport):
        import abstraction.logging as logging
        self._codec = logging
        self._transport = transport
        self._client = None

    def observe(self, cursor, max_records, max_bytes, wait_ms):
        _history_bounds(cursor, max_records, max_bytes)
        if type(wait_ms) is not int or not 0 <= wait_ms <= 30000:
            raise ValueError("wait_ms outside 0..30000")
        transport = self._transport
        if transport.deadline is None:
            transport = transport.with_waiting(deadline=time.monotonic() + transport.timeout + wait_ms / 1000,
                                               cancellation=transport.cancellation)
        page = self._codec.HistoryObserverClient(transport).observe(cursor, max_records, max_bytes, wait_ms)
        return self._check(page, cursor, max_records, max_bytes,
                           ("gap", "unavailable", "invalid_request", "record_too_large", "corrupt", "unsupported"))
