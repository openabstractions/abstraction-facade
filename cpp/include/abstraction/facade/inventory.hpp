#pragma once
#include <abstraction/facade/jobs.hpp>
#include <set>

namespace abstraction::facade {
namespace job_detail {
inline void validate_inventory(const job_api::InventoryPage& page, const std::string& cursor, std::int64_t limit) {
    if (page.outcome != "page") {
        if (!resolution_detail::contains({"gap", "forbidden", "invalid", "unavailable"}, page.outcome) ||
            !page.snapshots.empty() || !page.next.empty() || page.complete)
            throw job_api::ServiceError("invalid_inventory", "inconsistent refusal page");
        return;
    }
    if (limit < 1 || limit > 64 || page.snapshots.size() > static_cast<std::size_t>(limit) || page.next.size() > 128 || !job_api::valid_utf8(page.next) ||
        (page.complete ? !page.next.empty() : (page.next.empty() || page.next == cursor)))
        throw job_api::ServiceError("invalid_inventory", "invalid page bounds or cursor progress");
    std::set<std::string> operations;
    std::string owner;
    for (const auto& s : page.snapshots) {
        validate_snapshot(s);
        if (!owner.empty() && owner != s.receipt.logical_owner)
            throw job_api::ServiceError("invalid_inventory", "mixed logical owners");
        owner = s.receipt.logical_owner;
        const auto& r = s.receipt;
        if (r.identity.key.empty() || r.identity.history_epoch.empty() || r.logical_owner.empty() ||
            r.operation_id.empty() || r.history_retention_ms <= 0 ||
            !resolution_detail::distinct(r.accepted_guarantees) || !operations.insert(r.operation_id).second)
            throw job_api::ServiceError("invalid_inventory", "invalid or duplicate receipt");
    }
}
}
// Read-only binding. Cursors belong to this service and caller scope. A lost
// initial reply starts a new enumeration; this client never retries implicitly.
class JobInventoryClient : public job_api::JobInventory {
public:
    explicit JobInventoryClient(std::string endpoint, std::uint32_t timeout_ms = 5000)
        : endpoint_(std::move(endpoint)), transport_(endpoint_, timeout_ms, JobsClient::MaxFrameBytes), owner_(std::make_shared<Owner>()) {}
    JobInventoryClient(std::string endpoint, ipc::Deadline deadline)
        : endpoint_(std::move(endpoint)), transport_(endpoint_, deadline, JobsClient::MaxFrameBytes), owner_(std::make_shared<Owner>()) {}
    JobInventoryClient WithDeadline(ipc::Deadline deadline) const {
        auto copy = *this;
        copy.transport_ = ipc::FrameTransport(endpoint_, deadline, JobsClient::MaxFrameBytes).WithCancellation(cancellation_).WithServerExpectation(server_);
        return copy;
    }
    JobInventoryClient WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {auto copy=*this;copy.server_=std::move(server);copy.transport_=transport_.WithServerExpectation(copy.server_);return copy;}
 JobInventoryClient WithCancellation(ipc::CancellationToken token) const {
        auto copy = *this;
        copy.cancellation_ = std::move(token);
        copy.transport_ = transport_.WithCancellation(copy.cancellation_);
        return copy;
    }
    job_api::InventoryPage ListWork(const std::string& cursor, const std::int64_t& limit) override {
        if (limit < 1 || limit > 64 || cursor.size() > 128 || !job_api::valid_utf8(cursor))
            throw job_api::ServiceError("invalid_request", "inventory limit must be 1..64");
        job_api::JobInventoryClient<ipc::FrameTransport> client(transport_);
        auto page = client.ListWork(cursor, limit);
        job_detail::validate_inventory(page, cursor, limit);
        if (!page.snapshots.empty()) {
            std::lock_guard<std::mutex> lock(owner_->mutex);
            const auto& owner = page.snapshots.front().receipt.logical_owner;
            if (!owner_->value.empty() && owner_->value != owner)
                throw job_api::ServiceError("invalid_inventory", "logical owner changed");
            owner_->value = owner;
        }
        return page;
    }
private:
    std::string endpoint_;
    ipc::CancellationToken cancellation_;
 std::optional<ipc::ServerExpectation> server_;
    ipc::FrameTransport transport_;
    struct Owner { std::mutex mutex; std::string value; };
    std::shared_ptr<Owner> owner_;
};
inline JobInventoryClient ResolveJobInventory(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, std::string scope = "any") {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/inventory@1"};
    request.guarantees = std::move(guarantees); request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request));
    return JobInventoryClient(binding.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline JobInventoryClient ResolveJobInventory(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.job";
    request.contracts = {"abstraction.job/inventory@1"};
    request.guarantees = std::move(guarantees); request.scope = std::move(scope);
    auto binding = local_binding(request, resolver.Resolve(request, deadline));
    return JobInventoryClient(binding.endpoint, deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
}
