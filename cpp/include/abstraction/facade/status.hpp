#pragma once
#include <abstraction/facade/resolution.hpp>
#include <exception>

namespace abstraction::facade {
inline BootstrapObservation UnknownBootstrap() {
    BootstrapObservation value;
    value.state = "unknown";
    return value;
}

inline std::vector<ResolveRequest> DefaultStatusRequests() {
    std::vector<ResolveRequest> requests;
    for (const auto& contract : kDefaultRuntimeContracts) {
        ResolveRequest request;
        request.capability = contract.substr(0, contract.find('/'));
        request.contracts = {contract};
        request.scope = "local";
        requests.push_back(std::move(request));
    }
    return requests;
}

// A failed observation retains completed answers and unobserved requests.
// Bootstrap evidence belongs to the caller's independent platform observation.
struct RuntimeObservationResult {
    RuntimeObservation observation;
    std::exception_ptr error;
};

inline RuntimeObservationResult ObserveRuntime(
    const ResolutionClient& resolver, const std::vector<ResolveRequest>& requests,
    const BootstrapObservation& bootstrap, ipc::Deadline deadline) {
    RuntimeObservationResult result;
    result.observation.bootstrap = bootstrap;
    for (const auto& request : requests) {
        CapabilityObservation capability;
        capability.request = request;
        result.observation.capabilities.push_back(std::move(capability));
    }
    if (std::find(kBootstrapStateNames.begin(), kBootstrapStateNames.end(), bootstrap.state) ==
        kBootstrapStateNames.end()) {
        result.error = std::make_exception_ptr(std::invalid_argument("invalid bootstrap observation"));
        return result;
    }
    for (auto& capability : result.observation.capabilities) {
        try {
            capability.result = resolver.Resolve(capability.request, deadline);
        } catch (...) {
            result.error = std::current_exception();
            break;
        }
    }
    return result;
}
}
