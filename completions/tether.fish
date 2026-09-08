complete -c tether -e
complete -c tether -f
function __tether_completion_values_0
  begin
    printf '%s\n' 'auto' 'native' 'roam' 'persist'
  end | string match -rv '\t'; or true
end
function __tether_completion_values_1
  begin
    printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
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
      case ':plan'
        set context 'plan'
      case ':probe'
        set context 'probe'
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
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a plan -d 'tether plan --host <host> [--session <name>] [--native <ref>] [--native-raw <ref>] [--mode <mode>] [--pin <tier>] [-- <inner argv>]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a probe -d 'tether probe --host <host> [--force]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a status -d 'tether status [--host <host>]'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a completion -d 'tether completion <bash|fish|nu|zsh>'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l host -r -d 'ssh host alias'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l session -r -d 'Session name echoed in the output'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l native -r -d 'Opaque ref of the caller\'s native mux domain'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l native-raw -r -d 'Opaque ref of the caller\'s raw ssh domain'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -f -l mode -r -a '(__tether_completion_values_0)' -d 'Ordering mode; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -f -l pin -r -a '(__tether_completion_values_1)' -d 'Tier that must win; overrides the config'
complete -c tether -n 'test (__tether_completion_context) = "plan"' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -l host -r -d 'ssh host alias'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -l force -d 'Probe even inside the unreachable-host backoff'
complete -c tether -n 'test (__tether_completion_context) = "probe"' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = "status"' -l host -r -d 'Only this host'
complete -c tether -n 'test (__tether_completion_context) = "status"' -l help -s h -d 'Print command help'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'