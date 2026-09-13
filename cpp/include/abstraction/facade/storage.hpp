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
inline storage::Client ResolveStorage(const ResolutionClient& resolver,
        std::vector<std::string> guarantees = {}, std::string scope = "any") {
    return ResolveStorage(resolver, std::move(guarantees), std::move(scope),
                          ipc::Clock::now() + std::chrono::seconds(5));
}
}
