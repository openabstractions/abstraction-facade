// Each shim runs in this dedicated executable; production headers use Win32.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <sddl.h>
#include <cstdlib>
#include <iostream>
static int failed_stage;
static unsigned attempts, opened, closed;
static BOOL test_open(HANDLE process, DWORD access, PHANDLE token) {
    ++attempts;
    if(failed_stage==1){SetLastError(ERROR_ACCESS_DENIED);return FALSE;}
    const auto ok=OpenProcessToken(process,access,token);
    if(ok)++opened;
    return ok;
}
static BOOL test_info(HANDLE token,TOKEN_INFORMATION_CLASS kind,LPVOID data,DWORD size,PDWORD needed) {
    if(failed_stage==2){SetLastError(ERROR_ACCESS_DENIED);return FALSE;}
    return GetTokenInformation(token,kind,data,size,needed);
}
static BOOL test_convert(PSID sid,LPSTR* text) {
    if(failed_stage==3){SetLastError(ERROR_ACCESS_DENIED);return FALSE;}
    return ConvertSidToStringSidA(sid,text);
}
static BOOL test_close(HANDLE token) {
    const auto ok=CloseHandle(token);
    if(ok)++closed;
    return ok;
}
#define CloseHandle test_close
#define OpenProcessToken test_open
#define GetTokenInformation test_info
#define ConvertSidToStringSidA test_convert
#include <abstraction/facade/resolution.hpp>
#undef CloseHandle
#undef OpenProcessToken
#undef GetTokenInformation
#undef ConvertSidToStringSidA
int main(){
    _putenv_s("ABSTRACTION_RUNTIME_ENDPOINT","");
    for(failed_stage=1;failed_stage<=3;++failed_stage){

        try{abstraction::facade::runtime_endpoint();return 2;}
        catch(const std::system_error&e){if(e.code().value()!=ERROR_ACCESS_DENIED)return 3;}
        if(opened!=closed)return 4;
    }
    failed_stage=1;const auto previous=attempts;
    _putenv_s("ABSTRACTION_RUNTIME_ENDPOINT","explicit-bootstrap");
    if(abstraction::facade::runtime_endpoint()!="explicit-bootstrap"||attempts!=previous)return 5;
    std::cout<<"bootstrap failures preserve errors and handles\n";
}
