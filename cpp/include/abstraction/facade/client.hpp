#pragma once
#include <abstraction/logging/client.hpp>
#include <abstraction/config/client.hpp>
#include <abstraction/router/client.hpp>
#include <abstraction/facade/resolution.hpp>
#include <abstraction/facade/jobs.hpp>

namespace abstraction::facade {
// Legacy accessors retain conventional endpoints. Explicit Resolve* accessors
// ask the runtime for a sufficient binding before creating a capability client.
class Machine {
public:
    Machine() = default;
    explicit Machine(std::string endpoint) : resolver_(std::move(endpoint)) {}
    logging::Logger Log() const { return logging::Logger{}; }
    config::Client Config() const { return config::Client{}; }
    router::Client Router() const { return router::Client{}; }

    logging::Logger ResolveLog(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return logging::Logger(Bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope).endpoint);
    }
    config::Client ResolveConfig(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return config::Client(Bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope).endpoint);
    }
    router::Client ResolveRouter(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return router::Client(Bind("abstraction.router", "abstraction.router/router@1", guarantees, scope).endpoint);
    }
    JobsClient ResolveJobs(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return facade::ResolveJobs(resolver_, std::move(guarantees), std::move(scope));
    }
private:
    ServiceReference Bind(const std::string& capability, const std::string& contract,
                          const std::vector<std::string>& guarantees, const std::string& scope) const {
        ResolveRequest request;
        request.capability = capability;
        request.contracts = {contract};
        request.guarantees = guarantees;
        request.scope = scope;
        return local_binding(request, resolver_.Resolve(request));
    }
    ResolutionClient resolver_;
};
inline Machine Discover() { return Machine{}; }
}
