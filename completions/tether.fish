complete -c tether -e
complete -c tether -f

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
    end
    switch "$context:$word"
      case ':completion'
        set context 'completion'
      case ':completion'
        set context 'completion'
    end
  end
  echo $context
end
complete -c tether -n 'test (__tether_completion_context) = ""' -l help -s h -d 'Print command help'
complete -c tether -n 'test (__tether_completion_context) = ""' -l json -d 'Write JSON to stdout'
complete -c tether -n 'test (__tether_completion_context) = ""' -l version -d 'Write the version to stdout'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a completion -d 'Generate shell completions'
complete -c tether -f -n 'test (__tether_completion_context) = ""' -a completion -d 'tether completion <bash|fish|nu|zsh>'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'
complete -c tether -f -n 'test (__tether_completion_context) = "completion"' -a 'bash zsh fish nu'