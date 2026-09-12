#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/job/acceptance/rec.h>
#include <memory>
#include <mutex>

namespace abstraction::facade {
namespace job_api = abstraction::job::acceptance;

// Owns a fixed endpoint, not a provider or request identity. A failed call is
// never retried, rerouted or converted into definite nonacceptance.
// Binding requirements are mandatory for Submit and Reconcile. Submit merges
// them into a private copy, preserving the caller's explicit argument object.
// Copies share a synchronized owner pin; concurrent calls never replace it.
class JobsClient : public job_api::RecoverableAcceptance {
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
        scoped.transport_ = ipc::FrameTransport(endpoint_, deadline, MaxFrameBytes);
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
    return JobsClient(binding.endpoint, 5000, request.guarantees);
}
inline JobsClient ResolveJobs(const ResolutionClient& resolver,
                              std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/acceptance@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request, deadline));
    return JobsClient(binding.endpoint, deadline, request.guarantees);
}

}
