export extern "tether" [
  --help(-h) # Print command help
  --version # Write the version to stdout
  ...args: string@"__tether_completion_none"
]

export extern "tether completion" [
  shell: string@"nu-complete tether shell"
]

def "nu-complete tether shell" [] { [bash zsh fish nu] }

export extern "tether connect" [
  --session(-s): string # Attach this remote mux session (zmx, else tmux) instead of a login shell
  --quiet(-q) # Do not announce the winning hop on stderr
  --login(-l): string # Remote user, as ssh -l
  --port(-p): string # Remote port, as ssh -p
  --mode: string@"__tether_completion_values_1" # Ordering mode; overrides the config
  --pin: string@"__tether_completion_values_2" # Tier that must win; overrides the config
  --dry-run # Write the tether.plan/v1 document and exit without connecting
  --no-probe # Never run the ssh round trip; plan from the cache only
  --version # Write the version to stdout
  --help(-h) # Print command help
  ...args: string@"__tether_completion_values_0"
]

export extern "tether plan" [
  --host: string@"__tether_completion_values_3" # ssh config alias or tailnet peer
  --session: string # Session name echoed in the output
  --native: string # Opaque ref of the caller's native mux domain
  --native-raw: string # Opaque ref of the caller's raw ssh domain
  --mode: string@"__tether_completion_values_4" # Ordering mode; overrides the config
  --pin: string@"__tether_completion_values_5" # Tier that must win; overrides the config
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

export extern "tether probe" [
  --host: string@"__tether_completion_values_6" # ssh config alias or tailnet peer
  --force # Probe even inside the unreachable-host backoff
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

export extern "tether hosts" [
  --json # Write tether.hosts/v1 instead of a table
  --names # Write one name per line, for completion
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

export extern "tether status" [
  --host: string@"__tether_completion_values_7" # Only this host
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

def "__tether_completion_none" [] { [] }

def "__tether_completion_values_0" [context?: string] {
  [
    (try { run-external "tether" "hosts" "--names" | lines } catch { [] })
  ] | flatten | uniq
}

def "__tether_completion_values_1" [context?: string] {
  [
    "auto"
    "native"
    "roam"
    "persist"
  ] | flatten | uniq
}

def "__tether_completion_values_2" [context?: string] {
  [
    "native-mux"
    "mosh-mux"
    "ssh-raw"
    "ssh"
  ] | flatten | uniq
}

def "__tether_completion_values_3" [context?: string] {
  [
    (try { run-external "tether" "hosts" "--names" | lines } catch { [] })
  ] | flatten | uniq
}

def "__tether_completion_values_4" [context?: string] {
  [
    "auto"
    "native"
    "roam"
    "persist"
  ] | flatten | uniq
}

def "__tether_completion_values_5" [context?: string] {
  [
    "native-mux"
    "mosh-mux"
    "ssh-raw"
    "ssh"
  ] | flatten | uniq
}

def "__tether_completion_values_6" [context?: string] {
  [
    (try { run-external "tether" "hosts" "--names" | lines } catch { [] })
  ] | flatten | uniq
}

def "__tether_completion_values_7" [context?: string] {
  [
    (try { run-external "tether" "hosts" "--names" | lines } catch { [] })
  ] | flatten | uniq
}