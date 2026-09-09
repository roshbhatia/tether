export extern "tsh" [
  --dry-run # Write the tether.plan/v1 document and exit without connecting
  --help(-h) # Print command help
  --login(-l): string # Remote user, as ssh -l
  --mode: string@"__tsh_completion_values_0" # Ordering mode; overrides the config
  --no-probe # Never run the ssh round trip; plan from the cache only
  --pin: string@"__tsh_completion_values_1" # Tier that must win; overrides the config
  --port(-p): string # Remote port, as ssh -p
  --quiet(-q) # Do not announce the winning hop on stderr
  --session(-s): string # Attach this remote mux session (zmx, else tmux) instead of a login shell
  --version # Write the version to stdout
  ...args: string@"__tsh_completion_values_2"
]

export extern "tsh completion" [
  shell: string@"nu-complete tsh shell"
]

def "nu-complete tsh shell" [] { [bash zsh fish nu] }

def "__tsh_completion_none" [] { [] }

def "__tsh_completion_values_0" [context?: string] {
  [
    "auto"
    "native"
    "roam"
    "persist"
  ] | flatten | uniq
}

def "__tsh_completion_values_1" [context?: string] {
  [
    "native-mux"
    "mosh-mux"
    "ssh-raw"
    "ssh"
  ] | flatten | uniq
}

def "__tsh_completion_values_2" [context?: string] {
  [
    "completion"
    (try { run-external "tether" "hosts" "--names" | lines } catch { [] })
  ] | flatten | uniq
}