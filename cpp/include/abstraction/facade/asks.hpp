#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/asks/client.hpp>
namespace abstraction::facade {
inline asks::Client ResolveAsks(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.asks";request.contracts={"abstraction.asks/application@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request,deadline));return asks::Client(ref.endpoint,deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline asks::Client ResolveAsks(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any"){return ResolveAsks(resolver,std::move(guarantees),std::move(scope),ipc::Clock::now()+std::chrono::seconds(5));}
}
