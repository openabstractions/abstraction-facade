#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/storage/client.hpp>
namespace abstraction::facade {
// Resolution selects one endpoint. Resource lifetime and digest verification
// remain the content contract's responsibilities; no provider is activated here.
inline storage::Client ResolveStorage(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-reader@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    const auto binding = local_binding(request, resolver.Resolve(request, deadline));
    return storage::Client(binding.endpoint, deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
// Resolution has a five-second budget; each later call receives a fresh wait,
// so reading many chunks is not bounded by the resolution deadline.
inline storage::Client ResolveStorage(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, std::string scope = "any") {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-reader@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    const auto binding = local_binding(request, resolver.Resolve(request, ipc::Clock::now() + std::chrono::seconds(5)));
    return storage::Client(binding.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
// Resolution has a five-second budget; each later call receives a fresh wait
// extended by its own wait_ms.
inline storage::Changes ResolveStorageChanges(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, std::string scope = "any") {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-changes@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    const auto binding = local_binding(request, resolver.Resolve(request, ipc::Clock::now() + std::chrono::seconds(5)));
    return storage::Changes(binding.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
namespace storage_detail {
inline std::string writer_endpoint(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    ResolveRequest request;
    request.capability = "abstraction.storage";
    request.contracts = {"abstraction.storage/content-writer@1"};
    request.guarantees = std::move(guarantees);
    request.scope = std::move(scope);
    return local_binding(request, resolver.Resolve(request, deadline)).endpoint;
}
}
// The explicit deadline bounds resolution and every later writer call.
inline storage::Writer ResolveStorageWriter(const ResolutionClient& resolver,
        std::vector<std::string> guarantees, std::string scope, ipc::Deadline deadline) {
    return storage::Writer(storage_detail::writer_endpoint(resolver, std::move(guarantees), std::move(scope), deadline), deadline)
        .WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
// Resolution has a five-second budget; each later call receives a fresh wait,
// so a multi-chunk upload is not bounded by the resolution deadline.
inline storage::Writer ResolveStorageWriter(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, std::string scope = "any") {
    return storage::Writer(storage_detail::writer_endpoint(resolver, std::move(guarantees), std::move(scope),
                           ipc::Clock::now() + std::chrono::seconds(5)))
        .WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
}
