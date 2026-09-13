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


class LogHistory:
    def __init__(self, transport):
        from abstraction.logging import rec as logging
        self._codec = logging
        self._client = logging.HistoryReaderClient(transport)

    def Read(self, cursor, max_records, max_bytes):
        if (not isinstance(cursor, str) or type(max_records) is not int
                or type(max_bytes) is not int or not 1 <= max_records <= 256
                or not 1 <= max_bytes <= 65536):
            raise ValueError("invalid history cursor or limits")
        page = self._client.Read(cursor, max_records, max_bytes)
        if page.outcome == "page":
            if (len(page.records) > max_records or not page.next
                    or (page.records and page.next == cursor)
                    or (not page.records and not page.at_end)
                    or sum(len(self._codec.encode(r)) for r in page.records) > max_bytes):
                raise ValueError("inconsistent history page")
        elif (page.outcome not in ("gap", "unavailable", "invalid_request", "record_too_large", "corrupt")
                or page.records or page.next != cursor or page.at_end):
            raise ValueError("inconsistent history refusal")
        return page
