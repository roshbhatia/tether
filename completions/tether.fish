complete -c tether -e
complete -c tether -f
function __tether_completion_values_0
  begin
    command 'tether' 'hosts' '--names' 2>/dev/null; or true
  end | string match -rv '\t'; or true
end
function __tether_completion_values_1
  begin
    printf '%s\n' 'auto' 'native' 'roam' 'persist'
  end | string match -rv '\t'; or true
end
function __tether_completion_values_2
  begin
    printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
  end | string match -rv '\t'; or true
end
function __tether_completion_values_3
  begin
    command 'tether' 'hosts' '--names' 2>/dev/null; or true
  end | string match -rv '\t'; or true
end
function __tether_completion_values_4
  begin
    printf '%s\n' 'auto' 'native' 'roam' 'persist'
  end | string match -rv '\t'; or true
end
function __tether_completion_values_5
  begin
    printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
  end | string match -rv '\t'; or true
end
function __tether_completion_values_6
  begin
    command 'tether' 'hosts' '--names' 2>/dev/null; or true
  end | string match -rv '\t'; or true
end
function __tether_completion_values_7
  begin
    command 'tether' 'hosts' '--names' 2>/dev/null; or true
  end | string match -rv '\t'; or true
end

function __tether_completion_context
  set -l context ''
  set -l words (commandline -opc)
  set -l consume_value 0
  set -l options_done 0
  for word in $words[2..-1]
    if test $consume_value -eq 1
      set consume_value 0
      continue
    end
    if test $options_done -eq 1
      continue
    end
    if test "$word" = '--'
      set options_done 1
      continue
    end
    switch "$context:$word"
      case 'connect:--session'
        set consume_value 1
        continue
      case 'connect:--session=*'
        continue
      case 'connect:--mode'
        set consume_value 1
        continue
      case 'connect:--mode=*'
        continue
      case 'connect:--pin'
        set consume_value 1
        continue
      case 'connect:--pin=*'
        continue
      case 'plan:--host'
        set consume_value 1
        continue
      case 'plan:--host=*'
        continue
      case 'plan:--session'
        set consume_value 1
        continue
      case 'plan:--session=*'
        continue
      case 'plan:--native'
        set consume_value 1
        continue
      case 'plan:--native=*'
        continue
      case 'plan:--native-raw'
        set consume_value 1
        continue
      case 'plan:--native-raw=*'
        continue
      case 'plan:--mode'
        set consume_value 1
        continue
      case 'plan:--mode=*'
        continue
      case 'plan:--pin'
        set consume_value 1
        continue
      case 'plan:--pin=*'
        continue
      case 'probe:--host'
        set consume_value 1
        continue
      case 'probe:--host=*'
        continue
      case 'status:--host'
        set consume_value 1
        continue
      case 'status:--host=*'
        continue
    end
    switch "$context:$word"
      case ':completion'
        set context 'completion'
      case ':connect'
        set context 'connect'
      case ':plan'
        set context 'plan'
      case ':probe'
        set context 'probe'
      case ':hosts'
        set context 'hosts'
      case ':status'
        set context 'status'
      case ':completion'
        set context 'completion'
    end
  end
  echo $context
end
complete -c tether -n 'test (__tether_completion_context) = ""' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = ""' -l version -d 'Write the version to stdout'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a completion -d 'Generate shell completions'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a connect -d 'tether connect <host> [--session <name>] [--mode <mode>] [--pin <tier>] [--dry-run] [--quiet] [--no-probe] [-- <inner argv>]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a plan -d 'tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a probe -d 'tether probe --host <host> [--force]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a hosts -d 'tether hosts [--json] [--names]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a status -d 'tether status [--host <host>]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a completion -d 'tether completion <bash|fish|nu|zsh>'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -l session -r -d 'Session to attach with zmx or tmux when no inner argv is given; overrides defaults.session'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -f -l mode -r -a '(__tether_completion_values_1)' -d 'Ordering mode; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -f -l pin -r -a '(__tether_completion_values_2)' -d 'Tier that must win; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -l dry-run -d 'Write the plan JSON and exit without connecting'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -l quiet -d 'Do not announce the winning hop on stderr'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -l no-probe -d 'Never run the ssh round trip; plan from the cache only'
complete -c tether -n 'test (__tether_completion_context) = "connect"' -l help -s h -d 'Print command help'
complete -c tether -f -n 'test (__tether_completion_context) = "connect"' -a '(__tether_completion_values_0)'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -f -l host -r -a '(__tether_completion_values_3)' -d 'ssh config alias or tailnet peer'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l session -r -d 'Session name echoed in the output'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l native -r -d 'Opaque ref of the caller\'s native mux domain'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l native-raw -r -d 'Opaque ref of the caller\'s raw ssh domain'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -f -l mode -r -a '(__tether_completion_values_4)' -d 'Ordering mode; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -f -l pin -r -a '(__tether_completion_values_5)' -d 'Tier that must win; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -f -l host -r -a '(__tether_completion_values_6)' -d 'ssh config alias or tailnet peer'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -l force -d 'Probe even inside the unreachable-host backoff'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = "hosts"' -l json -d 'Write tether.hosts/v1 instead of a table'
complete -c tether -n 'test (__tether_completion_context) = "hosts"' -l names -d 'Write one name per line, for completion'
complete -c tether -n 'test (__tether_completion_context) = "hosts"' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = "status"' -f -l host -r -a '(__tether_completion_values_7)' -d 'Only this host'
complete -c tether -n 'test (__tether_completion_context) = "status"' -l help -s h -d 'Print command help'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'