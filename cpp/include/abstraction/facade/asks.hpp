#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/asks/client.hpp>
namespace abstraction::facade {
inline asks::Client resolve_asks(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.asks";request.contracts={"abstraction.asks/application@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return asks::Client(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline asks::Client resolve_asks(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){return resolve_asks(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));}
}
