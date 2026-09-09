# wezterm

Select a development host for the repair task.

## Install

```sh
brew install roshbhatia/tap/tether-provider-wezterm
nix profile add 'github:roshbhatia/tether#provider-wezterm'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Select a development host for the repair task](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/wezterm/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py wezterm` to record it.
