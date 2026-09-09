__tsh_completion_values_0() {
  printf '%s\n' 'auto' 'native' 'roam' 'persist'
}
__tsh_completion_values_1() {
  printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
}
__tsh_completion_values_2() {
  printf '%s\n' 'completion'
  'tether' 'hosts' '--names' 2>/dev/null || true
}
__tsh_completion_filter() {
  local prefix="$1"
  local prepend="${2-}"
  local candidate
  local existing
  local duplicate
  COMPREPLY=()
  while IFS= read -r candidate || [[ -n "$candidate" ]]; do
    [[ "$candidate" == "$prefix"* ]] || continue
    candidate="$prepend$candidate"
    duplicate=0
    for existing in "${COMPREPLY[@]}"; do
      if [[ "$existing" == "$candidate" ]]; then
        duplicate=1
        break
      fi
    done
    (( duplicate )) || COMPREPLY+=("$candidate")
  done
}

_tsh_complete() {
  local current="${COMP_WORDS[COMP_CWORD]}"
  local previous=""
  local context=""
  local word
  local index
  local consume_value=0
  local options_done=0
  if (( COMP_CWORD > 0 )); then
    previous="${COMP_WORDS[COMP_CWORD-1]}"
  fi
  for ((index=1; index<COMP_CWORD; index++)); do
    word="${COMP_WORDS[index]}"
    if (( consume_value )); then
      consume_value=0
      continue
    fi
    if (( options_done )); then
      continue
    fi
    if [[ "$word" == '--' ]]; then
      options_done=1
      continue
    fi
    case "$context:$word" in
      ':--login') consume_value=1; continue ;;
      ':--login='*) continue ;;
      ':-l') consume_value=1; continue ;;
      ':-l='*) continue ;;
      ':--mode') consume_value=1; continue ;;
      ':--mode='*) continue ;;
      ':--pin') consume_value=1; continue ;;
      ':--pin='*) continue ;;
      ':--port') consume_value=1; continue ;;
      ':--port='*) continue ;;
      ':-p') consume_value=1; continue ;;
      ':-p='*) continue ;;
      ':--session') consume_value=1; continue ;;
      ':--session='*) continue ;;
      ':-s') consume_value=1; continue ;;
      ':-s='*) continue ;;
    esac
    case "$context:$word" in
      ':completion') context='completion' ;;
    esac
  done
  case "$context:$previous" in
    ':--mode') __tsh_completion_filter "$current" < <(__tsh_completion_values_0); return ;;
    ':--pin') __tsh_completion_filter "$current" < <(__tsh_completion_values_1); return ;;
  esac
  case "$context:$current" in
    ':--mode='*) __tsh_completion_filter "${current#*=}" "--mode=" < <(__tsh_completion_values_0); return ;;
    ':--pin='*) __tsh_completion_filter "${current#*=}" "--pin=" < <(__tsh_completion_values_1); return ;;
  esac
  case "$context" in
    '')
      __tsh_completion_filter "$current" < <(
        printf '%s\n' 'completion' '--dry-run' '--help' '-h' '--login' '-l' '--mode' '--no-probe' '--pin' '--port' '-p' '--quiet' '-q' '--session' '-s' '--version'
        __tsh_completion_values_2
      )
      ;;
    'completion')
      __tsh_completion_filter "$current" < <(
        printf '%s\n' 'bash' 'zsh' 'fish' 'nu'
      )
      ;;
  esac
}
complete -F _tsh_complete tsh