#include <abstraction/ipc/client.h>
#include <cstring>
#include <string>
#include <stdexcept>
#include <chrono>
#include <thread>
static int opens, selections, releases, sessions;
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
// XPC uses one request-scoped session call. Feed it the same checked fixture
// response as the stream path while keeping each call's server expectation.
static uint32_t features(){return OA_IPC_FEATURE_XPC;}
static oa_ipc_status fixture_session_call(const char* endpoint,size_t length,uint32_t ms,oa_ipc_cancellation* token,const oa_ipc_server_expectation* server,const void*,size_t frame_length,uint32_t,uint32_t,oa_ipc_reply** reply,size_t* sent){
 ++sessions;
 *reply=nullptr;*sent=0;
 oa_ipc_connection* connection=nullptr;
 const auto status=verified(endpoint,length,ms,token,server,&connection);
 if(status!=OA_IPC_OK)return status;
 auto* fake=reinterpret_cast<Fake*>(connection);
 fake->reply.erase(0,4);
 *reply=reinterpret_cast<oa_ipc_reply*>(fake);*sent=frame_length;
 return OA_IPC_OK;
}
static const unsigned char* reply_data(const oa_ipc_reply* reply,size_t* length){
 const auto* fake=reinterpret_cast<const Fake*>(reply);*length=fake->reply.size();
 return reinterpret_cast<const unsigned char*>(fake->reply.data());
}
static void reply_release(oa_ipc_reply* reply){delete reinterpret_cast<Fake*>(reply);}
#define oa_ipc_select_runtime selected
#define oa_ipc_selected_server selected_server
#define oa_ipc_runtime_selection_release released
#define oa_ipc_open_verified verified
#define oa_ipc_open opened
#define oa_ipc_open_cancelable cancelled_open
#define oa_ipc_write wrote
#define oa_ipc_read read
#define oa_ipc_close closed
#define oa_ipc_features features
#define oa_ipc_session_call fixture_session_call
#define oa_ipc_reply_data reply_data
#define oa_ipc_reply_release reply_release
#include <abstraction/facade/client.hpp>
#undef oa_ipc_open_verified
#undef oa_ipc_open
#undef oa_ipc_open_cancelable
#undef oa_ipc_write
#undef oa_ipc_read
#undef oa_ipc_close
#undef oa_ipc_features
#undef oa_ipc_session_call
#undef oa_ipc_reply_data
#undef oa_ipc_reply_release
using namespace abstraction;
template<class F>void refused(F f){int before=opens;try{f();}catch(const ipc::FrameError&e){if(e.status==ipc::Status::Untrusted&&opens==before+1)return;throw;}throw std::runtime_error("guard lost");}
// On a platform the runtime declares unsupported (Android), a default client
// refuses before it selects an installation; platform_refusal.cpp covers the
// error itself. Explicit endpoints with explicit server evidence stay usable.
static void unsupported_platform_trust(){
 default_endpoint=true;
 const auto request=facade::service_request<facade::ResolverService>();
 try{facade::resolve_service<facade::ResolverService>(facade::ResolutionClient{},{},abstraction::facade::Scope::Any,ipc::Clock::now()+std::chrono::milliseconds(200));throw std::runtime_error("default client resolved on an unsupported platform");}
 catch(const facade::ResolutionError&e){if(e.status!=facade::kRuntimeUnavailable||e.platform!=facade::unsupported_platform()||selections!=0||opens!=0)throw;}
 facade::ResolutionClient{}.with_server_expectation(ipc::ServerExpectation{2,"123","/installed/runtime"}).resolve(request);
 if(selections!=0||opens!=1)throw std::runtime_error("explicit server evidence selected an installation");
}
int main(){
 if(!facade::unsupported_platform().empty()){unsupported_platform_trust();}else{
 default_endpoint=true;
 const auto request=facade::service_request<facade::ResolverService>();
 facade::ResolutionClient automatic;
 auto deadline0=ipc::Clock::now()+std::chrono::milliseconds(200);
 auto automatic_binding=facade::resolve_service<facade::ResolverService>(automatic,{},abstraction::facade::Scope::Any,deadline0);
 if(selections!=1||releases!=1||last_open_budget>180)throw std::runtime_error("selection reset budget");
 refused([&]{automatic_binding->resolve(request);});
 automatic.resolve(request);if(selections!=1)throw std::runtime_error("selection pin changed");
 facade::Machine machine;
 const auto machine_selections=selections;
 response_capability="abstraction.logging";response_contract="abstraction.logging/sink@1";
 auto logger=machine.resolve_log();refused([&]{logger.log(1,"private");});
 response_capability="abstraction.config";response_contract="abstraction.config/reader@1";
 auto configuration=machine.resolve_config();refused([&]{configuration.read_with_overrides({});});
 response_capability="abstraction.job";response_contract="abstraction.job/acceptance@1";
 auto jobs=machine.resolve_jobs();refused([&]{jobs.get_history_window();});
 if(selections!=machine_selections+1)throw std::runtime_error("default Machine lost pinned selection");
 response_capability="abstraction.facade";response_contract="abstraction.facade/resolver@1";
 int before=opens;
 try{facade::ResolutionClient{}.resolve(request,ipc::Clock::now());throw std::runtime_error("expired selection admitted");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::Timeout||opens!=before)throw;}
 selection_status=OA_IPC_UNTRUSTED;
 try{facade::ResolutionClient{}.resolve(request);throw std::runtime_error("missing installation admitted");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::Untrusted||opens!=before)throw;}
 selection_status=OA_IPC_CANCELLED;
 ipc::CancellationSource cancellation;
 try{facade::ResolutionClient{}.with_cancellation(cancellation.token()).resolve(request);throw std::runtime_error("cancellation ignored");}
 catch(const ipc::FrameError&e){if(e.status!=ipc::Status::Cancelled||!selected_token||opens!=before)throw;}
 // Explicit independent evidence remains usable despite unavailable installation.
 facade::ResolutionClient{}.with_server_expectation(ipc::ServerExpectation{2,"123","/installed/runtime"}).resolve(request);
 }
#ifdef __APPLE__
 if(sessions==0)throw std::runtime_error("installed XPC calls escaped the session fixture");
#endif
 selection_status=OA_IPC_OK;default_endpoint=false;opens=0;
 const ipc::ServerExpectation s{2,"123","/installed/runtime"};
 facade::ResolutionClient resolver("resolver");resolver=resolver.with_server_expectation(s);
 auto bound=facade::resolve_service<facade::ResolverService>(resolver);
 const auto q=facade::service_request<facade::ResolverService>();refused([&]{bound->resolve(q);});if(opens!=2)return 1;
 auto deadline=ipc::Clock::now()+std::chrono::seconds(1);
 refused([&]{logging::Logger("provider").with_server_expectation(s).log(1,"private");});
 refused([&]{config::Client("provider").with_server_expectation(s).read_with_overrides({});});
 refused([&]{config::Editor("provider").with_server_expectation(s).read_user();});
 refused([&]{router::Client("provider").with_server_expectation(s).models();});
 refused([&]{facade::JobsClient("provider").with_server_expectation(s).with_deadline(deadline).get_history_window();});
 refused([&]{facade::JobInventoryClient("provider").with_server_expectation(s).with_deadline(deadline).list_work("",1);});
}
