#include <abstraction/ipc/client.h>
#include <cstring>
#include <string>
#include <stdexcept>
#include <chrono>
#include <thread>
static int opens, selections, releases;
static oa_ipc_status selection_status=OA_IPC_OK;
static bool default_endpoint=false;
static uint32_t last_open_budget;
static std::string response_capability="abstraction.facade", response_contract="abstraction.facade/resolver@1";
static oa_ipc_cancellation* selected_token;
static oa_ipc_status selected(uint32_t ms,oa_ipc_cancellation* token,oa_ipc_runtime_selection** out){
 selected_token=token; ++selections;*out=nullptr;if(!ms)return OA_IPC_TIMEOUT;
 if(selection_status!=OA_IPC_OK)return selection_status;
 std::this_thread::sleep_for(std::chrono::milliseconds(25));
 *out=reinterpret_cast<oa_ipc_runtime_selection*>(1);return OA_IPC_OK;
}
static const oa_ipc_server_expectation* selected_server(const oa_ipc_runtime_selection*){
 static const oa_ipc_server_expectation s{sizeof(s),1,2,0,"123",3,"/installed/runtime",18};return &s;
}
static void released(oa_ipc_runtime_selection* s){if(s)++releases;}

struct Fake { std::string reply;size_t offset=0; };
static oa_ipc_status verified(const char* endpoint,size_t length,uint32_t ms,oa_ipc_cancellation*,const oa_ipc_server_expectation* s,oa_ipc_connection** out){
 if(!s||s->version!=1||std::string(s->principal,s->principal_length)!="123"||std::string(s->program,s->program_length)!="/installed/runtime")throw std::runtime_error("lost independent expectation");
 last_open_budget=ms; ++opens;*out=nullptr;if(std::string(endpoint,length)!="resolver" && (!default_endpoint || std::string(endpoint,length)=="provider"))return OA_IPC_UNTRUSTED;
 auto* f=new Fake;std::string body=R"({"version":1,"service":"abstraction.facade/resolver@1","method":"Resolve","ok":true,"payload":{"value":{"status":"resolved","reference":{"provider":"fixture","capability":"abstraction.facade","contract":"abstraction.facade/resolver@1","scope":"local","transport":"oa-framed-local@1","endpoint":"provider","guarantees":[]}}}})";
 const auto cap=body.find("\"capability\":\"abstraction.facade\"");
 body.replace(cap,std::strlen("\"capability\":\"abstraction.facade\""),"\"capability\":\""+response_capability+"\"");
 const auto contract=body.find("\"contract\":\"abstraction.facade/resolver@1\"");
 body.replace(contract,std::strlen("\"contract\":\"abstraction.facade/resolver@1\""),"\"contract\":\""+response_contract+"\"");
 const auto size=body.size();for(int i=3;i>=0;--i)f->reply.push_back(char(size>>(i*8)));f->reply+=body;*out=reinterpret_cast<oa_ipc_connection*>(f);return OA_IPC_OK;
}
static oa_ipc_status opened(const char*,size_t,uint32_t,oa_ipc_connection**){throw std::runtime_error("unverified fallback");}
static oa_ipc_status cancelled_open(const char*,size_t,uint32_t,oa_ipc_cancellation*,oa_ipc_connection**){throw std::runtime_error("cancelable unverified fallback");}
static oa_ipc_status wrote(oa_ipc_connection*,const void*,size_t n,size_t* moved){*moved=n;return OA_IPC_OK;}
static oa_ipc_status read(oa_ipc_connection* h,void* out,size_t size,size_t* moved){auto& f=*reinterpret_cast<Fake*>(h);*moved=std::min(size,f.reply.size()-f.offset);std::memcpy(out,f.reply.data()+f.offset,*moved);f.offset+=*moved;return *moved?OA_IPC_OK:OA_IPC_DISCONNECTED;}
static void closed(oa_ipc_connection* h){delete reinterpret_cast<Fake*>(h);}
#define oa_ipc_select_runtime selected
#define oa_ipc_selected_server selected_server
#define oa_ipc_runtime_selection_release released
#define oa_ipc_open_verified verified
#define oa_ipc_open opened
#define oa_ipc_open_cancelable cancelled_open
#define oa_ipc_write wrote
#define oa_ipc_read read
#define oa_ipc_close closed
#include <abstraction/facade/client.hpp>
#undef oa_ipc_open_verified
#undef oa_ipc_open
#undef oa_ipc_open_cancelable
#undef oa_ipc_write
#undef oa_ipc_read
#undef oa_ipc_close
using namespace abstraction;
template<class F>void refused(F f){int before=opens;try{f();}catch(const ipc::FrameError&e){if(e.status==ipc::Status::untrusted&&opens==before+1)return;throw;}throw std::runtime_error("guard lost");}
int main(){
 default_endpoint=true;
 const auto request=facade::service_request<facade::ResolverService>();
 facade::ResolutionClient automatic;
 auto deadline0=ipc::Clock::now()+std::chrono::milliseconds(200);
 auto automatic_binding=facade::ResolveService<facade::ResolverService>(automatic,{},"any",deadline0);
 if(selections!=1||releases!=1||last_open_budget>180)throw std::runtime_error("selection reset budget");
 refused([&]{automatic_binding->Resolve(request);});
 automatic.Resolve(request);if(selections!=1)throw std::runtime_error("selection pin changed");
 facade::Machine machine;
 const auto machine_selections=selections;
 response_capability="abstraction.logging";response_contract="abstraction.logging/sink@1";
 auto logger=machine.ResolveLog();refused([&]{logger.Log(1,"private");});
 response_capability="abstraction.config";response_contract="abstraction.config/reader@1";
 auto configuration=machine.ResolveConfig();refused([&]{configuration.ReadWithOverrides({});});
 response_capability="abstraction.job";response_contract="abstraction.job/acceptance@1";
 auto jobs=machine.ResolveJobs();refused([&]{jobs.GetHistoryWindow();});
 if(selections!=machine_selections+1)throw std::runtime_error("default Machine lost pinned selection");
 response_capability="abstraction.facade";response_contract="abstraction.facade/resolver@1";
 int before=opens;
 try{facade::ResolutionClient{}.Resolve(request,ipc::Clock::now());throw std::runtime_error("expired selection admitted");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::timeout||opens!=before)throw;}
 selection_status=OA_IPC_UNTRUSTED;
 try{facade::ResolutionClient{}.Resolve(request);throw std::runtime_error("missing installation admitted");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::untrusted||opens!=before)throw;}
 selection_status=OA_IPC_CANCELLED;
 ipc::CancellationSource cancellation;
 try{facade::ResolutionClient{}.WithCancellation(cancellation.Token()).Resolve(request);throw std::runtime_error("cancellation ignored");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::cancelled||!selected_token||opens!=before)throw;}
 // Explicit independent evidence remains usable despite unavailable installation.
 facade::ResolutionClient{}.WithServerExpectation(ipc::ServerExpectation{2,"123","/installed/runtime"}).Resolve(request);
 selection_status=OA_IPC_OK;default_endpoint=false;opens=0;
 const ipc::ServerExpectation s{2,"123","/installed/runtime"};
 facade::ResolutionClient resolver("resolver");resolver=resolver.WithServerExpectation(s);
 auto bound=facade::ResolveService<facade::ResolverService>(resolver);
 const auto q=facade::service_request<facade::ResolverService>();refused([&]{bound->Resolve(q);});if(opens!=2)return 1;
 auto deadline=ipc::Clock::now()+std::chrono::seconds(1);
 refused([&]{logging::Logger("provider").WithServerExpectation(s).Log(1,"private");});
 refused([&]{config::Client("provider").WithServerExpectation(s).ReadWithOverrides({});});
 refused([&]{config::Editor("provider").WithServerExpectation(s).ReadUser();});
 refused([&]{router::Client("provider").WithServerExpectation(s).Models();});
 refused([&]{facade::JobsClient("provider").WithServerExpectation(s).WithDeadline(deadline).GetHistoryWindow();});
 refused([&]{facade::JobInventoryClient("provider").WithServerExpectation(s).WithDeadline(deadline).ListWork("",1);});
}
