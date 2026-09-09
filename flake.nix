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
          version = "0.3.0";
          # Refresh with `nix build` after any go.mod or go.sum change; the
          # build prints the hash it expected.
          vendorHash = "sha256-ZLnyCDvIhS5mu8FCCTqQakqJ/5Smu3A/5058kh2jELE=";
          tether = pkgs.buildGoModule {
            pname = "tether";
            inherit version vendorHash;
            src = ./.;
            subPackages = [
              "cmd/tether"
              "cmd/tsh"
            ];
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
              for name in tether tsh; do
                bash -n "completions/$name.bash"
                fish --no-config -n "completions/$name.fish"
                nu --no-config-file --no-std-lib -c "source completions/$name.nu"
                zsh -n "completions/_$name"
              done
              runHook postCheck
            '';
            ldflags = [ "-s -w -X main.version=${version}" ];
            postInstall = ''
              for name in tether tsh; do
                installShellCompletion --cmd "$name" \
                  --bash "completions/$name.bash" \
                  --fish "completions/$name.fish" \
                  --zsh "completions/_$name"
                mkdir -p "$out/share/nushell/vendor/autoload"
                install -m 0444 "completions/$name.nu" "$out/share/nushell/vendor/autoload/$name.nu"
              done
              mkdir -p "$out/share/tether/schema"
              cp schema/*.json "$out/share/tether/schema/"
            '';
            meta = {
              description = "tsh: ssh with the hop (native mux, mosh, ssh) negotiated; tether is its plumbing";
              homepage = "https://github.com/roshbhatia/tether";
              license = pkgs.lib.licenses.mit;
              mainProgram = "tether";
              platforms = pkgs.lib.platforms.unix;
            };
          };
        in
        {
          inherit tether;
          provider-wezterm = import ./extras/wezterm {
            inherit pkgs;
            core = tether;
          };
          extras = self.packages.${system}.provider-wezterm;
          full = pkgs.symlinkJoin {
            name = "tether-full";
            paths = [
              tether
              self.packages.${system}.extras
            ];
            meta = tether.meta;
          };
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
              pkgs.python3
              pkgs.vhs
              pkgs.ffmpeg
              pkgs.git
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
