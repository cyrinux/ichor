# libichorllm.so: llama.cpp (pinned in build.sh, which passes its source as LLAMA_SRC) and
# the on-device engine shared with iOS (native/llm), behind a JNI layer. Built by build.sh
# with the NDK's toolchain file, one ABI at a time, into app/build/llm-jni/<abi>/.
cmake_minimum_required(VERSION 3.22)
project(ichorllm C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

if(NOT LLAMA_SRC)
  message(FATAL_ERROR "LLAMA_SRC must point at the llama.cpp source (build.sh fetches it)")
endif()

# Only the library: no tools, examples, tests, server or network code.
set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_COMMON OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TOOLS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_APP OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_MTMD OFF CACHE BOOL "" FORCE)
set(LLAMA_OPENSSL OFF CACHE BOOL "" FORCE)
set(GGML_NATIVE OFF CACHE BOOL "" FORCE)
set(GGML_OPENMP OFF CACHE BOOL "" FORCE)
set(GGML_LLAMAFILE OFF CACHE BOOL "" FORCE)
set(GGML_BACKEND_DL OFF CACHE BOOL "" FORCE)
add_subdirectory(${LLAMA_SRC} llama EXCLUDE_FROM_ALL)

set(ENGINE_DIR ${CMAKE_CURRENT_LIST_DIR}/../../../../native/llm)
add_library(ichorllm SHARED ichor_llm_jni.cpp ${ENGINE_DIR}/ichor_llm.cpp)
target_include_directories(ichorllm PRIVATE ${ENGINE_DIR})
target_link_libraries(ichorllm PRIVATE llama)
# Only the JNI entry points are exported; sections nothing reaches are dropped.
set_target_properties(ichorllm PROPERTIES C_VISIBILITY_PRESET hidden CXX_VISIBILITY_PRESET hidden)
target_link_options(ichorllm PRIVATE -Wl,--gc-sections -Wl,--exclude-libs,ALL)
if(ANDROID)
  # Android 15+ needs 16 KB page alignment, as for the Go core.
  target_link_options(ichorllm PRIVATE -Wl,-z,max-page-size=16384)
endif()
