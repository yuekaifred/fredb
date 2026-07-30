{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    fredb-engine.url = "github:yuekaifred/fredb-engine";
    fredb-engine.flake = false;
  };

  outputs = { self, nixpkgs, flake-utils, fredb-engine }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages = {
          fredb-server = pkgs.buildGoModule {
            pname = "fredb-server";
            version = "0.1.0";
            src = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset = pkgs.lib.fileset.unions [ ./server ./website ];
            };
            modRoot = "server";
            # note to self swap this out with pkgs.lib.fakeHash EVERYTIME
            vendorHash = "sha256-Neu+jW6zSbQifSNxBuYwNH5AmBXxaZK5PhcoH3ZRV+E=";
            doCheck = false;
            nativeBuildInputs = [ pkgs.cmake pkgs.clang ];
            env = {
              CGO_ENABLED = "1";
              CXX = "clang++";
            };
            preBuild = ''
              mkdir -p ../engine
              cp -r ${fredb-engine}/. ../engine/
              chmod -R u+w ../engine
              (
                cd ../engine
                cmake -S . -B build \
                  -DCMAKE_BUILD_TYPE=Release \
                  -DCMAKE_C_COMPILER=clang \
                  -DCMAKE_CXX_COMPILER=clang++ \
                  -Wno-dev
                cmake --build build --target fredb_core --parallel
              )
            '';
            postInstall = ''
              cp -r ../website/static $out/static
            '';
          };

          start = pkgs.writeShellApplication {
            name = "fredb-start";
            runtimeInputs = [ self.packages.${system}.fredb-server ];
            text = ''
              pkill -f fredb-server  2>/dev/null || true
              FREDB_VERBOSE="''${FREDB_VERBOSE:-0}" fredb-server
            '';
          };

          tests = pkgs.writeShellApplication {
            name = "fredb-tests";
            runtimeInputs = [ self.packages.${system}.fredb-server pkgs.curl pkgs.go ];
            text = ''
              pkill -f fredb-server  2>/dev/null || true

              TESTROOT="$(mktemp -d)"
              export FREDB_DATA_ROOT="$TESTROOT/data"
              export FREDB_MAX_STORAGE_BYTES="262144"
              export FREDB_RATE_LIMIT_CAPACITY_BYTES="400000"
              export FREDB_RATE_LIMIT_REFILL_BYTES_PER_SEC="1"
              export FREDB_RATE_LIMIT_FLAT_OVERHEAD_BYTES="500"
              export FREDB_ADMIN_KEY="test-admin-key"

              FREDB_VERBOSE="''${FREDB_VERBOSE:-0}" fredb-server &
              SERVER_PID=$!

              for _ in $(seq 1 50); do
                curl -s -o /dev/null http://localhost:8081/ && break
                sleep 0.1
              done

              cleanup() {
                kill "$SERVER_PID" 2>/dev/null || true
                wait "$SERVER_PID" 2>/dev/null || true
                rm -rf "$TESTROOT"
              }
              trap cleanup EXIT

              export HOME
              HOME="$(mktemp -d)"
              export GOCACHE="$HOME/.cache/go-build"
              export GOPATH="$HOME/go"

              WORKDIR="$(mktemp -d)"
              mkdir -p "$WORKDIR/integration-tests"
              cp -r ${./integration-tests}/. "$WORKDIR/integration-tests"/
              chmod -R u+w "$WORKDIR"
              cd "$WORKDIR/integration-tests"

              CGO_ENABLED=0 \
                FREDB_TEST_BASE_URL=http://localhost:8080 \
                FREDB_TEST_ADMIN_URL=http://localhost:8081 \
                FREDB_TEST_ADMIN_KEY=test-admin-key \
                go test ./... -v "$@"
            '';
          };

          default = self.packages.${system}.start;
        };

        apps = {
          default = flake-utils.lib.mkApp { drv = self.packages.${system}.start; };
          start = flake-utils.lib.mkApp { drv = self.packages.${system}.start; };
          tests = flake-utils.lib.mkApp { drv = self.packages.${system}.tests; };
        };

        devShells.default = pkgs.mkShell {
          packages = [ pkgs.cmake pkgs.clang pkgs.go pkgs.templ ];
        };
      }
    );
}
