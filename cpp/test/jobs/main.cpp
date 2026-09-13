#include <abstraction/facade/jobs.hpp>
#include <type_traits>
#include <iostream>
#include <sstream>
#include <thread>
#ifdef OA_DOWNLOAD_REQUEST
#include <abstraction/download/request/rec.h>
#endif
int main(int argc,char**argv){
 try{
 namespace f=abstraction::facade;
 static_assert(std::is_base_of_v<f::job_api::RecoverableAcceptance,f::JobsClient>);
 static_assert(f::JobsClient::MaxFrameBytes==2097152);
 if(argc==2&&std::string(argv[1])=="--default-runtime-endpoint") {std::cout<<abstraction::facade::runtime_endpoint()<<"\n";return 0;}
 if(argc==5&&std::string(argv[1])=="--runtime"&&std::string(argv[3])=="--jobs") {
   auto jobs=f::ResolveJobs(f::ResolutionClient(argv[2]),{"abstraction.job/reconciliation@1"});
   auto history=jobs.GetHistoryWindow();
   f::job_api::Submission submission;submission.identity.key=argv[4];submission.identity.history_epoch=history.history_epoch;
   submission.kind="download";const std::string spec=R"({"source":"test"})";
   submission.spec.assign(spec.begin(),spec.end());submission.required_guarantees={"abstraction.job/reconciliation@1"};
   const auto accepted=jobs.Submit(submission);
   if(accepted.outcome!="accepted"||!accepted.receipt||accepted.receipt->logical_owner!=history.logical_owner)return 5;
   const auto recovered=jobs.Reconcile(submission.identity);
   if(recovered.outcome!="accepted"||!recovered.receipt||recovered.receipt->operation_id!=accepted.receipt->operation_id||
      recovered.receipt->logical_owner!=accepted.receipt->logical_owner||recovered.receipt->accepted_guarantees!=accepted.receipt->accepted_guarantees)return 6;
   std::cout<<f::job_api::encode(recovered);return 0;
 }
#ifdef OA_DOWNLOAD_REQUEST
 if(argc==5&&std::string(argv[1])=="--execute") {
   const auto deadline=abstraction::ipc::Clock::now()+std::chrono::seconds(15);
   auto jobs=f::ResolveJobOperations(f::ResolutionClient(argv[2]),{"abstraction.job/reconciliation@1"},"local",deadline);
   const auto history=jobs.GetHistoryWindow();
   abstraction::download::request::Request request;
   const std::string url=argv[3];
   request.sources.push_back({url.rfind("https:",0)==0?"https":"http",url});
   const auto payload=abstraction::download::request::encode(request);
   f::job_api::Submission submission;submission.identity={argv[4],history.history_epoch};submission.kind="download";
   submission.spec.assign(payload.begin(),payload.end());
   const auto accepted=jobs.Submit(submission);
   if(accepted.outcome!="accepted"||!accepted.receipt)throw std::runtime_error("request not accepted");
   for(;;) {
     auto observed=jobs.ObserveWork(submission.identity);
     if(observed.outcome!="observed"||!observed.snapshot)throw std::runtime_error("operation unobservable");
     const auto& snapshot=*observed.snapshot;
     if(snapshot.receipt.operation_id!=accepted.receipt->operation_id)throw std::runtime_error("operation changed");
     if(snapshot.state=="complete")break;
     if(snapshot.state=="failed"||snapshot.state=="cancelled")throw std::runtime_error("operation did not complete");
     if(abstraction::ipc::Clock::now()>=deadline)throw std::runtime_error("observation budget expired");
     std::this_thread::sleep_for(std::chrono::milliseconds(20));
   }
   std::ostringstream output;
   const auto copy=jobs.CopyResult(submission.identity,output);
   if(copy.error)std::rethrow_exception(copy.error);
   const auto bytes=output.str();
   if(copy.confirmed!=static_cast<std::int64_t>(bytes.size()))throw std::runtime_error("copy count differs");
   static const char digits[]="0123456789abcdef";
   std::cout<<"RESULT ";for(unsigned char byte:bytes)std::cout<<digits[byte>>4]<<digits[byte&15];std::cout<<"\n";
   if(!std::cout)throw std::runtime_error("result output failed");
   return 0;
 }
#endif
 if(argc!=1)throw std::runtime_error("usage: facade_jobs_consumer [--default-runtime-endpoint | --runtime endpoint --jobs caller-key]");
 const auto expired=abstraction::ipc::Clock::now()-std::chrono::seconds(1);
 auto scoped=f::JobsClient("unused").WithDeadline(expired);
 try{scoped.GetHistoryWindow();return 8;}catch(const abstraction::ipc::FrameError&e){if(e.status!=abstraction::ipc::Status::timeout)return 9;}
 try{f::ResolveJobs(f::ResolutionClient("unused"),{},"local",expired);return 10;}catch(const abstraction::ipc::FrameError&e){if(e.status!=abstraction::ipc::Status::timeout)return 11;}
 f::JobsClient client("unused");
 try{client.Reconcile({});return 1;}catch(const f::job_api::ServiceError&e){if(e.code!="invalid_submission")return 2;}
 f::ResolveRequest q;q.capability="abstraction.job";q.contracts={"abstraction.job/acceptance@1"};q.scope="any";
 f::ResolveResult r;r.status="unavailable";
 try{f::local_binding(q,r);return 3;}catch(const f::ResolutionError&e){if(e.status!="unavailable")return 4;}
 return 0;
 }catch(const std::exception&e){std::cerr<<e.what()<<"\n";return 7;}
}
