// Uses the private named-pipe fixtures from main.cpp; no production listener.
struct EditorFixture : abstraction::config::ConfigEditor {
    abstraction::config::UserSnapshot current;
    unsigned reads=0,writes=0;
    EditorFixture(){current.revision="revision-1";current.values.log_sink="original";}
    abstraction::config::UserSnapshot read_user() override {++reads;return current;}
    abstraction::config::UserReplaceResult replace_user(const std::string& revision,const abstraction::config::UserSettings& values) override {
        ++writes;abstraction::config::UserReplaceResult result;
        result.outcome=revision==current.revision?abstraction::config::UserReplaceOutcome::Applied:abstraction::config::UserReplaceOutcome::Conflict;
        if(result.outcome=="applied"){current.values=values;current.revision="revision-2";}
        result.snapshot=current;return result;
    }
};
void config_editor() {
    namespace c=abstraction::config;namespace ipc=abstraction::ipc;
    EditorFixture handler;c::ConfigEditorDispatcher dispatcher(handler);
    SingleFrame selected([&](const std::string& frame){return dispatcher.exchange_frame(frame);},5);
    ResolverFixture catalogue;catalogue.capability="abstraction.config";
    catalogue.contract="abstraction.config/editor@1";catalogue.endpoint=selected.endpoint;catalogue.defaults=true;
    f::ResolverDispatcher resolver(catalogue);
    SingleFrame bootstrap([&](const std::string& frame){return resolver.exchange_frame(frame);});
    ipc::CancellationSource source;
    auto editor=f::Machine(bootstrap.endpoint).with_cancellation(source.token()).resolve_config_editor();
    auto original=editor.read_user();require(original.revision=="revision-1");
    auto values=original.values;values.log_sink="selected editor";values.off["feature"]="disabled";
    auto applied=editor.replace_user(original.revision,values);
    require(applied.outcome=="applied"&&applied.snapshot.revision=="revision-2");
    values.log_sink="stale overwrite";
    auto conflict=editor.replace_user(original.revision,values);
    require(conflict.outcome=="conflict"&&conflict.snapshot.values.log_sink=="selected editor");
    require(editor.read_user().values.off.at("feature")=="disabled");
    const auto expired=ipc::Clock::now()-std::chrono::seconds(1);
    timeout([&]{editor.with_deadline(expired).read_user();});
    const auto future=ipc::Clock::now()+std::chrono::seconds(5);
    ipc::CancellationSource cancelled;cancelled.cancel();
    expect_cancelled([&]{editor.with_deadline(future).with_cancellation(cancelled.token()).read_user();});
    expect_cancelled([&]{editor.with_cancellation(cancelled.token()).with_deadline(future).read_user();});
    source.cancel();expect_cancelled([&]{editor.replace_user("revision-2",values);});
    // Ordinary explicitly bound clients remain reusable after another scope ends.
    require(c::Editor(selected.endpoint).read_user().revision=="revision-2");
    bootstrap.finish();selected.finish();require(handler.reads==3&&handler.writes==2);
    expect_cancelled([&]{f::Machine("unused").with_cancellation(cancelled.token()).resolve_config_editor();});
    timeout([&]{f::Machine("unused").resolve_config_editor({},abstraction::facade::Scope::Any,expired);});
    SingleFrame absent([](const std::string& frame){auto v=f::detail::service_payload(frame);f::detail::OAResolverResolveResult r;r.value.status=abstraction::facade::ResolutionStatus::Unavailable;std::string raw;f::detail::enc_oa_resolver_resolve_result(raw,r,1);return f::detail::service_reply(v,raw,nullptr);});
    refusal("unavailable",[&]{f::Machine(absent.endpoint).resolve_config_editor();});absent.finish();
}
