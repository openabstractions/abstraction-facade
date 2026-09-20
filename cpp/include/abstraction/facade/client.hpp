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
    Machine with_server_expectation(std::optional<ipc::ServerExpectation> server) const {
        auto copy=*this;copy.resolver_=resolver_.with_server_expectation(std::move(server));return copy;
    }
    Machine with_cancellation(ipc::CancellationToken token) const {
        auto scoped = *this;
        scoped.resolver_ = resolver_.with_cancellation(std::move(token));
        return scoped;
    }
    RuntimeObservationResult observe(
        const std::vector<ResolveRequest>& requests = default_status_requests(),
        const BootstrapObservation& bootstrap = unknown_bootstrap()) const {
        return observe(requests, bootstrap, ipc::Clock::now() + std::chrono::seconds(5));
    }
    RuntimeObservationResult observe(const std::vector<ResolveRequest>& requests,
                                     const BootstrapObservation& bootstrap,
                                     ipc::Deadline deadline) const {
        return observe_runtime(resolver_, requests, bootstrap, deadline);
    }
    logging::Logger log() const { return resolve_log(); }
    config::Client config() const { return resolve_config(); }
    router::Client router() const { return resolve_router(); }

    logging::Logger resolve_log(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return logging::Logger(bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    logging::History resolve_log_reader(std::vector<std::string> guarantees = {},Scope scope=Scope::Any) const {
        return logging::History(bind("abstraction.logging","abstraction.logging/reader@1",guarantees,scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    logging::History resolve_log_reader(std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) const {
        return logging::History(bind("abstraction.logging","abstraction.logging/reader@1",guarantees,scope,deadline).endpoint,deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    logging::Observer resolve_log_observer(std::vector<std::string> guarantees={},Scope scope=Scope::Any)const {
        return facade::resolve_log_observer(resolver_,std::move(guarantees),scope);
    }
    logging::Observer resolve_log_observer(std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline)const {
        return facade::resolve_log_observer(resolver_,std::move(guarantees),scope,deadline);
    }
    config::Client resolve_config(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return config::Client(bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    config::Editor resolve_config_editor(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return config::Editor(bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    config::Editor resolve_config_editor(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return config::Editor(bind("abstraction.config", "abstraction.config/editor@1", guarantees, scope, deadline).endpoint, deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    // Explicit operation scopes share one deadline across resolution and calls.
    logging::Logger resolve_log(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return logging::Logger(bind("abstraction.logging", "abstraction.logging/sink@1", guarantees, scope, deadline).endpoint, deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    config::Client resolve_config(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return config::Client(bind("abstraction.config", "abstraction.config/reader@1", guarantees, scope, deadline).endpoint, deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    router::Client resolve_router(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return router::Client(bind("abstraction.router", "abstraction.router/router@1", guarantees, scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    router::Client resolve_router(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return router::Client(bind("abstraction.router", "abstraction.router/router@1", guarantees, scope, deadline).endpoint, deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    JobsClient resolve_jobs(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return facade::resolve_jobs(resolver_, std::move(guarantees), scope);
    }
    JobsClient resolve_jobs(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return facade::resolve_jobs(resolver_, std::move(guarantees), scope, deadline);
    }
    JobsClient resolve_job_operations(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return facade::resolve_job_operations(resolver_, std::move(guarantees), scope);
    }
    JobsClient resolve_job_operations(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return facade::resolve_job_operations(resolver_, std::move(guarantees), scope, deadline);
    }
    JobInventoryClient resolve_job_inventory(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return facade::resolve_job_inventory(resolver_, std::move(guarantees), scope);
    }
    JobInventoryClient resolve_job_inventory(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return facade::resolve_job_inventory(resolver_, std::move(guarantees), scope, deadline);
    }

    model::Client resolve_model(std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) const {
        return model::Client(bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope).endpoint).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }
    model::Client resolve_model(std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) const {
        return model::Client(bind("abstraction.model", "abstraction.model/resolver@1", guarantees, scope, deadline).endpoint, deadline).with_cancellation(resolver_.cancellation()).with_server_expectation(resolver_.server());
    }

private:
    ServiceReference bind(const std::string& capability, const std::string& contract,
                          const std::vector<std::string>& guarantees, Scope scope) const {
        ResolveRequest request;
        request.capability = capability;
        request.contracts = {contract};
        request.guarantees = guarantees;
        request.scope = scope;
        return resolver_.bind_local(request);
    }
    ServiceReference bind(const std::string& capability, const std::string& contract,
                          const std::vector<std::string>& guarantees, Scope scope,
                          ipc::Deadline deadline) const {
        ResolveRequest request;
        request.capability = capability;
        request.contracts = {contract};
        request.guarantees = guarantees;
        request.scope = scope;
        return resolver_.bind_local(request, deadline);
    }
    ResolutionClient resolver_;
};
inline Machine discover() { return Machine{}; }
}
