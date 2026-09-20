#pragma once
#include <abstraction/facade/resolution.hpp>
#include <abstraction/inference/client.hpp>
#include <abstraction/inference/operator.hpp>
namespace abstraction::facade {
// The runtime picks the host, decides abstraction.inference/complete for the
// bound caller and applies a named credential itself; resolution grants nothing.
inline inference::Chat resolve_inference(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline){
 ResolveRequest request;request.capability="abstraction.inference";request.contracts={"abstraction.inference/chat@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 return inference::Chat(resolver.bind_local(request,deadline).endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline inference::Chat resolve_inference(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){
 return resolve_inference(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));
}
// Hosts, gateway window keys and the inference audit; the runtime decides
// host.manage, key.issue or audit.read on each call.
inline inference::Operator resolve_inference_operator(const ResolutionClient& resolver,std::vector<std::string> guarantees,Scope scope,ipc::Deadline deadline){
 ResolveRequest request;request.capability="abstraction.inference";request.contracts={"abstraction.inference/operator@1"};request.guarantees=std::move(guarantees);request.scope=scope;
 return inference::Operator(resolver.bind_local(request,deadline).endpoint).with_cancellation(resolver.cancellation()).with_server_expectation(resolver.server());
}
inline inference::Operator resolve_inference_operator(const ResolutionClient& resolver,std::vector<std::string> guarantees={},Scope scope=Scope::Any){
 return resolve_inference_operator(resolver,std::move(guarantees),scope,ipc::Clock::now()+std::chrono::seconds(5));
}
}
