cmake_minimum_required(VERSION 3.15)
project(engine-server)

set(CMAKE_CXX_STANDARD 17)

add_executable(engine-server
    main.cpp
    ../engine/fredb.cpp
    ../engine/memtable.cpp
    ../engine/bloom.cpp
    ../engine/wal.cpp
    ../engine/compaction.cpp
)

target_include_directories(engine-server PRIVATE ../engine)
target_compile_options(engine-server PRIVATE -O2)
