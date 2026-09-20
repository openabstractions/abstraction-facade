#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/credentials/client.hpp>
namespace abstraction::facade {
namespace credentials_detail {
inline std::string endpoint(const ResolutionClient& resolver,const char* contract,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline){
 ResolveRequest request;request.capability="abstraction.credentials";request.contracts={contract};request.guarantees=std::move(guarantees);request.scope=scope;
 return resolver.bind_local(request,deadline).endpoint;
}
}
// Every holder call is a rights decision on the bound caller; resolution grants nothing.
inline credentials::Holder resolve_credentials(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline){
 return credentials::Holder(credentials_detail::endpoint(resolver,"abstraction.credentials/holder@1",std::move(guarantees),scope,deadline),deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline credentials::Holder resolve_credentials(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){
 return credentials::Holder(credentials_detail::endpoint(resolver,"abstraction.credentials/holder@1",std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5))).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
// Only a program the host designated as an enforcer receives anything but forbidden.
inline credentials::Applier resolve_credentials_applier(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){
 return credentials::Applier(credentials_detail::endpoint(resolver,"abstraction.credentials/applier@1",std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5))).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
}
