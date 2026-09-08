export extern "tether" [
  --help(-h) # Print command help
  --json # Write JSON to stdout
  --version # Write the version to stdout
  ...args: string@"__tether_completion_none"
]

export extern "tether completion" [
  shell: string@"nu-complete tether shell"
]

def "nu-complete tether shell" [] { [bash zsh fish nu] }

def "__tether_completion_none" [] { [] }