#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/resource/client.hpp>
namespace abstraction::facade {
inline resource::client::TableClient resolve_resource_table(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.resource";request.contracts={"abstraction.resource/table@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return resource::client::TableClient(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline resource::client::TableClient resolve_resource_table(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){return resolve_resource_table(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));}
inline resource::client::LeasesClient resolve_resource_leases(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.resource";request.contracts={"abstraction.resource/leases@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return resource::client::LeasesClient(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline resource::client::LeasesClient resolve_resource_leases(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){return resolve_resource_leases(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));}
}
