#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/logging/client.hpp>
namespace abstraction::facade {
inline logging::Logger ResolveLog(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any") {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/sink@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request));return logging::Logger(ref.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline logging::Logger ResolveLog(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/sink@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request,deadline));return logging::Logger(ref.endpoint,deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline logging::History ResolveLogReader(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any") {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/reader@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request));return logging::History(ref.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline logging::History ResolveLogReader(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/reader@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request,deadline));return logging::History(ref.endpoint,deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline logging::Observer ResolveLogObserver(const ResolutionClient& resolver,std::vector<std::string> guarantees={},std::string scope="any") {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/observer@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request));return logging::Observer(ref.endpoint).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
inline logging::Observer ResolveLogObserver(const ResolutionClient& resolver,std::vector<std::string> guarantees,std::string scope,ipc::Deadline deadline) {
 ResolveRequest request;request.capability="abstraction.logging";request.contracts={"abstraction.logging/observer@1"};request.guarantees=std::move(guarantees);request.scope=std::move(scope);
 const auto ref=local_binding(request,resolver.Resolve(request,deadline));return logging::Observer(ref.endpoint,deadline).WithCancellation(resolver.Cancellation()).WithServerExpectation(resolver.Server());
}
}
