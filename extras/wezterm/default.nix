{ pkgs, core }:
let
  executable = pkgs.writeShellApplication {
    name = "tether-picker";
    runtimeInputs = [ pkgs.python3 ];
    text = "exec python3 ${./provider.py} ${pkgs.lib.getExe core}";
  };
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
