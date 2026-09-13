// Uses the private named-pipe fixtures from main.cpp; no production listener.
struct EditorFixture : abstraction::config::ConfigEditor {
    abstraction::config::UserSnapshot current;
    unsigned reads=0,writes=0;
    EditorFixture(){current.revision="revision-1";current.values.log_sink="original";}
    abstraction::config::UserSnapshot ReadUser() override {++reads;return current;}
    abstraction::config::UserReplaceResult ReplaceUser(const std::string& revision,const abstraction::config::UserSettings& values) override {
        ++writes;abstraction::config::UserReplaceResult result;
        result.outcome=revision==current.revision?"applied":"conflict";
        if(result.outcome=="applied"){current.values=values;current.revision="revision-2";}
        result.snapshot=current;return result;
    }
};
void config_editor() {
    namespace c=abstraction::config;namespace ipc=abstraction::ipc;
    EditorFixture handler;c::ConfigEditorDispatcher dispatcher(handler);
    SingleFrame selected([&](const std::string& frame){return dispatcher.ExchangeFrame(frame);},5);
    ResolverFixture catalogue;catalogue.capability="abstraction.config";
    catalogue.contract="abstraction.config/editor@1";catalogue.endpoint=selected.endpoint;catalogue.defaults=true;
    f::ResolverDispatcher resolver(catalogue);
    SingleFrame bootstrap([&](const std::string& frame){return resolver.ExchangeFrame(frame);});
    ipc::CancellationSource source;
    auto editor=f::Machine(bootstrap.endpoint).WithCancellation(source.Token()).ResolveConfigEditor();
    auto original=editor.ReadUser();require(original.revision=="revision-1");
    auto values=original.values;values.log_sink="selected editor";values.off["feature"]="disabled";
    auto applied=editor.ReplaceUser(original.revision,values);
    require(applied.outcome=="applied"&&applied.snapshot.revision=="revision-2");
    values.log_sink="stale overwrite";
    auto conflict=editor.ReplaceUser(original.revision,values);
    require(conflict.outcome=="conflict"&&conflict.snapshot.values.log_sink=="selected editor");
    require(editor.ReadUser().values.off.at("feature")=="disabled");
    const auto expired=ipc::Clock::now()-std::chrono::seconds(1);
    timeout([&]{editor.WithDeadline(expired).ReadUser();});
    const auto future=ipc::Clock::now()+std::chrono::seconds(5);
    ipc::CancellationSource cancelled;cancelled.Cancel();
    expect_cancelled([&]{editor.WithDeadline(future).WithCancellation(cancelled.Token()).ReadUser();});
    expect_cancelled([&]{editor.WithCancellation(cancelled.Token()).WithDeadline(future).ReadUser();});
    source.Cancel();expect_cancelled([&]{editor.ReplaceUser("revision-2",values);});
    // Ordinary explicitly bound clients remain reusable after another scope ends.
    require(c::Editor(selected.endpoint).ReadUser().revision=="revision-2");
    bootstrap.finish();selected.finish();require(handler.reads==3&&handler.writes==2);
    expect_cancelled([&]{f::Machine("unused").WithCancellation(cancelled.Token()).ResolveConfigEditor();});
    timeout([&]{f::Machine("unused").ResolveConfigEditor({},"any",expired);});
    SingleFrame absent([](const std::string& frame){auto v=f::service_payload(frame);f::OAResolverResolveResult r;r.value.status="unavailable";std::string raw;f::enc_oaresolverresolveresult(raw,r,1);return f::service_reply(v,raw,nullptr);});
    refusal("unavailable",[&]{f::Machine(absent.endpoint).ResolveConfigEditor();});absent.finish();
}
