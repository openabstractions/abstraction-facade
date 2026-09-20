#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/logging/client.hpp>
namespace abstraction::facade {
inline logging::Logger resolve_log(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/sink@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request);return logging::Logger(ref.endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline logging::Logger resolve_log(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/sink@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return logging::Logger(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline logging::History resolve_log_reader(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/reader@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request);return logging::History(ref.endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline logging::History resolve_log_reader(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/reader@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return logging::History(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline logging::Observer resolve_log_observer(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/observer@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request);return logging::Observer(ref.endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline logging::Observer resolve_log_observer(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/observer@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 const auto ref=resolver.bind_local(request,deadline);return logging::Observer(ref.endpoint,deadline).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
}
