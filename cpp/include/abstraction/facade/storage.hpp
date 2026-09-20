#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/storage/client.hpp>
namespace abstraction::facade {
// Resolution selects one endpoint. Resource lifetime and digest verification
// remain the content contract's responsibilities; no provider is activated here.
inline storage::Client resolve_storage(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-reader@1"};
    request.guarantees = std::move(guarantees);
    request.scope = scope;
    const auto binding = resolver.bind_local(request, deadline);
    return storage::Client(binding.endpoint, deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
// Resolution has a five-second budget; each later call receives a fresh wait,
// so reading many chunks is not bounded by the resolution deadline.
inline storage::Client resolve_storage(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-reader@1"};
    request.guarantees = std::move(guarantees);
    request.scope = scope;
    const auto binding = resolver.bind_local(request, ipc::Clock::now() + std::chrono::seconds(5));
    return storage::Client(binding.endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
// Resolution has a five-second budget; each later call receives a fresh wait
// extended by its own wait_ms.
inline storage::Changes resolve_storage_changes(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-changes@1"};
    request.guarantees = std::move(guarantees);
    request.scope = scope;
    const auto binding = resolver.bind_local(request, ipc::Clock::now() + std::chrono::seconds(5));
    return storage::Changes(binding.endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
namespace storage_detail {
inline std::string writer_endpoint(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-writer@1"};
    request.guarantees = std::move(guarantees);
    request.scope = scope;
    return resolver.bind_local(request, deadline).endpoint;
}
}
// The explicit deadline bounds resolution and every later writer call.
inline storage::Writer resolve_storage_writer(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, Scope scope, ipc::Deadline deadline) {
    return storage::Writer(storage_detail::writer_endpoint(resolver, std::move(guarantees), scope, deadline), deadline)
        .with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
// Resolution has a five-second budget; each later call receives a fresh wait,
// so a multi-chunk upload is not bounded by the resolution deadline.
inline storage::Writer resolve_storage_writer(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, Scope scope = Scope::Any) {
    return storage::Writer(storage_detail::writer_endpoint(resolver, std::move(guarantees), scope,
                           ipc::Clock::now() + std::chrono::seconds(5)))
        .with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
}
