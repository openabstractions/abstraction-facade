#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/job/acceptance/rec.h>
#include <memory>
#include <mutex>
#include <ostream>
#include <exception>

namespace abstraction::facade {
namespace job_api = abstraction::job::acceptance;
namespace job_detail {
inline void validate_snapshot(const job_api::OperationSnapshot& s) {
    if (!resolution_detail::contains({"pending", "running", "transferred", "complete", "failed", "cancelled"}, s.state) ||
        s.progress.done < 0 || s.progress.total < 0 ||
        (s.failure && (!resolution_detail::contains({"retryable", "permanent", "unknown"}, s.failure->classification))))
        throw job_api::ServiceError("invalid_observation", "invalid snapshot state or progress");
}
}


// Owns a fixed endpoint, not a provider or request identity. A failed call is
// never retried, rerouted or converted into definite nonacceptance.
// Binding requirements are mandatory for Submit and Reconcile. Submit merges
// them into a private copy, preserving the caller's explicit argument object.
// Copies share a synchronized owner pin; concurrent calls never replace it.
class JobsClient : public job_api::RecoverableAcceptance, public job_api::OperationControl {
public:
    static constexpr std::uint32_t MaxFrameBytes = 2u << 20;
    // expected_owner is the persisted acceptance logical owner after restart;
    // it is not the facade registration's provider identifier. Without it,
    // GetHistoryWindow or the first valid accepted receipt establishes the pin.
    explicit JobsClient(std::string endpoint, std::uint32_t timeout_ms = 5000,
                        std::vector<std::string> required_guarantees = {}, std::string expected_owner = {})
        : endpoint_(std::move(endpoint)), transport_(endpoint_, timeout_ms, MaxFrameBytes),
          required_(std::move(required_guarantees)), owner_(std::make_shared<OwnerState>(std::move(expected_owner))) {
        if (!resolution_detail::distinct(required_))
            throw job_api::ServiceError("invalid_submission", "invalid binding guarantees");
    }

    JobsClient(std::string endpoint, ipc::Deadline deadline,
               std::vector<std::string> required_guarantees = {}, std::string expected_owner = {})
        : JobsClient(std::move(endpoint), 5000, std::move(required_guarantees), std::move(expected_owner)) {
        transport_ = ipc::FrameTransport(endpoint_, deadline, MaxFrameBytes);
    }

    // Fresh waiting scope on this exact binding. Copies share the owner pin;
    // the original scope remains unchanged. A Submit timeout leaves acceptance
    // unresolved: recover explicitly with Reconcile using the original identity.
    JobsClient WithDeadline(ipc::Deadline deadline) const {
        auto scoped = *this;
        scoped.transport_ = ipc::FrameTransport(endpoint_, deadline, MaxFrameBytes).WithCancellation(cancellation_).WithServerExpectation(server_);
        return scoped;
    }

    // Cancellation affects waiting only. Copies preserve the original binding,
    // deadline and synchronized logical-owner pin.
    JobsClient WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {auto copy=*this;copy.server_=std::move(server);copy.transport_=transport_.WithServerExpectation(copy.server_);return copy;}
 JobsClient WithCancellation(ipc::CancellationToken token) const {
        auto scoped = *this;
        scoped.cancellation_ = std::move(token);
        scoped.transport_ = transport_.WithCancellation(scoped.cancellation_);
        return scoped;
    }
    job_api::HistoryWindow GetHistoryWindow() override {
        job_api::RecoverableAcceptanceClient<ipc::FrameTransport> client(transport_);
        auto result = client.GetHistoryWindow();
        if (result.logical_owner.empty() || result.history_epoch.empty() || result.minimum_retention_ms <= 0)
            throw job_api::ServiceError("invalid_acceptance", "invalid history window");
        bind_owner(result.logical_owner);
        return result;
    }
    job_api::AcceptanceResult Submit(const job_api::Submission& submission) override {
        validate_identity(submission.identity);
        if (submission.kind.empty() || !resolution_detail::distinct(submission.required_guarantees))
            throw job_api::ServiceError("invalid_submission", "invalid kind or guarantees");
        job_api::RecoverableAcceptanceClient<ipc::FrameTransport> client(transport_);
        auto outgoing = submission;
        for (const auto& guarantee : required_) {
            if (!resolution_detail::contains(outgoing.required_guarantees, guarantee))
                outgoing.required_guarantees.push_back(guarantee);
        }
        auto result = client.Submit(outgoing);
        validate_result(result, submission.identity, outgoing.required_guarantees);
        return result;
    }
    job_api::AcceptanceResult Reconcile(const job_api::RequestIdentity& identity) override {
        validate_identity(identity);
        job_api::RecoverableAcceptanceClient<ipc::FrameTransport> client(transport_);
        auto result = client.Reconcile(identity);
        validate_result(result, identity, required_);
        return result;
    }
    job_api::CancellationResult CancelWork(const job_api::RequestIdentity& identity) override {
        validate_identity(identity);
        job_api::RecoverableAcceptanceClient<ipc::FrameTransport> client(transport_);
        return client.CancelWork(identity);
    }
    job_api::ObservationResult ObserveWork(const job_api::RequestIdentity& identity) override {
        validate_identity(identity);
        job_api::OperationControlClient<ipc::FrameTransport> client(transport_);
        auto result = client.ObserveWork(identity);
        if (result.outcome != "observed") {
            if (!resolution_detail::contains({"unknown", "forbidden", "invalid", "definitely_not_accepted"}, result.outcome) || result.snapshot)
                throw job_api::ServiceError("invalid_observation", "inconsistent observation outcome");
            return result;
        }
        if (!result.snapshot) throw job_api::ServiceError("invalid_observation", "missing snapshot");
        const auto& s = *result.snapshot;
        job_detail::validate_snapshot(s);
        job_api::AcceptanceResult accepted; accepted.outcome = "accepted"; accepted.receipt = s.receipt;
        validate_result(accepted, identity, required_);
        return result;
    }
    // Caller retains original operation ID and total when assembling successive
    // chunks, rejecting changes across calls. No per-operation cache is retained.
    job_api::ResultRead ReadResult(const job_api::RequestIdentity& identity, const std::int64_t& offset, const std::int64_t& max_bytes) override {
        validate_identity(identity);
        if (offset < 0 || max_bytes < 1 || max_bytes > 65536)
            throw job_api::ServiceError("invalid_request", "offset and result chunk limit out of range");
        job_api::OperationControlClient<ipc::FrameTransport> client(transport_);
        auto result = client.ReadResult(identity, offset, max_bytes);
        if (result.outcome != "data") {
            if (!resolution_detail::contains({"not_ready", "unavailable", "unsupported", "unknown", "forbidden", "invalid"}, result.outcome) || result.chunk)
                throw job_api::ServiceError("invalid_result", "inconsistent result outcome");
            return result;
        }
        if (!result.chunk) throw job_api::ServiceError("invalid_result", "missing result chunk");
        const auto& c = *result.chunk;
        if (c.offset != offset || c.total < 0 || offset > c.total || c.data.size() > static_cast<std::size_t>(max_bytes) ||
            c.data.size() > 65536 || c.data.size() > static_cast<std::uint64_t>(c.total - offset) ||
            c.eof != (c.data.size() == static_cast<std::uint64_t>(c.total - offset)) || (c.data.empty() && !c.eof))
            throw job_api::ServiceError("invalid_result", "inconsistent result chunk");
        job_api::AcceptanceResult accepted; accepted.outcome = "accepted"; accepted.receipt = c.receipt;
        validate_result(accepted, identity, required_);
        return result;
    }
    struct ResultCopy {
        std::int64_t confirmed = 0;
        std::exception_ptr error;
    };
    // One chunk at a time; operation ID and total are pinned for this call.
    // On a throwing streambuf, confirmed excludes the current write: its partial
    // effects are unknowable. Caller owns partial output and may rethrow error.
    ResultCopy CopyResult(const job_api::RequestIdentity& identity, std::ostream& destination) {
        ResultCopy copy;
        try {
            if (!destination || !destination.rdbuf()) throw std::ios_base::failure("result stream unavailable");
            std::string operation;
            std::int64_t total = 0;
            for (;;) {
                auto result = ReadResult(identity, copy.confirmed, 65536);
                if (result.outcome != "data") throw job_api::ServiceError(result.outcome, "result copy unavailable");
                const auto& chunk = *result.chunk;
                if (operation.empty()) { operation = chunk.receipt.operation_id; total = chunk.total; }
                else if (operation != chunk.receipt.operation_id || total != chunk.total)
                    throw job_api::ServiceError("invalid_result", "result identity or total changed during copy");
                if (!chunk.data.empty()) {
                    if (!destination) throw std::ios_base::failure("result stream unavailable");
                    const auto count = static_cast<std::streamsize>(chunk.data.size());
                    const auto n = destination.rdbuf()->sputn(reinterpret_cast<const char*>(chunk.data.data()), count);
                    if (n < 0 || n > count) throw std::ios_base::failure("invalid result stream count");
                    copy.confirmed += n;
                    if (n != count) {destination.setstate(std::ios::badbit);throw std::ios_base::failure("short result write");}
                }
                if (chunk.eof) return copy;
            }
        } catch (...) {copy.error = std::current_exception();}
        return copy;
    }
private:
    static void validate_identity(const job_api::RequestIdentity& id) {
        if (id.key.empty() || id.history_epoch.empty())
            throw job_api::ServiceError("invalid_submission", "explicit key and history epoch required");
    }
    void validate_result(const job_api::AcceptanceResult& result, const job_api::RequestIdentity& id,
                                const std::vector<std::string>& required) {
        if (result.outcome != "accepted") {
            if (result.receipt) throw job_api::ServiceError("invalid_acceptance", "receipt on nonaccepted outcome");
            return;
        }
        if (!result.receipt) throw job_api::ServiceError("invalid_acceptance", "accepted result lacks receipt");
        const auto& r = *result.receipt;
        if (r.identity.key != id.key || r.identity.history_epoch != id.history_epoch ||
            r.logical_owner.empty() || r.operation_id.empty() || r.history_retention_ms <= 0 ||
            !resolution_detail::distinct(r.accepted_guarantees) ||
            !resolution_detail::satisfies(r.accepted_guarantees, required))
            throw job_api::ServiceError("invalid_acceptance", "mismatched or insufficient receipt");
        bind_owner(r.logical_owner);
    }
    void bind_owner(const std::string& owner) {
        std::lock_guard<std::mutex> lock(owner_->mutex);
        if (!owner_->value.empty() && owner_->value != owner)
            throw job_api::ServiceError("invalid_acceptance", "logical owner changed");
        owner_->value = owner;
    }
    std::string endpoint_;
    ipc::FrameTransport transport_;
    ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
    std::vector<std::string> required_;
    // History (or first valid receipt) pins this client to a logical owner.
    // Supply the recovered owner to the constructor after caller restart.
    struct OwnerState {
        explicit OwnerState(std::string initial) : value(std::move(initial)) {}
        std::mutex mutex;
        std::string value;
    };
    std::shared_ptr<OwnerState> owner_;
};

// This entry point links only facade_jobs, the generated job protocol and IPC.
// Caller persists its identity/owner before Submit and uses this same binding
// for reconciliation; a new ResolveJobs call never reconciles existing work.
inline JobsClient ResolveJobs(const ResolutionClient& resolver,
                              std::vector<std::string> guarantees = {}, std::string scope = "any") {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/acceptance@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request));
    return JobsClient(binding.endpoint, 5000, request.guarantees).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline JobsClient ResolveJobs(const ResolutionClient& resolver,
                              std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/acceptance@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request, deadline));
    return JobsClient(binding.endpoint, deadline, request.guarantees).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}

// New discovery can require operation control; accepted work keeps its binding.
inline JobsClient ResolveJobOperations(const ResolutionClient& resolver,
                              std::vector<std::string> guarantees = {}, std::string scope = "any") {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/operations@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request));
    return JobsClient(binding.endpoint, 5000, request.guarantees).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline JobsClient ResolveJobOperations(const ResolutionClient& resolver,
                              std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/operations@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request, deadline));
    return JobsClient(binding.endpoint, deadline, request.guarantees).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}

}
