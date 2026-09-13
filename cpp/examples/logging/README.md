# Resolved logging from public C++ sources

This example uses `Machine::ResolveLog()` to select a service, then submits a log
frame. A missing resolver returns an error. The application owns no provider files.

The exact six public GitHub revisions in [sources.lock](sources.lock) were fetched
and built on 2026-09-12 with MSVC 19.51, Windows SDK 10.0.26100 and CMake 4.3.1-msvc1.
They are a tested source set, not a latest-release claim. At this older facade
revision `Log()` uses a conventional endpoint; this example deliberately calls
`ResolveLog()`. Current development source also supports that explicit API.

## Reproduce

Copy this example directory (README, sources.lock, main.cpp and CMakeLists.txt)
into an empty working directory named `logging-example`. Requirements: Git,
CMake 3.16 or newer, a C++17 compiler and its platform SDK. The fetch commands use
a POSIX shell such as Bash, available through Git Bash on Windows. From inside
`logging-example`, fetch only the pinned public commits:

```sh
set -eu
mkdir sources
while read -r name revision; do
  git init "sources/$name"
  git -C "sources/$name" remote add origin "https://github.com/openabstractions/$name.git"
  git -C "sources/$name" fetch --depth 1 origin "$revision"
  git -C "sources/$name" checkout --detach FETCH_HEAD
  test "$(git -C "sources/$name" rev-parse HEAD)" = "$revision"
done < sources.lock
```

Choose an absolute writable prefix for the following commands. On Windows use
an absolute drive path such as `C:/work/logging-example/prefix`; on Unix use an
absolute path under your working directory. Set `PREFIX` once to that path. The assignment below is an example; edit it
for your working directory.

```sh
PREFIX="C:/work/logging-example/prefix" # edit once; on Unix use an absolute Unix path
cmake -S sources/abstraction-facade/cpp -B build/packages -DABSTRACTION_FACADE_BUILD_AGGREGATE=ON -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build build/packages --config Release
cmake --install build/packages --config Release
cmake -S . -B build/app -DCMAKE_PREFIX_PATH="$PREFIX"
cmake --build build/app --config Release
```

The final consumer uses installed packages only. CMake obtains no private source
or workspace paths. The source set builds the client transport and protocol
libraries and installs them under the selected prefix. No OS service is installed.

## Run with a compatible runtime

The operator supplies a running runtime exposing `abstraction.facade/resolver@1`
and a registered logging service implementing `abstraction.logging/sink@1`.
Set `ABSTRACTION_RUNTIME_ENDPOINT` to that runtime's bootstrap address when using
an isolated host. Run `build/app/Release/my_app.exe` with Visual Studio, or
`build/app/my_app` with a single-configuration generator.

Success means the logging frame was submitted; durable logging acknowledgement
is outside this contract. An absent bootstrap exits with an error. Runtime
installation is a separate deployment step; consult the selected runtime
package's instructions and platform evidence.

Current validation built the exact example against a fresh installed prefix from
these public sources. Running the example against a unique absent Windows pipe
returned `frame write failed` and exit 1. A missing installed prefix failed CMake
configuration explicitly. Validation did not install or run an OS runtime. The default local
identity and transport behavior remains subject to the selected runtime's
platform contract.
