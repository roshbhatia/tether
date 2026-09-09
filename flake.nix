{
  description = "Transport negotiator for remote sessions";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    { self, nixpkgs, ... }:
    let
      supportedSystems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      eachSystem = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      formatter = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        pkgs.writeShellApplication {
          name = "tether-format";
          runtimeInputs = [
            pkgs.fd
            pkgs.nixfmt
          ];
          text = ''
            if [ "$#" -gt 0 ] && [ "''${1#-}" = "$1" ]; then
              exec nixfmt "$@"
            fi
            exec fd --extension nix --type file --exec-batch nixfmt "$@"
          '';
        }
      );

      packages = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          version = "0.2.0";
          # Refresh with `nix build` after any go.mod or go.sum change; the
          # build prints the hash it expected.
          vendorHash = "sha256-ZLnyCDvIhS5mu8FCCTqQakqJ/5Smu3A/5058kh2jELE=";
          tether = pkgs.buildGoModule {
            pname = "tether";
            inherit version vendorHash;
            src = ./.;
            subPackages = [ "cmd/tether" ];
            nativeBuildInputs = [ pkgs.installShellFiles ];
            nativeCheckInputs = [
              pkgs.bash
              pkgs.fish
              pkgs.nushell
              pkgs.zsh
            ];
            checkPhase = ''
              runHook preCheck
              export HOME="$TMPDIR/home"
              mkdir -p "$HOME"
              go vet ./...
              go test -race ./...
              ${pkgs.bash}/bin/bash ./hack/generate.sh --check
              bash -n completions/tether.bash
              fish --no-config -n completions/tether.fish
              nu --no-config-file --no-std-lib -c 'source completions/tether.nu'
              zsh -n completions/_tether
              runHook postCheck
            '';
            ldflags = [ "-s -w -X main.version=${version}" ];
            postInstall = ''
              installShellCompletion --cmd tether \
                --bash completions/tether.bash \
                --fish completions/tether.fish \
                --zsh completions/_tether
              mkdir -p "$out/share/nushell/vendor/autoload"
              install -m 0444 completions/tether.nu "$out/share/nushell/vendor/autoload/tether.nu"
              mkdir -p "$out/share/tether/schema"
              cp schema/*.json "$out/share/tether/schema/"
            '';
            meta = {
              description = "Transport negotiator: picks the hop (native mux, mosh, ssh) for a remote session";
              homepage = "https://github.com/roshbhatia/tether";
              license = pkgs.lib.licenses.mit;
              mainProgram = "tether";
              platforms = pkgs.lib.platforms.unix;
            };
          };
        in
        {
          inherit tether;
          default = tether;
        }
      );

      apps = eachSystem (system: {
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.default}";
        };
      });

      checks = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = self.packages.${system}.default;
          repository =
            pkgs.runCommand "tether-repository-check"
              {
                nativeBuildInputs = [
                  pkgs.actionlint
                  pkgs.shellcheck
                  pkgs.shfmt
                ];
              }
              ''
                actionlint ${./.github/workflows/ci.yml} ${./.github/workflows/release.yml}
                shellcheck ${./hack/generate.sh}
                shfmt -i 2 -ci -sr -s -d ${./hack/generate.sh}
                touch "$out"
              '';
        }
      );

      devShells = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShellNoCC {
            packages = [
              pkgs.go
              pkgs.actionlint
              pkgs.bash
              pkgs.fish
              pkgs.gopls
              pkgs.gotools
              pkgs.go-tools
              pkgs.goreleaser
              pkgs.jq
              pkgs.nushell
              pkgs.shellcheck
              pkgs.shfmt
              pkgs.zsh
            ];
            shellHook = ''
              export GOTOOLCHAIN=local
            '';
          };
        }
      );
    };
}
