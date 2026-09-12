#include <abstraction/facade/client.hpp>
#include <functional>
#include <iostream>
#ifdef _WIN32
#define NOMINMAX
#include <windows.h>
#include <thread>
#include <atomic>
#endif
namespace f = abstraction::facade;
void require(bool value) { if (!value) throw std::runtime_error("test assertion failed"); }
f::ResolveRequest request() {
    f::ResolveRequest q; q.capability="abstraction.logging";
    q.contracts={"abstraction.logging/sink@1"}; q.guarantees={"event-write@1"};q.scope="any";return q;
}
f::ResolveResult resolved(const f::ResolveRequest& q, const std::string& endpoint="selected") {
    f::ResolveResult r;r.status="resolved";
    f::ServiceReference ref;ref.provider="policy-first";ref.capability=q.capability;
    ref.contract=q.contracts[0];ref.guarantees=q.guarantees;ref.scope="local";
    ref.transport="oa-framed-local@1";ref.endpoint=endpoint;r.reference=ref;return r;
}
template<class Fn> void refusal(const std::string& status, Fn fn) {
    try { fn(); } catch (const f::ResolutionError& e) {require(e.status==status);return;}
    throw std::runtime_error("expected resolution refusal: "+status);
}
void semantics() {
    auto q=request();auto good=resolved(q);
    require(f::local_binding(q,good).endpoint=="selected");
    for (const auto& status : f::kResolutionStatusNames) {
        if(status=="resolved")continue;
        f::ResolveResult denied;denied.status=status;
        refusal(status,[&]{f::local_binding(q,denied);});
        denied.reference=good.reference;
        refusal("invalid_resolution",[&]{f::local_binding(q,denied);});
    }
    const std::vector<std::function<void(f::ResolveResult&)>> corrupt={
      [](auto&r){r.reference.reset();},[](auto&r){r.status="invented";},
      [](auto&r){r.reference->provider="";},[](auto&r){r.reference->capability="other";},
      [](auto&r){r.reference->contract="abstraction.logging/sink@2";},
      [](auto&r){r.reference->guarantees.clear();},[](auto&r){r.reference->guarantees.push_back("event-write@1");},
      [](auto&r){r.reference->endpoint="";},[](auto&r){r.reference->endpoint=std::string("a\0b",3);},
      [](auto&r){r.reference->scope="any";}
    };
    for(const auto& edit:corrupt){auto bad=good;edit(bad);refusal("invalid_resolution",[&]{f::local_binding(q,bad);});}
    auto bad=good;bad.reference->transport="https";refusal("unsupported_transport",[&]{f::local_binding(q,bad);});
    bad=good;bad.reference->scope="remote";refusal("unsupported_transport",[&]{f::local_binding(q,bad);});
    q.scope="local";refusal("invalid_resolution",[&]{f::local_binding(q,bad);});
    q=request();q.contracts.push_back(q.contracts[0]);refusal("invalid_request",[&]{f::validate_resolve_request(q);});
}
#ifdef _WIN32
// Private single-frame named-pipe fixture. No production C++ listener is supplied.
class SingleFrame {
    HANDLE pipe_;std::thread worker_;std::exception_ptr error_;
    static void io(HANDLE p, void* bytes, DWORD count, bool write) {
        auto* b=static_cast<char*>(bytes);
        while(count){DWORD n=0;BOOL ok=write?WriteFile(p,b,count,&n,nullptr):ReadFile(p,b,count,&n,nullptr);
          if(!ok||!n)throw std::runtime_error("fixture pipe I/O failed");b+=n;count-=n;}
    }
public:
    std::string endpoint;
    explicit SingleFrame(std::function<std::string(const std::string&)> fn, unsigned exchanges=1) {
        static std::atomic<unsigned> serial{0};
        endpoint=R"(\\.\pipe\oa-resolution-binding-)"+std::to_string(GetCurrentProcessId())+"-"+std::to_string(++serial);
        pipe_=CreateNamedPipeA(endpoint.c_str(),PIPE_ACCESS_DUPLEX,PIPE_TYPE_BYTE|PIPE_WAIT,1,1<<20,1<<20,0,nullptr);
        require(pipe_!=INVALID_HANDLE_VALUE);
        worker_=std::thread([this,fn,exchanges]{try{for(unsigned exchange=0;exchange<exchanges;++exchange){
          if(!ConnectNamedPipe(pipe_,nullptr)&&GetLastError()!=ERROR_PIPE_CONNECTED)throw std::runtime_error("connect fixture");
          unsigned char h[4];io(pipe_,h,4,false);
          auto n=(unsigned(h[0])<<24)|(unsigned(h[1])<<16)|(unsigned(h[2])<<8)|h[3];require(n<=f::JobsClient::MaxFrameBytes);
          std::string frame(n,'\0');io(pipe_,frame.data(),n,false);auto reply=fn(frame);
          if(!reply.empty()){n=static_cast<unsigned>(reply.size());unsigned char rh[4]={static_cast<unsigned char>(n>>24),static_cast<unsigned char>(n>>16),static_cast<unsigned char>(n>>8),static_cast<unsigned char>(n)};io(pipe_,rh,4,true);io(pipe_,reply.data(),n,true);FlushFileBuffers(pipe_);}
          DisconnectNamedPipe(pipe_);
        }}catch(...){error_=std::current_exception();DisconnectNamedPipe(pipe_);}});
    }
    ~SingleFrame(){if(worker_.joinable()){CancelSynchronousIo(worker_.native_handle());worker_.join();}CloseHandle(pipe_);}
    void finish(){worker_.join();if(error_)std::rethrow_exception(error_);}
};
struct ResolverFixture:f::Resolver {
    std::string capability,contract,endpoint;
    f::ResolveResult Resolve(const f::ResolveRequest& q)override {
        require(q.capability==capability&&q.contracts==std::vector<std::string>{contract});
        require(q.guarantees==std::vector<std::string>{"required@1"}&&q.scope=="local");
        return resolved(q,endpoint);
    }
};
struct JobFixture:f::job_api::RecoverableAcceptance {
 f::job_api::HistoryWindow GetHistoryWindow()override{f::job_api::HistoryWindow h;h.logical_owner="owner";h.history_epoch="epoch";h.minimum_retention_ms=60000;return h;}
 f::job_api::AcceptanceResult Submit(const f::job_api::Submission& s)override{
   require(s.identity.key=="explicit-key"&&s.spec.size()>750000);
   require(f::resolution_detail::contains(s.required_guarantees,"required@1"));
   f::job_api::AcceptanceResult r;r.outcome="accepted";f::job_api::Receipt receipt;
   receipt.identity=s.identity;receipt.logical_owner="owner";receipt.operation_id="original-op";
   receipt.accepted_guarantees=s.required_guarantees;receipt.history_retention_ms=60000;r.receipt=receipt;return r;
 }
 f::job_api::AcceptanceResult Reconcile(const f::job_api::RequestIdentity&)override{f::job_api::AcceptanceResult r;r.outcome="unknown";return r;}
 f::job_api::CancellationResult CancelWork(const f::job_api::RequestIdentity&)override{f::job_api::CancellationResult r;r.outcome="requested";return r;}
};
void weak_job_receipts() {
 for(bool reconcile:{false,true}) {
  SingleFrame selected([&](const std::string& frame){
   auto v=f::job_api::service_payload(frame);f::job_api::AcceptanceResult result;result.outcome="accepted";
   f::job_api::Receipt r;r.identity.key="key";r.identity.history_epoch="epoch";r.logical_owner="owner";r.operation_id="op";r.history_retention_ms=60000;result.receipt=r;
   std::string raw;
   if(reconcile){f::job_api::OARecoverableAcceptanceReconcileResult p;p.value=result;f::job_api::enc_oarecoverableacceptancereconcileresult(raw,p,1);}
   else {f::job_api::OARecoverableAcceptanceSubmitResult p;p.value=result;f::job_api::enc_oarecoverableacceptancesubmitresult(raw,p,1);}
   return f::job_api::service_reply(v,raw,nullptr);
  });
  ResolverFixture catalogue;catalogue.capability="abstraction.job";catalogue.contract="abstraction.job/acceptance@1";catalogue.endpoint=selected.endpoint;
  f::ResolverDispatcher dispatcher(catalogue);SingleFrame bootstrap([&](const std::string& frame){return dispatcher.ExchangeFrame(frame);});
  auto jobs=f::Machine(bootstrap.endpoint).ResolveJobs({"required@1"},"local");
  bool rejected=false;
  try {if(reconcile){f::job_api::RequestIdentity id;id.key="key";id.history_epoch="epoch";jobs.Reconcile(id);}
    else {f::job_api::Submission value;value.identity.key="key";value.identity.history_epoch="epoch";value.kind="test";jobs.Submit(value);}}
  catch(const f::job_api::ServiceError&e){rejected=e.code=="invalid_acceptance";}
  bootstrap.finish();selected.finish();
  if(!rejected)throw std::runtime_error(reconcile?"Reconcile accepted weaker binding receipt":"Submit accepted weaker binding receipt");
 }
}
void job_owner_consistency() {
 for(bool recovered:{false,true}) {
  SingleFrame selected([&](const std::string& frame){
   auto v=f::job_api::service_payload(frame);std::string raw;
   if(v.method=="GetHistoryWindow") {f::job_api::OARecoverableAcceptanceGetHistoryWindowResult p;p.value.logical_owner="original-owner";p.value.history_epoch="epoch";p.value.minimum_retention_ms=60000;f::job_api::enc_oarecoverableacceptancegethistorywindowresult(raw,p,1);}
   else {f::job_api::OARecoverableAcceptanceReconcileResult p;p.value.outcome="accepted";f::job_api::Receipt r;r.identity.key="key";r.identity.history_epoch="epoch";r.logical_owner="different-owner";r.operation_id="op";r.history_retention_ms=60000;p.value.receipt=r;f::job_api::enc_oarecoverableacceptancereconcileresult(raw,p,1);}
   return f::job_api::service_reply(v,raw,nullptr);
  },recovered?1:2);
  f::JobsClient jobs(selected.endpoint,5000,{},recovered?"original-owner":"");
  auto copied=jobs;
  if(!recovered)jobs.GetHistoryWindow();
  bool rejected=false;f::job_api::RequestIdentity id;id.key="key";id.history_epoch="epoch";
  try{copied.Reconcile(id);}catch(const f::job_api::ServiceError&e){rejected=e.code=="invalid_acceptance";}
  selected.finish();if(!rejected)throw std::runtime_error("accepted changed logical owner");
 }
}
void job_binding() {
 static_assert(f::JobsClient::MaxFrameBytes==2097152,"job provider frame budget changed");
 JobFixture provider;f::job_api::RecoverableAcceptanceDispatcher dispatcher(provider);
 SingleFrame selected([&](const std::string& frame){require(frame.size()>1048576);return dispatcher.ExchangeFrame(frame);});
 ResolverFixture catalogue;catalogue.capability="abstraction.job";catalogue.contract="abstraction.job/acceptance@1";catalogue.endpoint=selected.endpoint;
 f::ResolverDispatcher resolver(catalogue);
 SingleFrame bootstrap([&](const std::string& frame){return resolver.ExchangeFrame(frame);});
 auto client=f::Machine(bootstrap.endpoint).ResolveJobs({"required@1"},"local");
 f::job_api::Submission submission;submission.identity.key="explicit-key";submission.identity.history_epoch="epoch";
 submission.kind="download";submission.spec.assign(800000,'x');submission.required_guarantees={};
 auto result=client.Submit(submission);require(result.receipt&&result.receipt->operation_id=="original-op");require(submission.required_guarantees.empty());
 bootstrap.finish();selected.finish();
 for(const auto& status:std::vector<std::string>{"forbidden","unavailable","remote","wrong-transport"}) {
  SingleFrame service([&](const std::string& frame){auto v=f::service_payload(frame);f::OAResolverResolveResult payload;
    f::ResolveRequest q;q.capability="abstraction.job";q.contracts={"abstraction.job/acceptance@1"};q.scope="any";
    if(status=="remote"||status=="wrong-transport"){payload.value=resolved(q);if(status=="remote")payload.value.reference->scope="remote";else payload.value.reference->transport="https";}
    else payload.value.status=status;
    std::string raw;f::enc_oaresolverresolveresult(raw,payload,1);return f::service_reply(v,raw,nullptr);});
  refusal(status=="remote"||status=="wrong-transport"?"unsupported_transport":status,[&]{f::Machine(service.endpoint).ResolveJobs();});service.finish();
 }
}
void selected_endpoints() {
    for(const std::string cap:{"logging","config","router"}) {
        std::string method;
        SingleFrame selected([&](const std::string& frame){
          // Decode through the capability's own generated dispatcher/client format.
          if(cap=="logging") {
            auto v=abstraction::logging::service_payload(frame);require(v.service=="abstraction.logging/sink@1"&&v.method=="Write");
            method=v.method;return std::string{};
          }
          if(cap=="config") {
            auto v=abstraction::config::service_payload(frame);require(v.service=="abstraction.config/reader@1"&&v.method=="Read");method=v.method;
            abstraction::config::OAConfigReaderReadResult payload;payload.value.stamp="selected-config";
            std::string raw;abstraction::config::enc_oaconfigreaderreadresult(raw,payload,1);
            return abstraction::config::service_reply(v,raw,nullptr);
          }
          auto v=abstraction::router::service_payload(frame);require(v.service=="abstraction.router/router@1"&&v.method=="Hosts");method=v.method;
          abstraction::router::OARouterHostsResult payload;payload.value.doubled={"selected-router"};
          std::string raw;abstraction::router::enc_oarouterhostsresult(raw,payload,1);
          return abstraction::router::service_reply(v,raw,nullptr);
        });
        ResolverFixture provider;provider.capability="abstraction."+cap;provider.endpoint=selected.endpoint;
        provider.contract=provider.capability+(cap=="logging"?"/sink@1":cap=="config"?"/reader@1":"/router@1");
        f::ResolverDispatcher dispatcher(provider);
        SingleFrame bootstrap([&](const std::string& frame){return dispatcher.ExchangeFrame(frame);});
        f::Machine machine(bootstrap.endpoint);
        if(cap=="logging")machine.ResolveLog({"required@1"},"local").Log(1,"selected logging");
        if(cap=="config")require(machine.ResolveConfig({"required@1"},"local").Read().stamp=="selected-config");
        if(cap=="router")require(machine.ResolveRouter({"required@1"},"local").Hosts().doubled==std::vector<std::string>{"selected-router"});
        bootstrap.finish();selected.finish();require(!method.empty());
    }
    for(const std::string status:{"forbidden","not_ready","unavailable"}) {
      SingleFrame bootstrap([&](const std::string& frame){auto v=f::service_payload(frame);f::OAResolverResolveResult payload;payload.value.status=status;std::string raw;f::enc_oaresolverresolveresult(raw,payload,1);return f::service_reply(v,raw,nullptr);});
      refusal(status,[&]{f::Machine(bootstrap.endpoint).ResolveLog();});bootstrap.finish();
    }
    SingleFrame forged([](const std::string& frame){auto v=f::service_payload(frame);f::OAResolverResolveResult payload;payload.value=resolved(request());payload.value.reference->contract="wrong-contract";std::string raw;f::enc_oaresolverresolveresult(raw,payload,1);return f::service_reply(v,raw,nullptr);});
    refusal("invalid_resolution",[&]{f::Machine(forged.endpoint).ResolveLog();});forged.finish();
}
#endif
int main(int argc,char**argv){try{
 if(argc==2&&std::string(argv[1])=="--default-runtime-endpoint") {std::cout<<abstraction::facade::runtime_endpoint()<<"\n";return 0;}
 if(argc==5&&std::string(argv[1])=="--runtime"&&std::string(argv[3])=="--jobs") {
   auto jobs=f::Machine(argv[2]).ResolveJobs({"abstraction.job/reconciliation@1"});
   auto history=jobs.GetHistoryWindow();
   f::job_api::Submission submission;submission.identity.key=argv[4];submission.identity.history_epoch=history.history_epoch;
   submission.kind="download";const std::string spec=R"({"source":"test"})";
   submission.spec.assign(spec.begin(),spec.end());submission.required_guarantees={"abstraction.job/reconciliation@1"};
   const auto accepted=jobs.Submit(submission);require(accepted.outcome=="accepted"&&accepted.receipt&&accepted.receipt->logical_owner==history.logical_owner);
   const auto recovered=jobs.Reconcile(submission.identity);require(recovered.outcome=="accepted"&&recovered.receipt);
   require(recovered.receipt->operation_id==accepted.receipt->operation_id&&recovered.receipt->logical_owner==accepted.receipt->logical_owner);
   require(recovered.receipt->accepted_guarantees==accepted.receipt->accepted_guarantees);
   std::cout<<f::job_api::encode(recovered);return 0;
 }
 if(argc==3&&std::string(argv[1])=="--runtime") {
   f::Machine machine(argv[2]);
   machine.ResolveLog().Log(1,"cpp-resolved-log");
   machine.ResolveConfig().ReadWithOverrides({});
   std::cout<<"C++ runtime resolution passed\n";return 0;
 }
 if(argc!=1)throw std::runtime_error("usage: facade_binding_consumer [--default-runtime-endpoint | --runtime endpoint [--jobs caller-key]]");
 f::Machine legacy = {}; // preserve legacy default initialization
 (void)legacy;
 semantics();
#ifdef _WIN32
 selected_endpoints();
 job_binding();
 weak_job_receipts();
 job_owner_consistency();
#endif
 std::cout<<"resolution binding checks passed\n";return 0;}catch(const std::exception&e){std::cerr<<e.what()<<'\n';return 1;}}
