#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/rights/client.hpp>
namespace abstraction::facade {
inline rights::Client ResolveRights(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.rights";request.contracts={"abstraction.rights/authorization@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request,deadline));return rights::Client(ref.endpoint,deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline rights::Client ResolveRights(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any"){return ResolveRights(resolver,std::move(guarantees),std::move(scope),ipc::Clock::now()+std::chrono::seconds(5));}
// The receiver must explicitly designate this executable as an enforcer.
inline rights::TrustedEnforcerClient ResolveTrustedRightsEnforcer(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any") {return rights::TrustedEnforcerClient(ResolveRights(resolver,std::move(guarantees),std::move(scope)));}
inline rights::TrustedEnforcerClient ResolveTrustedRightsEnforcer(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {return rights::TrustedEnforcerClient(ResolveRights(resolver,std::move(guarantees),std::move(scope),deadline));}
}
