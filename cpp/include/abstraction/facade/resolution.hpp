#pragma once
#include <abstraction/facade/rec.h>
#include <abstraction/facade/activation.hpp>
#include <abstraction/ipc/frame.hpp>
#include <abstraction/ipc/bootstrap.hpp>
#include <algorithm>
#include <exception>
#include <memory>
#include <mutex>
#include <cstdlib>
#ifdef _WIN32
#include <abstraction/ipc/process.hpp>
#endif
#if defined(__APPLE__)
#include <TargetConditionals.h>
#endif

namespace abstraction::facade {

inline std::string runtime_endpoint() { return ipc::runtime_endpoint(); }

// Client-side resolution statuses; the resolver's own refusals keep their words.
inline constexpr std::string_view kRuntimeUnavailable = "runtime_unavailable";
inline constexpr std::string_view kInvalidResolution = "invalid_resolution";
inline constexpr std::string_view kUnsupportedTransport = "unsupported_transport";

// The platform this build targets when the runtime's platform declaration lists
// it as unsupported: "android" or "macos". Empty on every other platform.
// Defining ABSTRACTION_FACADE_TARGET_PLATFORM before the first include names
// the target explicitly.
inline std::string_view unsupported_platform() {
#if defined(ABSTRACTION_FACADE_TARGET_PLATFORM)
    const std::string_view target = ABSTRACTION_FACADE_TARGET_PLATFORM;
    return target == "android" || target == "macos" ? target : std::string_view();
#elif defined(__ANDROID__)
    return "android";
#elif defined(__APPLE__) && TARGET_OS_OSX
    return "macos";
#else
    return {};
#endif
}

// A resolve call that produced no usable service. status is runtime_unavailable
// when no runtime could be selected, reached or activated, upgrade_in_progress
// when activating the installed runtime was refused during its upgrade (cause
// holds the ActivationError), the resolver's refusal word,
// invalid_resolution for an answer that failed validation, invalid_request for a
// request refused before sending, or unsupported_transport for a reference this
// binding cannot use (scope and transport name it). cause holds the selection,
// transport or activation failure for runtime_unavailable. platform names a platform the
// runtime declares unsupported; that runtime_unavailable has no cause. The
// caller's own cancellation, and a deadline already past when resolution starts,
// stay ipc::FrameError.
class ResolutionError : public std::runtime_error {
public:
    std::string status;
    std::string capability, contract, looked_for;
    std::string scope, transport;
    std::string platform;
    std::exception_ptr cause;

    explicit ResolutionError(std::string status_word)
        : std::runtime_error("service resolution: " + status_word), status(std::move(status_word)) {}
    ResolutionError(std::string status_word, std::string capability_name, std::string contract_name,
                    std::string looked_for_text, std::exception_ptr cause_ptr = nullptr)
        : std::runtime_error(message(status_word, contract_name, capability_name, looked_for_text)),
          status(std::move(status_word)), capability(std::move(capability_name)), contract(std::move(contract_name)),
          looked_for(std::move(looked_for_text)), cause(std::move(cause_ptr)) {}

    // runtime_unavailable on a platform the runtime declares unsupported.
    static ResolutionError for_unsupported_platform(std::string capability_name, std::string contract_name,
                                                    std::string looked_for_text, std::string platform_name) {
        return ResolutionError(PlatformTag{}, std::move(capability_name), std::move(contract_name),
                               std::move(looked_for_text), std::move(platform_name));
    }

    // The resolver's refusal, when status is one.
    std::optional<ResolutionStatus> refusal() const {
        auto word = parse_resolution_status(status);
        if (word && *word != ResolutionStatus::Resolved) return word;
        return std::nullopt;
    }
    // Rethrows the selection or transport failure; returns when there is none.
    void rethrow_cause() const { if (cause) std::rethrow_exception(cause); }

private:
    struct PlatformTag {};
    ResolutionError(PlatformTag, std::string capability_name, std::string contract_name,
                    std::string looked_for_text, std::string platform_name)
        : std::runtime_error(message(std::string(kRuntimeUnavailable), contract_name, capability_name, looked_for_text) +
                             ": no supported OpenAbstractions runtime exists for " + platform_name),
          status(kRuntimeUnavailable), capability(std::move(capability_name)), contract(std::move(contract_name)),
          looked_for(std::move(looked_for_text)), platform(std::move(platform_name)) {}
    static std::string message(const std::string& status, const std::string& contract,
                               const std::string& capability, const std::string& looked_for) {
        return "service resolution: " + status + ": " + contract + " (capability " + capability + ") at " + looked_for;
    }
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
    if (!resolution_detail::contains(kResolutionStatusNames, std::string(wire_name(result.status))))
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

// looked_for names what resolution tried, for the error a refusal throws.
inline ServiceReference local_binding(const ResolveRequest& request, const ResolveResult& result,
                                      const std::string& looked_for = "an unnamed resolver") {
    const std::string capability = request.capability;
    const std::string contract = request.contracts.empty() ? std::string() : request.contracts.front();
    try {
        validate_resolve_result(request, result);
    } catch (const ResolutionError& e) {
        throw ResolutionError(e.status, capability, contract, looked_for);
    }
    if (result.status != "resolved")
        throw ResolutionError(std::string(wire_name(result.status)), capability, contract, looked_for);
    const auto& ref = *result.reference;
    if ((ref.scope != "local" && ref.scope != "remote") || ref.transport != "oa-framed-local@1") {
        ResolutionError error(std::string(kUnsupportedTransport), capability, contract, looked_for);
        error.scope = std::string(wire_name(ref.scope));
        error.transport = ref.transport;
        throw error;
    }
    return ref;
}

// Default clients select and retain independent installed-server evidence on
// their first call. Explicit endpoints accept an explicit server expectation.
// On Windows, a default client's bind_local that finds the selected
// installation's resolver endpoint absent starts that installation's runtime
// once (see activate_installed) and resolves again within 2 s. Explicit
// endpoints, explicit server expectations, a refused or untrusted server and
// resolve (observation) never activate.
class ResolutionClient {
public:
    ResolutionClient() : endpoint_(runtime_endpoint()), timeout_(5000), selection_(std::make_shared<Selection>()) {}
    explicit ResolutionClient(std::string endpoint,
                              std::uint32_t timeout_ms = 5000)
        : endpoint_(std::move(endpoint)), timeout_(timeout_ms) {}

    ResolutionClient with_cancellation(ipc::CancellationToken token) const {
        auto scoped = *this;
        scoped.cancellation_ = std::move(token);
        return scoped;
    }
    ResolutionClient with_server_expectation(std::optional<ipc::ServerExpectation> server) const {
        auto scoped = *this; scoped.server_ = std::move(server); scoped.selection_.reset(); return scoped;
    }
    std::optional<ipc::ServerExpectation> server() const {
        if (!selection_) return server_;
        std::lock_guard<std::mutex> lock(selection_->mutex);
        return selection_->server;
    }
    ipc::CancellationToken cancellation() const {return cancellation_;}
    std::uint32_t timeout() const { return timeout_; }
    ResolveResult resolve(const ResolveRequest& request) const {
        return resolve(request, ipc::Clock::now() + std::chrono::milliseconds(timeout_));
    }
    ResolveResult resolve(const ResolveRequest& request, ipc::Deadline deadline) const {
        validate_resolve_request(request);
        auto server = selected_server(deadline);
        ipc::FrameTransport transport(endpoint_, deadline, 1 << 20);
        transport = transport.with_cancellation(cancellation_).with_server_expectation(server);
        return resolve(request, transport);
    }
    // Resolves and binds one local reference. Every outcome that yields no
    // service throws ResolutionError naming the capability, the contract and
    // what was looked for; see ResolutionError for the exceptions.
    // An activation without a caller deadline runs within kDefaultActivationBudget.
    ServiceReference bind_local(const ResolveRequest& request) const {
        return bind_local(request, ipc::Clock::now() + std::chrono::milliseconds(timeout_), std::nullopt);
    }
    ServiceReference bind_local(const ResolveRequest& request, ipc::Deadline deadline) const {
        return bind_local(request, deadline, deadline);
    }
    // What resolution tries: the installed runtime (at its endpoint once selected)
    // or the endpoint this client was given.
    std::string looked_for(bool selected) const {
        if (!selection_) return "the explicit endpoint " + endpoint_;
        return selected ? "the installed runtime at " + endpoint_ : "the installed runtime";
    }
private:
    ServiceReference bind_local(const ResolveRequest& request, ipc::Deadline deadline,
                                std::optional<ipc::Deadline> caller) const {
        const std::string capability = request.capability;
        const std::string contract = request.contracts.empty() ? std::string() : request.contracts.front();
        try {
            validate_resolve_request(request);
        } catch (const ResolutionError& e) {
            throw ResolutionError(e.status, capability, contract, looked_for(false));
        }
        if (deadline <= ipc::Clock::now()) throw ipc::FrameError("call deadline expired", ipc::Status::Timeout);
        if (const auto platform = unsupported_platform(); selection_ && !platform.empty())
            throw ResolutionError::for_unsupported_platform(capability, contract, looked_for(false), std::string(platform));
        std::optional<ipc::ServerExpectation> server;
        try {
            server = selected_server(deadline);
        } catch (const ipc::FrameError& e) {
            if (e.status == ipc::Status::Cancelled) throw;
            throw ResolutionError(std::string(kRuntimeUnavailable), capability, contract, looked_for(false), std::current_exception());
        }
        auto result = resolve_selected(request, deadline, server, true);
        if (!result) {
            try {
                activate_installed(*server, caller);
            } catch (const ActivationError& e) {
                const auto status = e.kind == ActivationError::Kind::UpgradeInProgress ? kUpgradeInProgress : kRuntimeUnavailable;
                throw ResolutionError(std::string(status), capability, contract, looked_for(true), std::current_exception());
            }
            const auto retry = ipc::Clock::now() + std::chrono::seconds(2);
            result = resolve_selected(request, caller ? std::min(*caller, retry) : retry, server, false);
        }
        return local_binding(request, *result, looked_for(true));
    }
    // Resolves at endpoint_ and reports a failure as ResolutionError. With
    // activatable, an absent resolver endpoint of a selected installation
    // returns nullopt, on the platform whose SDK activates it.
    std::optional<ResolveResult> resolve_selected(const ResolveRequest& request, ipc::Deadline deadline,
                                                  const std::optional<ipc::ServerExpectation>& server, bool activatable) const {
        const std::string capability = request.capability;
        const std::string contract = request.contracts.empty() ? std::string() : request.contracts.front();
        try {
            ipc::FrameTransport transport(endpoint_, deadline, 1 << 20);
            transport = transport.with_cancellation(cancellation_).with_server_expectation(server);
            return resolve(request, transport);
        } catch (const ipc::FrameError& e) {
            if (e.status == ipc::Status::Cancelled) throw;
            if (activatable && selection_ && server && activation_supported() && e.status == ipc::Status::IoError &&
                ipc::Clock::now() < deadline && activation_detail::endpoint_absent(endpoint_))
                return std::nullopt;
            throw ResolutionError(std::string(kRuntimeUnavailable), capability, contract, looked_for(true), std::current_exception());
        } catch (const ResolutionError& e) {
            throw ResolutionError(e.status, capability, contract, looked_for(true));
        }
    }
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
        auto selected = ipc::select_runtime(deadline, cancellation_);
        std::lock_guard<std::mutex> lock(selection_->mutex);
        if (!selection_->server) selection_->server = std::move(selected);
        return selection_->server;
    }
    static ResolveResult resolve(const ResolveRequest& request, ipc::FrameTransport& transport) {
        validate_resolve_request(request);
        ResolverClient<ipc::FrameTransport> client(transport);
        auto result = client.resolve(request);
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
ResolveRequest service_request(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) {
    const std::string capability(Service::kCapability), contract(Service::kWireName);
    if (capability.empty() || contract.compare(0, capability.size()+1, capability+"/") != 0)
        throw ResolutionError("invalid_service_descriptor");
    ResolveRequest request{capability, {contract}, std::move(guarantees), scope};
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
    const ServiceReference& reference() const { return state_->reference; }
};

// An explicitly supplied transport owns its identity/scope guarantees. This
// common binder checks the same descriptor and reference selection invariants.
template<class Service, class Transport>
BoundService<Service, Transport> bind_service(const ServiceReference& reference, Transport transport,
        const std::vector<std::string>& guarantees = {}, Scope scope = Scope::Any) {
    auto request = service_request<Service>(guarantees, scope);
    validate_resolve_result(request, ResolveResult{ResolutionStatus::Resolved, reference});
    return {reference, std::move(transport)};
}

template<class Service>
BoundService<Service, ipc::FrameTransport> resolve_service(
        const ResolutionClient& resolver, const std::vector<std::string>& guarantees,
        Scope scope, ipc::Deadline deadline) {
    auto request = service_request<Service>(guarantees, scope);
    auto reference = resolver.bind_local(request, deadline);
    auto transport = ipc::FrameTransport(reference.endpoint, deadline, 2 * 1024 * 1024)
        .with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
    return {std::move(reference), std::move(transport)};
}

template<class Service>
BoundService<Service, ipc::FrameTransport> resolve_service(
        const ResolutionClient& resolver, const std::vector<std::string>& guarantees = {},
        Scope scope = Scope::Any) {
    auto request = service_request<Service>(guarantees, scope);
    auto reference = resolver.bind_local(request);
    auto transport = ipc::FrameTransport(reference.endpoint, resolver.timeout(), 2 * 1024 * 1024)
        .with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
    return {std::move(reference), std::move(transport)};
}

// The runtime's provider declarations, abstraction.facade/registry@1: an
// operator tool, whose every call the runtime decides as provider.manage.
inline BoundService<RegistryService, ipc::FrameTransport> resolve_registry(
        const ResolutionClient& resolver, const std::vector<std::string>& guarantees = {},
        Scope scope = Scope::Any) {
    return resolve_service<RegistryService>(resolver, guarantees, scope);
}

// abstraction.facade/endpoint@1 Describe on one local endpoint: the services it
// hosts and each one's readiness. The description's program is the provider's
// own claim; pass a server expectation to bind the server.
inline Description describe_endpoint(const std::string& endpoint, std::uint32_t timeout_ms = 5000,
        std::optional<ipc::ServerExpectation> server = std::nullopt) {
    auto transport = ipc::FrameTransport(endpoint, timeout_ms, 1024 * 1024);
    if (server) transport = std::move(transport).with_server_expectation(server);
    EndpointClient<ipc::FrameTransport> client(transport);
    return client.describe();
}

}
