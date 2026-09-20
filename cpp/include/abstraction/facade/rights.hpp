#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/rights/client.hpp>
namespace abstraction::facade {
inline rights::Client resolve_rights(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.rights";request.contracts={"abstraction.rights/authorization@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return rights::Client(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline rights::Client resolve_rights(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){return resolve_rights(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));}
// The receiver must explicitly designate this executable as an enforcer.
inline rights::TrustedEnforcerClient resolve_trusted_rights_enforcer(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any) {return rights::TrustedEnforcerClient(resolve_rights(resolver,std::move(guarantees),scope));}
inline rights::TrustedEnforcerClient resolve_trusted_rights_enforcer(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {return rights::TrustedEnforcerClient(resolve_rights(resolver,std::move(guarantees),scope,deadline));}
}
