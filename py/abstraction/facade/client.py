"""Typed service facade with shared native bootstrap; no provider fallback."""
import time
from abstraction.ipc import FrameTransport, Library, FrameError, TIMEOUT, ServerExpectation
from abstraction.facade import rec as wire


class ResolutionError(RuntimeError):
    def __init__(self, status):
        super().__init__("service resolution: " + status)
        self.status = status


class Machine:
    def __init__(self, endpoint=None, library=None, *, timeout=5.0, deadline=None, cancellation=None,
                 server=None, provider_trust=None):
        """Default discovery verifies the selected installed runtime and providers.

        Explicit endpoints retain compatibility unless server is supplied.
        provider_trust may select independent configured trust for other hosts.
        """
        if library is None:
            library = cancellation._library if cancellation is not None else Library()
        if provider_trust is not None and not callable(provider_trust):
            raise ValueError("provider_trust must be callable")
        self._library = library
        self._options = dict(timeout=timeout, deadline=deadline, cancellation=cancellation)
        self._endpoint, self._server, self._provider_trust = endpoint, server, provider_trust
        # Validate options without selecting, connecting or consuming a call budget.
        FrameTransport(library, endpoint if endpoint is not None else "validation", server=server, **self._options)

    def resolve_storage(self, *, guarantees=(), scope="any"):
        """Resolve one fixed content reader through the shared transport."""
        from abstraction.storage.content.client import Client
        return Client(self._bind("abstraction.storage", "abstraction.storage/content-reader@1", guarantees, scope))

    def resolve_storage_writer(self, *, guarantees=(), scope="any"):
        """Resolve one content writer; every call remains subject to its write policy."""
        from abstraction.storage.content.client import Writer
        return Writer(self._bind("abstraction.storage", "abstraction.storage/content-writer@1", guarantees, scope))

    def resolve_log(self, *, guarantees=(), scope="any"):
        """Return the generated SinkClient at one validated, fixed endpoint."""
        from abstraction.logging import rec as logging
        return logging.SinkClient(self._bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope))

    def resolve_config(self, *, guarantees=(), scope="any"):
        """Read existing provider settings with explicit per-call run overrides."""
        from abstraction.config import rec as config
        return config.ConfigReaderClient(self._bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope))

    def resolve_config_editor(self, *, guarantees=(), scope="any"):
        """Edit user settings with the service's compare-and-replace revision."""
        from abstraction.config import rec as config
        return config.ConfigEditorClient(self._bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope))

    def resolve_asks(self, *, guarantees=(), scope="any"):
        """Admit and observe questions in the caller's bound scope."""
        from abstraction.asks.api import rec as asks
        return asks.QuestionApplicationClient(self._bind("abstraction.asks", "abstraction.asks/application@1", guarantees, scope))

    def resolve_asks_operator(self, *, guarantees=(), scope="any"):
        """List, answer and retire questions; every call remains subject to the host's operator policy."""
        from abstraction.asks.api import rec as asks
        return asks.QuestionOperatorClient(self._bind("abstraction.asks", "abstraction.asks/operator@1", guarantees, scope))

    def resolve_model(self, *, guarantees=(), scope="any"):
        """Look up portable download requests; forbidden and unavailable are typed lookup outcomes."""
        from abstraction.model.api import rec as model
        return model.ModelResolverClient(self._bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope))

    def resolve_router(self, *, guarantees=(), scope="any"):
        """Read host inventory and pick hosts; policy refusals raise ServiceError codes forbidden or policy_unavailable."""
        from abstraction.router import rec as router
        return router.RouterClient(self._bind("abstraction.router", "abstraction.router/router@1", guarantees, scope))

    def resolve_jobs(self, *, guarantees=(), scope="any"):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Jobs
        return Jobs(self._bind("abstraction.job", "abstraction.job/acceptance@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_job_operations(self, *, guarantees=(), scope="any"):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Jobs
        return Jobs(self._bind("abstraction.job", "abstraction.job/operations@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_job_inventory(self, *, guarantees=(), scope="any"):
        guarantees = tuple(guarantees)
        from abstraction.facade.jobs import Inventory
        return Inventory(self._bind("abstraction.job", "abstraction.job/inventory@1", guarantees, scope), required_guarantees=guarantees)

    def resolve_log_reader(self, *, guarantees=(), scope="any"):
        """Return bounded history at one fixed endpoint; gaps require explicit restart."""
        return LogHistory(self._bind("abstraction.logging", "abstraction.logging/reader@1", guarantees, scope))

    def resolve_rights(self, *, guarantees=(), scope="any"):
        """Point-in-time decisions for the bound caller; DecideFor needs trusted-enforcer authority at the service."""
        return Rights(self._bind("abstraction.rights", "abstraction.rights/authorization@1", guarantees, scope))

    def resolve_rights_operator(self, *, guarantees=(), scope="any"):
        """Conditional policy administration; every call remains subject to the host's operator authorization."""
        return RightsOperator(self._bind("abstraction.rights", "abstraction.rights/operator@1", guarantees, scope))

    def resolve_config_observer(self, *, guarantees=(), scope="any"):
        """Latest-snapshot long-poll configuration observation; each call's budget is extended by its wait_ms."""
        return ConfigObserver(self._bind("abstraction.config", "abstraction.config/observer@1", guarantees, scope))

    def resolve_log_observer(self, *, guarantees=(), scope="any"):
        """Long-poll history observation; each call's budget is extended by its wait_ms."""
        return LogObserver(self._bind("abstraction.logging", "abstraction.logging/observer@1", guarantees, scope))

    def resolve_storage_changes(self, *, guarantees=(), scope="any"):
        """Observe added and removed objects; every call remains subject to the observe and read policies."""
        from abstraction.storage.content.client import Changes
        return Changes(self._bind("abstraction.storage", "abstraction.storage/content-changes@1", guarantees, scope))

    def _bind(self, capability, contract, guarantees, scope):
        guarantees = list(guarantees)
        if scope not in ("any", "local", "remote") or any(
                not isinstance(g, str) or not g for g in guarantees) or len(set(guarantees)) != len(guarantees):
            raise ValueError("invalid resolution requirements")
        request = wire.ResolveRequest(capability=capability,
                                      contracts=[contract],
                                      guarantees=guarantees, scope=scope)
        end = self._options["deadline"]
        if end is None:
            end = time.monotonic() + self._options["timeout"]
        options = dict(self._options, deadline=end)
        server = self._server
        if self._endpoint is None and server is None:
            server = self._library.select_runtime(**options)
        endpoint = self._endpoint if self._endpoint is not None else self._library.runtime_endpoint()
        result = wire.ResolverClient(FrameTransport(self._library, endpoint, server=server, **options)).Resolve(request)
        ref = result.reference
        if result.status != "resolved":
            if ref is not None:
                raise ResolutionError("invalid_resolution")
            raise ResolutionError(result.status)
        if (ref is None or not ref.provider or not ref.endpoint or "\0" in ref.endpoint
                or ref.capability != request.capability or ref.contract not in request.contracts
                or ref.scope not in ("local", "remote") or (scope != "any" and ref.scope != scope)
                or len(set(ref.guarantees)) != len(ref.guarantees)
                or any(not g for g in ref.guarantees)
                or not set(guarantees).issubset(ref.guarantees)):
            raise ResolutionError("invalid_resolution")
        if ref.scope != "local" or ref.transport != "oa-framed-local@1":
            raise ResolutionError("unsupported_transport")
        if self._provider_trust is not None:
            server = self._provider_trust(ref)
            if not isinstance(server, ServerExpectation):
                raise ValueError("provider_trust must return independent server expectation")
        if time.monotonic() >= end:
            raise FrameError(TIMEOUT, "resolution deadline expired")
        return FrameTransport(self._library, ref.endpoint, server=server, max_frame=2*1024*1024 if capability == "abstraction.job" else 1024*1024, **self._options)


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
        from abstraction.rights.api import rec as rights
        self._client = rights.AuthorizationClient(transport)

    @staticmethod
    def _checked(decision):
        if decision.outcome not in _EVALUATED + ("invalid", "forbidden", "unavailable"):
            raise ValueError("unknown decision outcome")
        if (decision.outcome in _EVALUATED) != bool(decision.policy_revision):
            raise ValueError("inconsistent decision revision")
        return decision

    def Decide(self, action, resource):
        return self._checked(self._client.Decide(action, resource))

    def DecideFor(self, subject, action, resource):
        return self._checked(self._client.DecideFor(subject, action, resource))


class RightsOperator:
    """Conditional administration at an expected revision. A lost reply is uncertain and is never retried."""

    def __init__(self, transport):
        from abstraction.rights.api import rec as rights
        self._codec = rights
        self._client = rights.AuthorizationOperatorClient(transport)

    def ListPolicy(self, cursor, limit):
        if (not isinstance(cursor, str) or len(cursor.encode("utf-8")) > 256
                or type(limit) is not int or not 1 <= limit <= 64):
            raise ValueError("invalid policy range")
        page = self._client.ListPolicy(cursor, limit)
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

    def SetRule(self, expected_revision, rule):
        if not _rights_text(expected_revision, 128) or not _rights_rule(rule):
            raise ValueError("invalid policy edit")
        result = self._client.SetRule(expected_revision, rule)
        return self._edit(result, rule.subject, rule.action, rule.resource, rule.permit)

    def RevokeRule(self, expected_revision, subject, action, resource):
        probe = self._codec.PolicyRule(subject=subject, action=action, resource=resource, permit=False)
        if not _rights_text(expected_revision, 128) or not _rights_rule(probe):
            raise ValueError("invalid policy revoke")
        result = self._client.RevokeRule(expected_revision, subject, action, resource)
        return self._edit(result, subject, action, resource, None)


class ConfigObserver:
    """Explicit run overrides bind the cursor; changed overrides need an empty cursor.

    snapshot carries a new cursor; unchanged and refusals keep the supplied
    cursor and carry no snapshot.
    """

    def __init__(self, transport):
        from abstraction.config import rec as config
        self._codec = config
        self._transport = transport

    def Observe(self, overrides, cursor, wait_ms):
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
        result = self._codec.ConfigObserverClient(transport).Observe(overrides, cursor, wait_ms)
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
        from abstraction.logging import rec as logging
        self._codec = logging
        self._client = logging.HistoryReaderClient(transport)

    def Read(self, cursor, max_records, max_bytes):
        _history_bounds(cursor, max_records, max_bytes)
        return self._check(self._client.Read(cursor, max_records, max_bytes), cursor, max_records, max_bytes,
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
        from abstraction.logging import rec as logging
        self._codec = logging
        self._transport = transport
        self._client = None

    def Observe(self, cursor, max_records, max_bytes, wait_ms):
        _history_bounds(cursor, max_records, max_bytes)
        if type(wait_ms) is not int or not 0 <= wait_ms <= 30000:
            raise ValueError("wait_ms outside 0..30000")
        transport = self._transport
        if transport.deadline is None:
            transport = transport.with_waiting(deadline=time.monotonic() + transport.timeout + wait_ms / 1000,
                                               cancellation=transport.cancellation)
        page = self._codec.HistoryObserverClient(transport).Observe(cursor, max_records, max_bytes, wait_ms)
        return self._check(page, cursor, max_records, max_bytes,
                           ("gap", "unavailable", "invalid_request", "record_too_large", "corrupt", "unsupported"))
