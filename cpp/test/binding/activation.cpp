// Activation of an installed but stopped runtime from default discovery, in
// isolation. The activation steps run against a fake OS environment on every
// platform. On Windows, installed-runtime selection is a shim naming a copy of
// this executable as act-<pid>\tools\openabstractions.exe; that copy stands in
// for `openabstractions start` and for the runtime it starts. Nothing is
// installed or registered, and no registry is written.
#include <abstraction/ipc/client.h>
#include <cstdint>
#include <string>
static oa_ipc_status selection_status = OA_IPC_OK;
static std::string selected_principal, selected_program;
static oa_ipc_server_expectation selected_expectation;
static oa_ipc_status shim_select(uint32_t, oa_ipc_cancellation*, oa_ipc_runtime_selection** out) {
    *out = nullptr;
    if (selection_status != OA_IPC_OK) return selection_status;
    *out = reinterpret_cast<oa_ipc_runtime_selection*>(&selected_expectation);
    return OA_IPC_OK;
}
static const oa_ipc_server_expectation* shim_selected_server(const oa_ipc_runtime_selection*) {
    selected_expectation = {sizeof(oa_ipc_server_expectation), 1, OA_IPC_PRINCIPAL_WINDOWS_SID, 0,
                            selected_principal.data(), selected_principal.size(), selected_program.data(), selected_program.size()};
    return &selected_expectation;
}
static void shim_release(oa_ipc_runtime_selection*) {}
#define oa_ipc_select_runtime shim_select
#define oa_ipc_selected_server shim_selected_server
#define oa_ipc_runtime_selection_release shim_release
#include <abstraction/facade/client.hpp>
#undef oa_ipc_select_runtime
#undef oa_ipc_selected_server
#undef oa_ipc_runtime_selection_release
#include <functional>
#include <iostream>
#include <stdexcept>
#include <vector>

namespace f = abstraction::facade;
namespace ipc = abstraction::ipc;
namespace detail = abstraction::facade::activation_detail;
using Kind = f::ActivationError::Kind;
static void require(bool value, const std::string& what) { if (!value) throw std::runtime_error(what); }

static const auto fixed_now = ipc::Clock::now();
static const std::string installed_program = R"(C:\oa\tools\openabstractions.exe)";

struct FakeActivation {
    bool elevated = false;
    unsigned long code = 0;
    detail::FileState state = detail::FileState::Regular;
    std::vector<std::vector<std::string>> runs;
    detail::Environment env() {
        return {[this] { return elevated; },
                [this](const std::string&) { return state; },
                [this](const std::string& program, const std::vector<std::string>& args, ipc::Deadline) {
                    std::vector<std::string> run{program};
                    run.insert(run.end(), args.begin(), args.end());
                    runs.push_back(run);
                    return detail::Completed{"start refused: an upgrade of this installation\nis in progress\n", code};
                },
                [] { return fixed_now; }};
    }
};

static f::ActivationError activation_error(const std::function<void()>& call) {
    try { call(); } catch (const f::ActivationError& e) { return e; }
    throw std::runtime_error("expected facade::ActivationError");
}

static ipc::ServerExpectation installed(std::string program = installed_program) {
    return {OA_IPC_PRINCIPAL_WINDOWS_SID, "S-1-5-21-0-0-0-1000", std::move(program)};
}

static void runs_the_installed_start_within_the_callers_budget() {
    FakeActivation fake;
    detail::activate_with(installed(), fixed_now + std::chrono::milliseconds(7500), fake.env());
    require(fake.runs == std::vector<std::vector<std::string>>{{installed_program, "start", "--timeout", "7500ms"}}, "run within 7.5 s");
    FakeActivation defaulted;
    detail::activate_with(installed(), fixed_now + f::kDefaultActivationBudget, defaulted.env());
    require(defaulted.runs == std::vector<std::vector<std::string>>{{installed_program, "start", "--timeout", "20000ms"}}, "run within the default budget");
    std::cout << "PASS activation runs the installed start once within the caller's budget\n";
}

static void reports_an_upgrade_in_progress() {
    FakeActivation upgrading;
    upgrading.code = 3;
    auto e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), upgrading.env()); });
    require(e.kind == Kind::UpgradeInProgress, "exit 3 kind");
    require(std::string(e.what()).find("upgrade of this installation is in progress") != std::string::npos, "exit 3 detail");
    FakeActivation failing;
    failing.code = 1;
    e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), failing.env()); });
    require(e.kind == Kind::Failed, "exit 1 kind");
    std::cout << "PASS exit 3 is upgrade_in_progress, exit 1 a failure\n";
}

static void reports_a_virtualized_caller_as_refused() {
    FakeActivation contained;
    contained.code = 4;
    auto e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), contained.env()); });
    require(e.kind == Kind::Refused && contained.runs.size() == 1, "exit 4 kind");
    std::cout << "PASS exit 4, a caller inside a packaged app, is refused\n";
}

static void refuses_without_launching() {
    FakeActivation elevated;
    elevated.elevated = true;
    auto e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), elevated.env()); });
    require(e.kind == Kind::Refused && elevated.runs.empty(), "elevated caller");
    FakeActivation unknown;
    auto env = unknown.env();
    env.elevated = []() -> bool { throw std::runtime_error("token query failed"); };
    e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), env); });
    require(e.kind == Kind::Refused && unknown.runs.empty(), "unknown elevation");
    const std::vector<std::pair<std::string, Kind>> programs{
        {R"(tools\openabstractions.exe)", Kind::Refused},
        {R"(C:\oa\tools\openabstractionsw.exe)", Kind::Refused},
        {"", Kind::Refused},
    };
    for (const auto& entry : programs) {
        const std::string& program = entry.first;
        FakeActivation fake;
        e = activation_error([&] { detail::activate_with(installed(program), fixed_now + std::chrono::seconds(1), fake.env()); });
        require(e.kind == entry.second && fake.runs.empty(), "program " + program);
    }
    for (auto state : {detail::FileState::Missing, detail::FileState::Other}) {
        FakeActivation fake;
        fake.state = state;
        e = activation_error([&] { detail::activate_with(installed(), fixed_now + std::chrono::seconds(1), fake.env()); });
        require(e.kind == Kind::NoInstallation && fake.runs.empty(), "missing or irregular installation");
    }
    FakeActivation expired;
    e = activation_error([&] { detail::activate_with(installed(), fixed_now, expired.env()); });
    require(e.kind == Kind::Failed && expired.runs.empty(), "expired budget");
    require(detail::installed_program_name(R"(C:/OA/Tools/OpenAbstractions.EXE)"), "case-insensitive installed name");
    std::cout << "PASS activation refuses elevated callers, other programs and missing installations without launching\n";
}

#ifndef _WIN32
int main() {
    try {
        runs_the_installed_start_within_the_callers_budget();
        reports_an_upgrade_in_progress();
        reports_a_virtualized_caller_as_refused();
        refuses_without_launching();
        require(!f::activation_supported(), "activation claimed on a platform whose service manager owns the runtime");
        auto e = activation_error([] { f::activate_installed(installed()); });
        require(e.kind == Kind::Unsupported, "unsupported kind");
        std::cout << "PASS this platform does not activate the installed runtime\n";
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
#else
#include <atomic>
#include <cstdlib>
#include <fstream>
#include <sstream>
#include <thread>

static std::string narrow(const std::wstring& wide) {
    const int size = WideCharToMultiByte(CP_UTF8, 0, wide.data(), static_cast<int>(wide.size()), nullptr, 0, nullptr, nullptr);
    std::string out(static_cast<std::size_t>(size), '\0');
    WideCharToMultiByte(CP_UTF8, 0, wide.data(), static_cast<int>(wide.size()), out.data(), size, nullptr, nullptr);
    return out;
}

static std::wstring own_image() {
    std::vector<wchar_t> path(32768);
    const DWORD size = GetModuleFileNameW(nullptr, path.data(), static_cast<DWORD>(path.size()));
    require(size > 0 && size < path.size(), "own image");
    return std::wstring(path.data(), size);
}

static std::string environment(const char* name) {
    const char* value = std::getenv(name);
    return value ? value : "";
}

static void append_record(const std::string& line) {
    std::ofstream(environment("OA_ACTIVATION_RECORD"), std::ios::app) << line << "\n";
}

struct Catalogue : f::Resolver {
    f::ResolveResult resolve(const f::ResolveRequest& request) override {
        f::ResolveResult result;
        result.status = f::ResolutionStatus::Resolved;
        f::ServiceReference ref;
        ref.provider = "fixture"; ref.capability = request.capability; ref.contract = request.contracts.front();
        ref.scope = f::Scope::Local; ref.transport = "oa-framed-local@1"; ref.endpoint = R"(\\.\pipe\oa-activation-provider)";
        result.reference = ref;
        return result;
    }
};

static void pipe_io(HANDLE pipe, void* bytes, DWORD count, bool write) {
    auto* next = static_cast<char*>(bytes);
    while (count) {
        DWORD moved = 0;
        const BOOL ok = write ? WriteFile(pipe, next, count, &moved, nullptr) : ReadFile(pipe, next, count, &moved, nullptr);
        if (!ok || !moved) throw std::runtime_error("fixture pipe I/O failed");
        next += moved; count -= moved;
    }
}

// The started runtime answers one resolver exchange and stays connected until
// the verified client closes after its post-read identity check.
static int serve_mode() {
    std::thread([] { Sleep(20000); ExitProcess(2); }).detach();
    const auto endpoint = environment("ABSTRACTION_RUNTIME_ENDPOINT");
    HANDLE pipe = CreateNamedPipeA(endpoint.c_str(), PIPE_ACCESS_DUPLEX, PIPE_TYPE_BYTE | PIPE_WAIT, 1, 1 << 20, 1 << 20, 0, nullptr);
    if (pipe == INVALID_HANDLE_VALUE) return 4;
    if (!ConnectNamedPipe(pipe, nullptr) && GetLastError() != ERROR_PIPE_CONNECTED) return 5;
    Catalogue catalogue;
    f::ResolverDispatcher dispatcher(catalogue);
    unsigned char header[4];
    pipe_io(pipe, header, 4, false);
    const unsigned size = (unsigned(header[0]) << 24) | (unsigned(header[1]) << 16) | (unsigned(header[2]) << 8) | header[3];
    std::string frame(size, '\0');
    pipe_io(pipe, frame.data(), size, false);
    auto reply = dispatcher.exchange_frame(frame);
    const auto length = static_cast<unsigned>(reply.size());
    unsigned char reply_header[4] = {static_cast<unsigned char>(length >> 24), static_cast<unsigned char>(length >> 16),
                                     static_cast<unsigned char>(length >> 8), static_cast<unsigned char>(length)};
    pipe_io(pipe, reply_header, 4, true);
    pipe_io(pipe, reply.data(), length, true);
    if (!FlushFileBuffers(pipe)) return 8;
    unsigned char trailing = 0;
    DWORD moved = 0;
    const BOOL read = ReadFile(pipe, &trailing, 1, &moved, nullptr);
    const DWORD read_error = read ? ERROR_SUCCESS : GetLastError();
    if (read || read_error != ERROR_BROKEN_PIPE) return 9;
    DisconnectNamedPipe(pipe);
    CloseHandle(pipe);
    return 0;
}

// The stand-in `openabstractions start`: records its arguments, prints a line,
// starts the runtime when asked to, waits for its endpoint and exits with the
// requested status.
static int start_mode(int argc, char** argv) {
    std::string line = "start";
    for (int i = 1; i < argc; ++i) line += std::string(" ") + argv[i];
    append_record(line);
    std::cout << "activation helper output" << std::endl;
    const int code = std::atoi(environment("OA_ACTIVATION_EXIT").c_str());
    if (code != 0 || environment("OA_ACTIVATION_SERVE").empty()) return code;
    const auto image = own_image();
    std::wstring command = L"\"" + image + L"\" serve";
    STARTUPINFOW startup{sizeof startup};
    PROCESS_INFORMATION launched{};
    if (!CreateProcessW(image.c_str(), command.data(), nullptr, nullptr, FALSE, CREATE_NO_WINDOW, nullptr, nullptr, &startup, &launched)) return 6;
    CloseHandle(launched.hThread);
    CloseHandle(launched.hProcess);
    append_record("served " + std::to_string(launched.dwProcessId));
    const auto endpoint = environment("ABSTRACTION_RUNTIME_ENDPOINT");
    const std::wstring wide(endpoint.begin(), endpoint.end());
    for (int attempt = 0; attempt < 1000; ++attempt) {
        if (WaitNamedPipeW(wide.c_str(), 1)) return 0;
        Sleep(10);
    }
    return 7;
}

struct Fixture {
    std::wstring root, program;
    std::string record;
    unsigned serial = 0;
    Fixture() {
        std::vector<wchar_t> cwd(32768);
        const DWORD size = GetCurrentDirectoryW(static_cast<DWORD>(cwd.size()), cwd.data());
        require(size > 0 && size < cwd.size(), "working directory");
        root = std::wstring(cwd.data(), size) + L"\\act-" + std::to_wstring(GetCurrentProcessId());
        require(CreateDirectoryW(root.c_str(), nullptr) || GetLastError() == ERROR_ALREADY_EXISTS, "fixture directory");
        require(CreateDirectoryW((root + L"\\tools").c_str(), nullptr) || GetLastError() == ERROR_ALREADY_EXISTS, "tools directory");
        std::vector<wchar_t> full(32768);
        const DWORD length = GetLongPathNameW((root + L"\\tools").c_str(), full.data(), static_cast<DWORD>(full.size()));
        require(length > 0 && length < full.size(), "long tools path");
        program = std::wstring(full.data(), length) + L"\\openabstractions.exe";
        require(CopyFileW(own_image().c_str(), program.c_str(), FALSE), "copy the stand-in openabstractions.exe");
        record = narrow(root + L"\\record.txt");
        _putenv_s("OA_ACTIVATION_RECORD", record.c_str());
        selected_principal = ipc::process_user_sid();
    }
    // A fresh endpoint and record for one case; selection names the stand-in.
    std::string begin(const char* exit_code, bool serve) {
        DeleteFileA(record.c_str());
        const auto endpoint = R"(\\.\pipe\oa-activation-)" + std::to_string(GetCurrentProcessId()) + "-" + std::to_string(++serial);
        _putenv_s("ABSTRACTION_RUNTIME_ENDPOINT", endpoint.c_str());
        _putenv_s("OA_ACTIVATION_EXIT", exit_code);
        _putenv_s("OA_ACTIVATION_SERVE", serve ? "1" : "");
        selection_status = OA_IPC_OK;
        selected_program = narrow(program);
        return endpoint;
    }
    std::vector<std::string> records(const char* prefix) const {
        std::ifstream in(record);
        std::vector<std::string> found;
        for (std::string line; std::getline(in, line);)
            if (line.rfind(prefix, 0) == 0) found.push_back(line);
        return found;
    }
    void wait_served() const {
        for (const auto& line : records("served ")) {
            HANDLE process = OpenProcess(SYNCHRONIZE, FALSE, static_cast<DWORD>(std::strtoul(line.c_str() + 7, nullptr, 10)));
            if (!process) continue;
            WaitForSingleObject(process, 10000);
            CloseHandle(process);
        }
    }
    ~Fixture() {
        wait_served();
        DeleteFileW(program.c_str());
        DeleteFileA(record.c_str());
        RemoveDirectoryW((root + L"\\tools").c_str());
        RemoveDirectoryW(root.c_str());
    }
};

static f::ResolutionError resolution_error(const std::function<void()>& call) {
    try { call(); } catch (const f::ResolutionError& e) { return e; }
    throw std::runtime_error("expected facade::ResolutionError");
}

static f::ActivationError activation_cause(const f::ResolutionError& e) {
    try { e.rethrow_cause(); } catch (const f::ActivationError& cause) { return cause; } catch (...) {}
    throw std::runtime_error("resolution error cause is no ActivationError: " + std::string(e.what()));
}

static long long start_timeout_ms(const std::string& line) {
    const std::string flag = "start start --timeout ";
    require(line.rfind(flag, 0) == 0 && line.size() > flag.size() + 2 && line.compare(line.size() - 2, 2, "ms") == 0, "start arguments: " + line);
    return std::stoll(line.substr(flag.size(), line.size() - flag.size() - 2));
}

static void activates_a_stopped_installation_once(Fixture& fixture) {
    fixture.begin("0", true);
    auto logger = f::Machine{}.resolve_log();
    (void)logger;
    auto starts = fixture.records("start");
    require(starts.size() == 1, "one activation without a deadline");
    const auto defaulted = start_timeout_ms(starts.front());
    require(defaulted > 15000 && defaulted <= 20000, "default activation budget " + std::to_string(defaulted));
    fixture.wait_served();

    fixture.begin("0", true);
    auto bounded = f::Machine{}.resolve_log({}, abstraction::facade::Scope::Any, ipc::Clock::now() + std::chrono::milliseconds(7500));
    (void)bounded;
    starts = fixture.records("start");
    require(starts.size() == 1, "one activation within a deadline");
    const auto remaining = start_timeout_ms(starts.front());
    require(remaining > 0 && remaining <= 7500, "caller's activation budget " + std::to_string(remaining));
    fixture.wait_served();
    std::cout << "PASS default discovery starts the stopped installation once and resolves within the budget\n";
}

static void reports_an_upgrade_in_progress_as_its_own_status(Fixture& fixture) {
    const auto endpoint = fixture.begin("3", false);
    auto e = resolution_error([] { f::Machine{}.resolve_log(); });
    require(e.status == f::kUpgradeInProgress && !e.refusal(), "upgrade_in_progress status: " + e.status);
    require(e.looked_for == "the installed runtime at " + endpoint, "looked for");
    const auto cause = activation_cause(e);
    require(cause.kind == Kind::UpgradeInProgress, "cause kind");
    require(std::string(cause.what()).find("activation helper output") != std::string::npos, "start output in the cause");
    require(fixture.records("start").size() == 1, "one activation");
    std::cout << "PASS exit 3 is upgrade_in_progress with the start output as its cause\n";
}

static void reports_another_failure_as_runtime_unavailable(Fixture& fixture) {
    fixture.begin("1", false);
    auto e = resolution_error([] { f::Machine{}.resolve_config(); });
    require(e.status == f::kRuntimeUnavailable, "runtime_unavailable status");
    require(activation_cause(e).kind == Kind::Failed, "failed cause");
    require(fixture.records("start").size() == 1, "one activation");
    std::cout << "PASS another start exit status stays runtime_unavailable\n";
}

static void starts_only_an_installed_openabstractions_exe(Fixture& fixture) {
    fixture.begin("0", true);
    selected_program = narrow(own_image());
    auto e = resolution_error([] { f::Machine{}.resolve_log(); });
    require(e.status == f::kRuntimeUnavailable && activation_cause(e).kind == Kind::Refused, "another program refused");
    require(fixture.records("start").empty(), "another program launched");
    std::cout << "PASS a selection naming another program is refused without launching\n";
}

static void activates_nothing_without_an_installation(Fixture& fixture) {
    fixture.begin("0", true);
    selection_status = OA_IPC_UNTRUSTED;
    auto e = resolution_error([] { f::Machine{}.resolve_log(); });
    require(e.status == f::kRuntimeUnavailable && e.looked_for == "the installed runtime", "no installation");
    require(fixture.records("start").empty(), "launched with no installation");
    std::cout << "PASS no installation: runtime_unavailable and nothing starts\n";
}

static void explicit_endpoints_are_never_activated(Fixture& fixture) {
    const auto endpoint = fixture.begin("0", true);
    auto e = resolution_error([&] { f::Machine(endpoint).resolve_log(); });
    require(e.status == f::kRuntimeUnavailable && e.looked_for == "the explicit endpoint " + endpoint, "explicit endpoint");
    const ipc::ServerExpectation expected{OA_IPC_PRINCIPAL_WINDOWS_SID, selected_principal, selected_program};
    e = resolution_error([&] { f::Machine{}.with_server_expectation(expected).resolve_log(); });
    require(e.status == f::kRuntimeUnavailable, "explicit server expectation");
    require(fixture.records("start").empty(), "explicit endpoint launched");
    std::cout << "PASS explicit endpoints and server expectations never activate\n";
}

static void a_present_untrusted_server_is_never_activated(Fixture& fixture) {
    const auto endpoint = fixture.begin("0", true);
    // This process listens; the selection expects the stand-in's image.
    HANDLE pipe = CreateNamedPipeA(endpoint.c_str(), PIPE_ACCESS_DUPLEX, PIPE_TYPE_BYTE | PIPE_WAIT, 1, 4096, 4096, 0, nullptr);
    require(pipe != INVALID_HANDLE_VALUE, "untrusted fixture pipe");
    auto e = resolution_error([] { f::Machine{}.resolve_log(); });
    CloseHandle(pipe);
    require(e.status == f::kRuntimeUnavailable, "untrusted server status");
    bool untrusted = false;
    try { e.rethrow_cause(); } catch (const ipc::FrameError& cause) { untrusted = cause.status == ipc::Status::Untrusted; } catch (...) {}
    require(untrusted, "untrusted server cause");
    require(fixture.records("start").empty(), "untrusted server launched");
    std::cout << "PASS a present untrusted server is reported as it is and nothing starts\n";
}

// A create that lands in root reads real; one redirected into a package's
// LocalCache reads virtualized with that family; neither copy survives. The live
// view is printed, and OA_EXPECT_PROFILE, when set, names it.
static void probes_the_profile_view() {
    const std::wstring root = own_image().substr(0, own_image().find_last_of(L"\\")) + L"\\profile-" + std::to_wstring(::GetCurrentProcessId());
    const std::wstring local = root + L"\\Packages\\Claude_pzs8sxrjxfjjc\\LocalCache\\Local";
    ::CreateDirectoryW(root.c_str(), nullptr);
    ::CreateDirectoryW((root + L"\\Packages").c_str(), nullptr);
    ::CreateDirectoryW((root + L"\\Packages\\Claude_pzs8sxrjxfjjc").c_str(), nullptr);
    ::CreateDirectoryW((root + L"\\Packages\\Claude_pzs8sxrjxfjjc\\LocalCache").c_str(), nullptr);
    ::CreateDirectoryW(local.c_str(), nullptr);
    const auto make = [](const std::wstring& path) {
        detail::Handle file(::CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, nullptr));
        return file.value != nullptr;
    };
    auto view = detail::probe_profile(root, make);
    require(!view.virtualized && view.to_string() == "real", "real create");
    std::wstring made;
    view = detail::probe_profile(root, [&](const std::wstring& path) {
        made = local + path.substr(path.find_last_of(L"\\"));
        return make(made);
    });
    require(view.virtualized && view.family == "Claude_pzs8sxrjxfjjc" && view.to_string() == "virtualized(Claude_pzs8sxrjxfjjc)", "redirected create");
    require(::GetFileAttributesW(made.c_str()) == INVALID_FILE_ATTRIBUTES, "the package copy survived the probe");
    const auto live = f::profile_view();
    std::cout << "profile view: " << live.to_string() << "\n";
    const auto expected = environment("OA_EXPECT_PROFILE");
    require(expected.empty() || expected == live.to_string(), "live profile view " + live.to_string() + ", want " + expected);
    std::cout << "PASS the profile probe finds a package's copy and removes it\n";
}

static void observation_never_activates(Fixture& fixture) {
    fixture.begin("0", true);
    const auto observed = f::observe_runtime(f::ResolutionClient{}, f::default_status_requests(), f::unknown_bootstrap(),
                                             ipc::Clock::now() + std::chrono::seconds(2));
    require(observed.error != nullptr, "observation of an absent runtime succeeded");
    require(fixture.records("start").empty(), "observation launched");
    std::cout << "PASS observation never activates\n";
}

int main(int argc, char** argv) {
    if (argc > 1 && std::string(argv[1]) == "start") return start_mode(argc, argv);
    if (argc > 1 && std::string(argv[1]) == "serve") return serve_mode();
    try {
        runs_the_installed_start_within_the_callers_budget();
        reports_an_upgrade_in_progress();
        reports_a_virtualized_caller_as_refused();
        refuses_without_launching();
        require(f::activation_supported(), "Windows activation support");
        probes_the_profile_view();
        if (detail::token_elevated()) {
            std::cout << "SKIP elevated: activation refuses this token before launching\n";
            return 0;
        }
        Fixture fixture;
        activates_a_stopped_installation_once(fixture);
        reports_an_upgrade_in_progress_as_its_own_status(fixture);
        reports_another_failure_as_runtime_unavailable(fixture);
        starts_only_an_installed_openabstractions_exe(fixture);
        activates_nothing_without_an_installation(fixture);
        explicit_endpoints_are_never_activated(fixture);
        a_present_untrusted_server_is_never_activated(fixture);
        observation_never_activates(fixture);
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
#endif
