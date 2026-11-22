# wezterm

Open remote hosts from the WezTerm picker.

## Install

```sh
brew install roshbhatia/tap/tether-provider-wezterm
nix profile add 'github:roshbhatia/tether#provider-wezterm'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

Each picker row represents a logical host. Selecting a row checks its ordered SSH addresses and attaches the corresponding native mux domain when available. Other hosts open a local transport pane.

## Demo

Recording pending. The previous script printed adapter output without exercising WezTerm.

[Tape source](demo.tape)
