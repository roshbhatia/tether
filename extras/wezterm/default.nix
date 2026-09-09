{ pkgs, core }:
let
  executable = core.overrideAttrs (old: {
    pname = "tether-picker";
    subPackages = [ "extras/wezterm" ];
    nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ pkgs.makeWrapper ];
    checkPhase = ''
      runHook preCheck
      go test -race ./extras/wezterm
      runHook postCheck
    '';
    postInstall = ''
      mv "$out/bin/wezterm" "$out/bin/tether-picker"
      wrapProgram "$out/bin/tether-picker" --add-flags "${pkgs.lib.getExe core}"
    '';
    meta = old.meta // {
      mainProgram = "tether-picker";
    };
  });
  manifest = pkgs.writeText "tether.json" (
    builtins.toJSON (
      (builtins.fromJSON (builtins.readFile ./provider.json))
      // {
        command = [ (pkgs.lib.getExe executable) ];
      }
    )
  );
in
pkgs.symlinkJoin {
  name = "tether-picker";
  paths = [ executable ];
  postBuild = "mkdir -p $out/share/wezterm/providers; cp ${manifest} $out/share/wezterm/providers/tether.json";
  meta.mainProgram = "tether-picker";
}
