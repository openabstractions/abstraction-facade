#pragma once
#include <abstraction/ipc/frame.hpp>
#include <algorithm>
#include <chrono>
#include <cstdint>
#include <functional>
#include <optional>
#include <random>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>
#ifdef _WIN32
#include <abstraction/ipc/process.hpp>
#endif

namespace abstraction::facade {

// Default discovery found the installed runtime's endpoint absent, and
// activating it was refused because an installer is replacing that
// installation. The ResolutionError's cause is an ActivationError of kind
// UpgradeInProgress. Retry when the installation finishes.
inline constexpr std::string_view kUpgradeInProgress = "upgrade_in_progress";

// Bounds an activation whose caller set no deadline. It is
// `openabstractions start`'s own default.
inline constexpr std::chrono::milliseconds kDefaultActivationBudget{20000};

// A failed activation of the selected installation's runtime.
class ActivationError : public std::runtime_error {
public:
    enum class Kind {
        // `openabstractions start` exited 3: an installer holds the upgrade exclusion.
        UpgradeInProgress,
        // Nothing was launched: an elevated caller, a selection naming no
        // openabstractions.exe, or `start` exited 4 because the caller sees a
        // packaged app's private copy of AppData.
        Refused,
        // systemd and launchd start and restart the installed runtime.
        Unsupported,
        // The selected program is not an installed regular file.
        NoInstallation,
        // The launch failed, the budget expired, or start exited with another status.
        Failed,
    };
    Kind kind;
    ActivationError(Kind kind_value, const std::string& message)
        : std::runtime_error("activate installed runtime: " + message), kind(kind_value) {}
};

// How this process sees the account's profile folders. A process descended
// from an MSIX packaged app with file virtualization on sees a per-package copy
// of new files under AppData, so a runtime it started, or state it wrote, would
// be private to that package.
struct ProfileView {
    // New files under AppData land in a package's private copy.
    bool virtualized = false;
    // That package's family name when virtualized.
    std::string family;
    // "real" or "virtualized(<family>)", as `openabstractions status` prints it.
    std::string to_string() const { return virtualized ? "virtualized(" + family + ")" : std::string("real"); }
};

namespace activation_detail {
enum class FileState { Missing, Regular, Other };
struct Completed {
    std::string output;
    unsigned long exit_code = 0;
};
// OS access, replaceable by isolated fixtures. run launches program with args
// hidden and waits within deadline; it throws ActivationError(Failed) when the
// program cannot run or does not finish in time.
struct Environment {
    std::function<bool()> elevated;
    std::function<FileState(const std::string&)> stat;
    std::function<Completed(const std::string&, const std::vector<std::string>&, ipc::Deadline)> run;
    std::function<ipc::Clock::time_point()> now;
};

// `start --timeout` for the remaining budget, truncated to milliseconds, at
// least 1 ms. Go's duration parser reads the value.
inline std::string timeout_argument(ipc::Deadline deadline, ipc::Clock::time_point now) {
    const auto remaining = std::chrono::duration_cast<std::chrono::milliseconds>(deadline - now).count();
    return std::to_string(std::max<std::int64_t>(remaining, 1)) + "ms";
}

inline std::string collapse_whitespace(const std::string& text) {
    std::string out;
    bool space = false;
    for (unsigned char c : text) {
        if (c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f') { space = !out.empty(); continue; }
        if (space) out.push_back(' ');
        space = false;
        out.push_back(static_cast<char>(c));
    }
    return out;
}

inline bool installed_program_name(const std::string& program) {
    const bool drive = program.size() >= 3 &&
        ((program[0] >= 'A' && program[0] <= 'Z') || (program[0] >= 'a' && program[0] <= 'z')) &&
        program[1] == ':' && (program[2] == '\\' || program[2] == '/');
    const bool unc = program.size() >= 2 && (program[0] == '\\' || program[0] == '/') && (program[1] == '\\' || program[1] == '/');
    if (!drive && !unc) return false;
    const auto slash = program.find_last_of("\\/");
    std::string base = program.substr(slash + 1);
    std::transform(base.begin(), base.end(), base.begin(), [](unsigned char c) { return static_cast<char>(c >= 'A' && c <= 'Z' ? c - 'A' + 'a' : c); });
    return base == "openabstractions.exe";
}

// Runs the selected installation's own `openabstractions.exe start` once. The
// deadline bounds the launch and its readiness wait.
inline void activate_with(const ipc::ServerExpectation& server, ipc::Deadline deadline, const Environment& env) {
    using Kind = ActivationError::Kind;
    const auto& program = server.program;
    if (!installed_program_name(program))
        throw ActivationError(Kind::Refused, "refused: the selected installation names no openabstractions.exe");
    switch (env.stat(program)) {
    case FileState::Missing: throw ActivationError(Kind::NoInstallation, "installed " + program + " does not exist");
    case FileState::Other: throw ActivationError(Kind::NoInstallation, "installed " + program + " is not a regular file");
    case FileState::Regular: break;
    }
    bool elevated = false;
    try {
        elevated = env.elevated();
    } catch (const std::exception& e) {
        throw ActivationError(Kind::Refused, std::string("refused: the caller's token elevation is unknown: ") + e.what());
    }
    if (elevated)
        throw ActivationError(Kind::Refused, "refused: the caller's token is elevated, and the installed runtime starts only in the user's unelevated session");
    const auto now = env.now();
    if (deadline <= now) throw ActivationError(Kind::Failed, "the activation budget expired before " + program + " start");
    const auto completed = env.run(program, {"start", "--timeout", timeout_argument(deadline, now)}, deadline);
    const auto detail = collapse_whitespace(completed.output);
    if (completed.exit_code == 0) return;
    if (completed.exit_code == 3)
        throw ActivationError(Kind::UpgradeInProgress, "an upgrade of the installed runtime is in progress: " + detail);
    if (completed.exit_code == 4)
        throw ActivationError(Kind::Refused, "refused: " + detail);
    throw ActivationError(Kind::Failed, program + " start exited " + std::to_string(completed.exit_code) + ": " + detail);
}

#ifdef _WIN32
struct Handle {
    HANDLE value = nullptr;
    Handle() = default;
    explicit Handle(HANDLE h) : value(h == INVALID_HANDLE_VALUE ? nullptr : h) {}
    Handle(const Handle&) = delete;
    Handle& operator=(const Handle&) = delete;
    ~Handle() { reset(); }
    void reset() { if (value) ::CloseHandle(value); value = nullptr; }
};

[[noreturn]] inline void launch_failure(const std::string& what) {
    throw ActivationError(ActivationError::Kind::Failed, what + " failed (Windows error " + std::to_string(::GetLastError()) + ")");
}

inline std::wstring widen(const std::string& utf8) {
    if (utf8.empty()) return {};
    const int size = ::MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, utf8.data(), static_cast<int>(utf8.size()), nullptr, 0);
    if (size <= 0) throw ActivationError(ActivationError::Kind::Refused, "refused: the selected program path is not UTF-8");
    std::wstring out(static_cast<std::size_t>(size), L'\0');
    ::MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, utf8.data(), static_cast<int>(utf8.size()), out.data(), size);
    return out;
}

inline bool token_elevated() {
    HANDLE raw = nullptr;
    if (!::OpenProcessToken(::GetCurrentProcess(), TOKEN_QUERY, &raw))
        throw std::runtime_error("OpenProcessToken failed (Windows error " + std::to_string(::GetLastError()) + ")");
    Handle token(raw);
    TOKEN_ELEVATION elevation{};
    DWORD size = 0;
    if (!::GetTokenInformation(token.value, TokenElevation, &elevation, sizeof elevation, &size))
        throw std::runtime_error("TokenElevation failed (Windows error " + std::to_string(::GetLastError()) + ")");
    return elevation.TokenIsElevated != 0;
}

inline FileState file_state(const std::string& program) {
    const DWORD attributes = ::GetFileAttributesW(widen(program).c_str());
    if (attributes == INVALID_FILE_ATTRIBUTES) return FileState::Missing;
    return attributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_DEVICE) ? FileState::Other : FileState::Regular;
}

// The probe behind profile_view(). It creates one uniquely named empty file
// directly in root, which is %LOCALAPPDATA% and always exists, so no folder is
// created in a package's copy. A virtualized create lands in
// Packages\<family>\LocalCache\Local, where the probe then finds it. Both
// copies are removed. create makes the file and reports whether it did.
inline ProfileView probe_profile(const std::wstring& root, const std::function<bool(const std::wstring&)>& create) {
    std::random_device random;
    const std::wstring name = L".oa-profile-" + std::to_wstring(::GetCurrentProcessId()) + L"-" +
        std::to_wstring(random()) + std::to_wstring(random());
    const std::wstring probe = root + L"\\" + name;
    if (!create(probe)) throw std::runtime_error("profile view: the probe file could not be created");
    std::vector<std::wstring> copies;
    WIN32_FIND_DATAW found{};
    const std::wstring packages = root + L"\\Packages\\";
    HANDLE search = ::FindFirstFileExW((packages + L"*").c_str(), FindExInfoBasic, &found, FindExSearchLimitToDirectories, nullptr, 0);
    if (search != INVALID_HANDLE_VALUE) {
        do {
            const std::wstring family = found.cFileName;
            if (family == L"." || family == L"..") continue;
            const std::wstring copy = packages + family + L"\\LocalCache\\Local\\" + name;
            if (::GetFileAttributesW(copy.c_str()) != INVALID_FILE_ATTRIBUTES) copies.push_back(family);
        } while (::FindNextFileW(search, &found));
        ::FindClose(search);
    }
    ::DeleteFileW(probe.c_str());
    for (const auto& family : copies) ::DeleteFileW((packages + family + L"\\LocalCache\\Local\\" + name).c_str());
    if (copies.empty()) return {};
    if (copies.size() > 1) throw std::runtime_error("profile view: the probe appears in " + std::to_string(copies.size()) + " package copies");
    const std::wstring& family = copies.front();
    const int size = ::WideCharToMultiByte(CP_UTF8, 0, family.data(), static_cast<int>(family.size()), nullptr, 0, nullptr, nullptr);
    std::string narrow(static_cast<std::size_t>(size > 0 ? size : 0), '\0');
    if (size > 0) ::WideCharToMultiByte(CP_UTF8, 0, family.data(), static_cast<int>(family.size()), narrow.data(), size, nullptr, nullptr);
    return {true, narrow};
}

inline ProfileView current_profile_view() {
    wchar_t root[MAX_PATH + 1];
    const DWORD length = ::GetEnvironmentVariableW(L"LOCALAPPDATA", root, MAX_PATH + 1);
    if (length == 0 || length > MAX_PATH) throw std::runtime_error("profile view: LOCALAPPDATA is not set");
    return probe_profile(std::wstring(root, length), [](const std::wstring& path) {
        Handle file(::CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, nullptr));
        return file.value != nullptr;
    });
}

// Keeps at most 64 KiB of the child's combined output; the rest is read and dropped.
inline void drain(HANDLE pipe, std::string& output) {
    char buffer[4096];
    for (;;) {
        DWORD available = 0;
        if (!::PeekNamedPipe(pipe, nullptr, 0, nullptr, &available, nullptr) || available == 0) return;
        DWORD moved = 0;
        if (!::ReadFile(pipe, buffer, std::min<DWORD>(available, sizeof buffer), &moved, nullptr) || moved == 0) return;
        output.append(buffer, std::min<std::size_t>(moved, (64u << 10) - std::min<std::size_t>(output.size(), 64u << 10)));
    }
}

// Launches program hidden, with no window and only its own standard handles
// inherited, in its installation directory, and waits within deadline. The
// child is terminated when the deadline passes. The output pipe is drained
// while the child runs and read once more after it exits, so a descendant
// holding the pipe open cannot extend the wait.
inline Completed run_hidden(const std::string& program, const std::vector<std::string>& args, ipc::Deadline deadline) {
    const std::wstring image = widen(program);
    std::wstring command = L"\"" + image + L"\"";
    for (const auto& arg : args) {
        if (arg.empty() || arg.find_first_of(" \t\"") != std::string::npos)
            throw ActivationError(ActivationError::Kind::Failed, "argument needs quoting: " + arg);
        command += L" " + widen(arg);
    }
    const auto separator = image.find_last_of(L"\\/");
    const std::wstring directory = image.substr(0, separator == std::wstring::npos ? 0 : separator);

    SECURITY_ATTRIBUTES inheritable{sizeof(SECURITY_ATTRIBUTES), nullptr, TRUE};
    HANDLE read_raw = nullptr, write_raw = nullptr;
    if (!::CreatePipe(&read_raw, &write_raw, &inheritable, 0)) launch_failure("CreatePipe");
    Handle read_end(read_raw), write_end(write_raw);
    if (!::SetHandleInformation(read_end.value, HANDLE_FLAG_INHERIT, 0)) launch_failure("SetHandleInformation");
    Handle input(::CreateFileW(L"NUL", GENERIC_READ, FILE_SHARE_READ | FILE_SHARE_WRITE, &inheritable, OPEN_EXISTING, 0, nullptr));
    if (!input.value) launch_failure("open NUL");

    SIZE_T list_size = 0;
    ::InitializeProcThreadAttributeList(nullptr, 1, 0, &list_size);
    std::vector<unsigned char> list_storage(list_size);
    const auto list = static_cast<LPPROC_THREAD_ATTRIBUTE_LIST>(static_cast<void*>(list_storage.data()));
    if (!::InitializeProcThreadAttributeList(list, 1, 0, &list_size)) launch_failure("InitializeProcThreadAttributeList");
    struct ListOwner { LPPROC_THREAD_ATTRIBUTE_LIST list; ~ListOwner() { ::DeleteProcThreadAttributeList(list); } } owned_list{list};
    HANDLE inherited[2] = {input.value, write_end.value};
    if (!::UpdateProcThreadAttribute(list, 0, PROC_THREAD_ATTRIBUTE_HANDLE_LIST, inherited, sizeof inherited, nullptr, nullptr))
        launch_failure("UpdateProcThreadAttribute");

    STARTUPINFOEXW startup{};
    startup.StartupInfo.cb = sizeof startup;
    startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES | STARTF_USESHOWWINDOW;
    startup.StartupInfo.wShowWindow = SW_HIDE;
    startup.StartupInfo.hStdInput = input.value;
    startup.StartupInfo.hStdOutput = write_end.value;
    startup.StartupInfo.hStdError = write_end.value;
    startup.lpAttributeList = list;
    std::vector<wchar_t> command_line(command.begin(), command.end());
    command_line.push_back(L'\0');
    PROCESS_INFORMATION launched{};
    if (!::CreateProcessW(image.c_str(), command_line.data(), nullptr, nullptr, TRUE,
                          CREATE_NO_WINDOW | EXTENDED_STARTUPINFO_PRESENT, nullptr,
                          directory.empty() ? nullptr : directory.c_str(), &startup.StartupInfo, &launched))
        throw ActivationError(ActivationError::Kind::Failed, "run " + program + " start failed (Windows error " + std::to_string(::GetLastError()) + ")");
    Handle process(launched.hProcess), thread(launched.hThread);
    write_end.reset();
    input.reset();

    Completed completed;
    for (;;) {
        drain(read_end.value, completed.output);
        const auto left = std::chrono::duration_cast<std::chrono::milliseconds>(deadline - ipc::Clock::now()).count();
        const DWORD waited = ::WaitForSingleObject(process.value, static_cast<DWORD>(std::clamp<decltype(left)>(left, 0, 10)));
        if (waited == WAIT_OBJECT_0) break;
        if (waited != WAIT_TIMEOUT) launch_failure("wait for " + program + " start");
        if (left <= 0) {
            ::TerminateProcess(process.value, 1);
            ::WaitForSingleObject(process.value, 1000);
            throw ActivationError(ActivationError::Kind::Failed, program + " start did not finish within the activation budget");
        }
    }
    drain(read_end.value, completed.output);
    DWORD code = 0;
    if (!::GetExitCodeProcess(process.value, &code)) launch_failure("GetExitCodeProcess");
    completed.exit_code = code;
    return completed;
}

inline Environment system_environment() {
    return {token_elevated, file_state, run_hidden, [] { return ipc::Clock::now(); }};
}

// A named pipe with no instance: the equivalent of Go's fs.ErrNotExist on
// dial. A busy or refusing pipe exists. WaitNamedPipe connects to nothing.
inline bool endpoint_absent(const std::string& endpoint) {
    const std::string prefix = R"(\\.\pipe\)";
    if (endpoint.compare(0, prefix.size(), prefix) != 0) return false;
    for (unsigned char c : endpoint) if (c > 127) return false;
    const std::wstring wide(endpoint.begin(), endpoint.end());
    if (::WaitNamedPipeW(wide.c_str(), 1)) return false;
    const DWORD error = ::GetLastError();
    return error == ERROR_FILE_NOT_FOUND || error == ERROR_PATH_NOT_FOUND;
}
#else
inline bool endpoint_absent(const std::string&) { return false; }
#endif
}

// This process's profile view, probed once. On Windows the probe creates and
// deletes one empty file under %LOCALAPPDATA%; elsewhere the view is always
// real. It throws std::runtime_error when the view is unknown, which a caller
// about to write state or activate a runtime treats as a refusal.
inline ProfileView profile_view() {
#ifdef _WIN32
    static const ProfileView view = activation_detail::current_profile_view();
    return view;
#else
    return {};
#endif
}

// Whether this platform's SDK starts an installed but stopped runtime.
inline bool activation_supported() {
#ifdef _WIN32
    return true;
#else
    return false;
#endif
}

// Starts the selected installation's runtime once, through its own
// `openabstractions.exe start --timeout <remaining budget>`, hidden and
// unelevated, and waits for it to report readiness before the deadline
// (kDefaultActivationBudget without one). It performs no installation and
// selects nothing: server comes from installed-runtime selection. An elevated
// caller is refused without a launch. Other platforms throw Unsupported.
inline void activate_installed(const ipc::ServerExpectation& server, std::optional<ipc::Deadline> deadline = std::nullopt) {
#ifdef _WIN32
    activation_detail::activate_with(server, deadline.value_or(ipc::Clock::now() + kDefaultActivationBudget),
                                     activation_detail::system_environment());
#else
    (void)server;
    (void)deadline;
    throw ActivationError(ActivationError::Kind::Unsupported, "on-demand runtime activation is not implemented on this platform");
#endif
}

}
