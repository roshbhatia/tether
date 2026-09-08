export extern "tether" [
  --help(-h) # Print command help
  --version # Write the version to stdout
  ...args: string@"__tether_completion_none"
]

export extern "tether completion" [
  shell: string@"nu-complete tether shell"
]

def "nu-complete tether shell" [] { [bash zsh fish nu] }

export extern "tether plan" [
  --host: string # ssh host alias
  --session: string # Session name echoed in the output
  --native: string # Opaque ref of the caller's native mux domain
  --native-raw: string # Opaque ref of the caller's raw ssh domain
  --mode: string@"__tether_completion_values_0" # Ordering mode; overrides the config
  --pin: string@"__tether_completion_values_1" # Tier that must win; overrides the config
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

export extern "tether probe" [
  --host: string # ssh host alias
  --force # Probe even inside the unreachable-host backoff
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

export extern "tether status" [
  --host: string # Only this host
  --help(-h) # Print command help
  ...args: string@"__tether_completion_none"
]

def "__tether_completion_none" [] { [] }

def "__tether_completion_values_0" [context?: string] {
  [
    "auto"
    "native"
    "roam"
    "persist"
  ] | flatten | uniq
}

def "__tether_completion_values_1" [context?: string] {
  [
    "native-mux"
    "mosh-mux"
    "ssh-raw"
    "ssh"
  ] | flatten | uniq
}