#pragma once
#include <abstraction/facade/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <abstraction/ipc/bootstrap.hpp>
#include <algorithm>
#include <memory>
#include <mutex>
#include <cstdlib>
#ifdef _WIN32
#include <abstraction/ipc/process.hpp>
#endif

namespace abstraction::facade {

inline std::string runtime_endpoint() { return ipc::runtime_endpoint(); }

class ResolutionError : public std::runtime_error {
public:
    std::string status;
    explicit ResolutionError(std::string value)
        : std::runtime_error("resolution: " + value), status(std::move(value)) {}
};

namespace resolution_detail {
inline bool contains(const std::vector<std::string>& values, const std::string& value) {
    return std::find(values.begin(), values.end(), value) != values.end();
}
inline bool distinct(const std::vector<std::string>& values) {
    for (std::size_t i = 0; i < values.size(); ++i) {
        if (values[i].empty() ||
            std::find(values.begin(), values.begin() + i, values[i]) != values.begin() + i)
            return false;
    }
    return true;
}
inline bool satisfies(const std::vector<std::string>& available,
                      const std::vector<std::string>& required) {
    for (const auto& value : required) if (!contains(available, value)) return false;
    return true;
}
}

inline void validate_resolve_request(const ResolveRequest& request) {
    if (request.capability.empty() || request.contracts.empty() ||
        !resolution_detail::distinct(request.contracts) ||
        !resolution_detail::distinct(request.guarantees) ||
        (request.scope != "any" && request.scope != "local" && request.scope != "remote"))
        throw ResolutionError("invalid_request");
}

// Generated codecs check wire shape. Cross-field selection meaning is checked
// before a reference can supply an endpoint to a capability client.
inline void validate_resolve_result(const ResolveRequest& request, const ResolveResult& result) {
    validate_resolve_request(request);
    if (!resolution_detail::contains(kResolutionStatusNames, result.status))
        throw ResolutionError("invalid_resolution");
    if (result.status != "resolved") {
        if (result.reference) throw ResolutionError("invalid_resolution");
        return;
    }
    if (!result.reference) throw ResolutionError("invalid_resolution");
    const auto& ref = *result.reference;
    if (ref.provider.empty() || ref.capability != request.capability || ref.contract.empty() ||
        !resolution_detail::contains(request.contracts, ref.contract) ||
        (ref.scope != "local" && ref.scope != "remote") ||
        (request.scope != "any" && request.scope != ref.scope) ||
        ref.transport.empty() || ref.endpoint.empty() || ref.endpoint.find('\0') != std::string::npos ||
        !resolution_detail::distinct(ref.guarantees) ||
        !resolution_detail::satisfies(ref.guarantees, request.guarantees))
        throw ResolutionError("invalid_resolution");
}

inline ServiceReference local_binding(const ResolveRequest& request, const ResolveResult& result) {
    validate_resolve_result(request, result);
    if (result.status != "resolved") throw ResolutionError(result.status);
    const auto& ref = *result.reference;
    if (ref.scope != "local" || ref.transport != "oa-framed-local@1")
        throw ResolutionError("unsupported_transport");
    return ref;
}

// Default clients select and retain independent installed-server evidence on
// their first call. Explicit endpoints accept an explicit server expectation.
class ResolutionClient {
public:
    ResolutionClient() : endpoint_(runtime_endpoint()), timeout_(5000), selection_(std::make_shared<Selection>()) {}
    explicit ResolutionClient(std::string endpoint,
                              std::uint32_t timeout_ms = 5000)
        : endpoint_(std::move(endpoint)), timeout_(timeout_ms) {}

    ResolutionClient WithCancellation(ipc::CancellationToken token) const {
        auto scoped = *this;
        scoped.cancellation_ = std::move(token);
        return scoped;
    }
    ResolutionClient WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {
        auto scoped = *this; scoped.server_ = std::move(server); scoped.selection_.reset(); return scoped;
    }
    std::optional<ipc::ServerExpectation> Server() const {
        if (!selection_) return server_;
        std::lock_guard<std::mutex> lock(selection_->mutex);
        return selection_->server;
    }
    ipc::CancellationToken Cancellation() const {return cancellation_;}
    std::uint32_t Timeout() const { return timeout_; }
    ResolveResult Resolve(const ResolveRequest& request) const {
        return Resolve(request, ipc::Clock::now() + std::chrono::milliseconds(timeout_));
    }
    ResolveResult Resolve(const ResolveRequest& request, ipc::Deadline deadline) const {
        validate_resolve_request(request);
        auto server = selected_server(deadline);
        ipc::FrameTransport transport(endpoint_, deadline, 1 << 20);
        transport = transport.WithCancellation(cancellation_).WithServerExpectation(server);
        return resolve(request, transport);
    }
private:
    struct Selection {
        std::mutex mutex;
        std::optional<ipc::ServerExpectation> server;
    };
    std::optional<ipc::ServerExpectation> selected_server(ipc::Deadline deadline) const {
        if (!selection_) return server_;
        {
            std::lock_guard<std::mutex> lock(selection_->mutex);
            if (selection_->server) return selection_->server;
        }
        auto selected = ipc::SelectRuntime(deadline, cancellation_);
        std::lock_guard<std::mutex> lock(selection_->mutex);
        if (!selection_->server) selection_->server = std::move(selected);
        return selection_->server;
    }
    static ResolveResult resolve(const ResolveRequest& request, ipc::FrameTransport& transport) {
        validate_resolve_request(request);
        ResolverClient<ipc::FrameTransport> client(transport);
        auto result = client.Resolve(request);
        validate_resolve_result(request, result);
        return result;
    }
    std::string endpoint_;
    std::uint32_t timeout_;
    ipc::CancellationToken cancellation_;
    std::optional<ipc::ServerExpectation> server_;
    std::shared_ptr<Selection> selection_;
};
// Generated service descriptors carry the authoritative wire name. Legacy wire
// names without a capability/profile split cannot be resolved through this API.
template<class Service>
ResolveRequest service_request(std::vector<std::string> guarantees = {}, std::string scope = "any") {
    const std::string capability(Service::capability), contract(Service::wire_name);
    if (capability.empty() || contract.compare(0, capability.size()+1, capability+"/") != 0)
        throw ResolutionError("invalid_service_descriptor");
    ResolveRequest request{capability, {contract}, std::move(guarantees), std::move(scope)};
    validate_resolve_request(request);
    return request;
}

// Stable heap ownership keeps the generated client's transport reference valid
// across moves. The generated interface stays usable with any suitable transport.
template<class Service, class Transport>
class BoundService {
    using Client = typename Service::template Client<Transport>;
    struct State {
        ServiceReference reference;
        Transport transport;
        Client client;
        State(ServiceReference r, Transport t)
            : reference(std::move(r)), transport(std::move(t)), client(transport) {}
    };
    std::unique_ptr<State> state_;
public:
    BoundService(ServiceReference reference, Transport transport)
        : state_(std::make_unique<State>(std::move(reference), std::move(transport))) {}
    BoundService(BoundService&&) noexcept = default;
    BoundService& operator=(BoundService&&) noexcept = default;
    BoundService(const BoundService&) = delete;
    BoundService& operator=(const BoundService&) = delete;
    Client* operator->() { return &state_->client; }
    const ServiceReference& Reference() const { return state_->reference; }
};

// An explicitly supplied transport owns its identity/scope guarantees. This
// common binder checks the same descriptor and reference selection invariants.
template<class Service, class Transport>
BoundService<Service, Transport> BindService(const ServiceReference& reference, Transport transport,
        const std::vector<std::string>& guarantees = {}, const std::string& scope = "any") {
    auto request = service_request<Service>(guarantees, scope);
    validate_resolve_result(request, ResolveResult{"resolved", reference});
    return {reference, std::move(transport)};
}

template<class Service>
BoundService<Service, ipc::FrameTransport> ResolveService(
        const ResolutionClient& resolver, const std::vector<std::string>& guarantees,
        const std::string& scope, ipc::Deadline deadline) {
    auto request = service_request<Service>(guarantees, scope);
    auto reference = local_binding(request, resolver.Resolve(request, deadline));
    auto transport = ipc::FrameTransport(reference.endpoint, deadline, 2 * 1024 * 1024)
        .WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
    return {std::move(reference), std::move(transport)};
}

template<class Service>
BoundService<Service, ipc::FrameTransport> ResolveService(
        const ResolutionClient& resolver, const std::vector<std::string>& guarantees = {},
        const std::string& scope = "any") {
    auto request = service_request<Service>(guarantees, scope);
    auto reference = local_binding(request, resolver.Resolve(request));
    auto transport = ipc::FrameTransport(reference.endpoint, resolver.Timeout(), 2 * 1024 * 1024)
        .WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
    return {std::move(reference), std::move(transport)};
}

}
