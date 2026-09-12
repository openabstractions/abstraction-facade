#include <abstraction/facade/jobs.hpp>
#include <type_traits>
#include <iostream>
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
 if(argc!=1)throw std::runtime_error("usage: facade_jobs_consumer [--default-runtime-endpoint | --runtime endpoint --jobs caller-key]");
 f::JobsClient client("unused");
 try{client.Reconcile({});return 1;}catch(const f::job_api::ServiceError&e){if(e.code!="invalid_submission")return 2;}
 f::ResolveRequest q;q.capability="abstraction.job";q.contracts={"abstraction.job/acceptance@1"};q.scope="any";
 f::ResolveResult r;r.status="unavailable";
 try{f::local_binding(q,r);return 3;}catch(const f::ResolutionError&e){if(e.status!="unavailable")return 4;}
 return 0;
 }catch(const std::exception&e){std::cerr<<e.what()<<"\n";return 7;}
}
