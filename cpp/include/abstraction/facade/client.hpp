#pragma once
#include <abstraction/logging/client.hpp>
#include <abstraction/facade/logging.hpp>
#include <abstraction/config/client.hpp>
#include <abstraction/config/editor.hpp>
#include <abstraction/router/client.hpp>
#include <abstraction/model/client.hpp>
#include <abstraction/facade/resolution.hpp>
#include <abstraction/facade/jobs.hpp>
#include <abstraction/facade/inventory.hpp>
#include <abstraction/facade/status.hpp>

namespace abstraction::facade {
// Accessors resolve a sufficient binding before creating a capability client.
class Machine {
public:
    Machine() = default;
    explicit Machine(std::string endpoint) : resolver_(std::move(endpoint)) {}
    // One shared waiting token follows resolution into every resulting client.
    Machine WithServerExpectation(std::optional<ipc::ServerExpectation> server) const {
        auto copy=*this;copy.resolver_=resolver_.WithServerExpectation(std::move(server));return copy;
    }
    Machine WithCancellation(ipc::CancellationToken token) const {
        auto scoped = *this;
        scoped.resolver_ = resolver_.WithCancellation(std::move(token));
        return scoped;
    }
    RuntimeObservationResult Observe(
        const std::vector<ResolveRequest>& requests = DefaultStatusRequests(),
        const BootstrapObservation& bootstrap = UnknownBootstrap()) const {
        return Observe(requests, bootstrap, ipc::Clock::now() + std::chrono::seconds(5));
    }
    RuntimeObservationResult Observe(const std::vector<ResolveRequest>& requests,
                                     const BootstrapObservation& bootstrap,
                                     ipc::Deadline deadline) const {
        return ObserveRuntime(resolver_, requests, bootstrap, deadline);
    }
    logging::Logger Log() const { return ResolveLog(); }
    config::Client Config() const { return ResolveConfig(); }
    router::Client Router() const { return ResolveRouter(); }

    logging::Logger ResolveLog(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return logging::Logger(Bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    logging::History ResolveLogReader(std::vector<std::string> guarantees = {},std::string scope="any") const {
        return logging::History(Bind("abstraction.logging","abstraction.logging/reader@1",guarantees,scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    logging::History ResolveLogReader(std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) const {
        return logging::History(Bind("abstraction.logging","abstraction.logging/reader@1",guarantees,scope,deadline).endpoint,deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    logging::Observer ResolveLogObserver(std::vector<std::string> guarantees={},std::string scope="any")const {
        return facade::ResolveLogObserver(resolver_,std::move(guarantees),std::move(scope));
    }
    logging::Observer ResolveLogObserver(std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline)const {
        return facade::ResolveLogObserver(resolver_,std::move(guarantees),std::move(scope),deadline);
    }
    config::Client ResolveConfig(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return config::Client(Bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    config::Editor ResolveConfigEditor(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return config::Editor(Bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    config::Editor ResolveConfigEditor(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return config::Editor(Bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope, deadline).endpoint, deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    // Explicit operation scopes share one deadline across resolution and calls.
    logging::Logger ResolveLog(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return logging::Logger(Bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope, deadline).endpoint, deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    config::Client ResolveConfig(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return config::Client(Bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope, deadline).endpoint, deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    router::Client ResolveRouter(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return router::Client(Bind("abstraction.router", "abstraction.router/router@1", guarantees, scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    router::Client ResolveRouter(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return router::Client(Bind("abstraction.router", "abstraction.router/router@1", guarantees, scope, deadline).endpoint, deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    JobsClient ResolveJobs(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return facade::ResolveJobs(resolver_, std::move(guarantees), std::move(scope));
    }
    JobsClient ResolveJobs(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return facade::ResolveJobs(resolver_, std::move(guarantees), std::move(scope), deadline);
    }
    JobsClient ResolveJobOperations(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return facade::ResolveJobOperations(resolver_, std::move(guarantees), std::move(scope));
    }
    JobsClient ResolveJobOperations(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return facade::ResolveJobOperations(resolver_, std::move(guarantees), std::move(scope), deadline);
    }
    JobInventoryClient ResolveJobInventory(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return facade::ResolveJobInventory(resolver_, std::move(guarantees), std::move(scope));
    }
    JobInventoryClient ResolveJobInventory(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return facade::ResolveJobInventory(resolver_, std::move(guarantees), std::move(scope), deadline);
    }

    model::Client ResolveModel(std::vector<std::string> guarantees = {}, std::string scope = "any") const {
        return model::Client(Bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope).endpoint).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
    }
    model::Client ResolveModel(std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) const {
        return model::Client(Bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope, deadline).endpoint, deadline).WithCancellation(resolver_.Cancellation()).WithServerExpectation(resolver_.Server());
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
    ServiceReference Bind(const std::string& capability, const std::string& contract,
                          const std::vector<std::string>& guarantees, const std::string& scope,
                          ipc::Deadline deadline) const {
        ResolveRequest request;
        request.capability = capability;
        request.contracts = {contract};
        request.guarantees = guarantees;
        request.scope = scope;
        return local_binding(request, resolver_.Resolve(request, deadline));
    }
    ResolutionClient resolver_;
};
inline Machine Discover() { return Machine{}; }
}
