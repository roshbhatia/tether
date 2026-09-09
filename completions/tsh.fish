complete -c tsh -e
complete -c tsh -f
function __tsh_completion_values_0
  begin
    printf '%s\n' 'auto' 'native' 'roam' 'persist'
  end | string match -rv '\t'; or true
end
function __tsh_completion_values_1
  begin
    printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
  end | string match -rv '\t'; or true
end
function __tsh_completion_values_2
  begin
    printf '%s\n' 'completion'
    command 'tether' 'hosts' '--names' 2>/dev/null; or true
  end | string match -rv '\t'; or true
end

function __tsh_completion_context
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
      case ':--login' ':-l'
        set consume_value 1
        continue
      case ':--login=*'
        continue
      case ':-l=*'
        continue
      case ':--mode'
        set consume_value 1
        continue
      case ':--mode=*'
        continue
      case ':--pin'
        set consume_value 1
        continue
      case ':--pin=*'
        continue
      case ':--port' ':-p'
        set consume_value 1
        continue
      case ':--port=*'
        continue
      case ':-p=*'
        continue
      case ':--session' ':-s'
        set consume_value 1
        continue
      case ':--session=*'
        continue
      case ':-s=*'
        continue
    end
    switch "$context:$word"
      case ':completion'
        set context 'completion'
    end
  end
  echo $context
end
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l dry-run -d 'Write the tether.plan/v1 document and exit without connecting'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l help -s h -d 'Print command help'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l login -s l -r -d 'Remote user, as ssh -l'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -f -l mode -r -a '(__tsh_completion_values_0)' -d 'Ordering mode; overrides the config'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l no-probe -d 'Never run the ssh round trip; plan from the cache only'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -f -l pin -r -a '(__tsh_completion_values_1)' -d 'Tier that must win; overrides the config'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l port -s p -r -d 'Remote port, as ssh -p'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l quiet -s q -d 'Do not announce the winning hop on stderr'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l session -s s -r -d 'Attach this remote mux session (zmx, else tmux) instead of a login shell'
complete -c tsh -n 'test (__tsh_completion_context) = ""' -l version -d 'Write the version to stdout'
complete -c tsh -f -n 'test (__tsh_completion_context) = ""' -a completion -d 'Generate shell completions'
complete -c tsh -f -n 'test (__tsh_completion_context) = ""' -a '(__tsh_completion_values_2)'
complete -c tsh -f -n 'test (__tsh_completion_context) = "completion"' -a 'bash zsh fish nu'