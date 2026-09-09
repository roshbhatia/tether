__tether_completion_values_0() {
  'tether' 'hosts' '--names' 2>/dev/null || true
}
__tether_completion_values_1() {
  printf '%s\n' 'auto' 'native' 'roam' 'persist'
}
__tether_completion_values_2() {
  printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
}
__tether_completion_values_3() {
  'tether' 'hosts' '--names' 2>/dev/null || true
}
__tether_completion_values_4() {
  printf '%s\n' 'auto' 'native' 'roam' 'persist'
}
__tether_completion_values_5() {
  printf '%s\n' 'native-mux' 'mosh-mux' 'ssh-raw' 'ssh'
}
__tether_completion_values_6() {
  'tether' 'hosts' '--names' 2>/dev/null || true
}
__tether_completion_values_7() {
  'tether' 'hosts' '--names' 2>/dev/null || true
}
__tether_completion_filter() {
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

_tether_complete() {
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
      'connect:--session') consume_value=1; continue ;;
      'connect:--session='*) continue ;;
      'connect:--mode') consume_value=1; continue ;;
      'connect:--mode='*) continue ;;
      'connect:--pin') consume_value=1; continue ;;
      'connect:--pin='*) continue ;;
      'plan:--host') consume_value=1; continue ;;
      'plan:--host='*) continue ;;
      'plan:--session') consume_value=1; continue ;;
      'plan:--session='*) continue ;;
      'plan:--native') consume_value=1; continue ;;
      'plan:--native='*) continue ;;
      'plan:--native-raw') consume_value=1; continue ;;
      'plan:--native-raw='*) continue ;;
      'plan:--mode') consume_value=1; continue ;;
      'plan:--mode='*) continue ;;
      'plan:--pin') consume_value=1; continue ;;
      'plan:--pin='*) continue ;;
      'probe:--host') consume_value=1; continue ;;
      'probe:--host='*) continue ;;
      'status:--host') consume_value=1; continue ;;
      'status:--host='*) continue ;;
    esac
    case "$context:$word" in
      ':completion') context='completion' ;;
      ':connect') context='connect' ;;
      ':plan') context='plan' ;;
      ':probe') context='probe' ;;
      ':hosts') context='hosts' ;;
      ':status') context='status' ;;
      ':completion') context='completion' ;;
    esac
  done
  case "$context:$previous" in
    'connect:--mode') __tether_completion_filter "$current" < <(__tether_completion_values_1); return ;;
    'connect:--pin') __tether_completion_filter "$current" < <(__tether_completion_values_2); return ;;
    'plan:--host') __tether_completion_filter "$current" < <(__tether_completion_values_3); return ;;
    'plan:--mode') __tether_completion_filter "$current" < <(__tether_completion_values_4); return ;;
    'plan:--pin') __tether_completion_filter "$current" < <(__tether_completion_values_5); return ;;
    'probe:--host') __tether_completion_filter "$current" < <(__tether_completion_values_6); return ;;
    'status:--host') __tether_completion_filter "$current" < <(__tether_completion_values_7); return ;;
  esac
  case "$context:$current" in
    'connect:--mode='*) __tether_completion_filter "${current#*=}" "--mode=" < <(__tether_completion_values_1); return ;;
    'connect:--pin='*) __tether_completion_filter "${current#*=}" "--pin=" < <(__tether_completion_values_2); return ;;
    'plan:--host='*) __tether_completion_filter "${current#*=}" "--host=" < <(__tether_completion_values_3); return ;;
    'plan:--mode='*) __tether_completion_filter "${current#*=}" "--mode=" < <(__tether_completion_values_4); return ;;
    'plan:--pin='*) __tether_completion_filter "${current#*=}" "--pin=" < <(__tether_completion_values_5); return ;;
    'probe:--host='*) __tether_completion_filter "${current#*=}" "--host=" < <(__tether_completion_values_6); return ;;
    'status:--host='*) __tether_completion_filter "${current#*=}" "--host=" < <(__tether_completion_values_7); return ;;
  esac
  case "$context" in
    '')
      __tether_completion_filter "$current" < <(
        printf '%s\n' 'completion' 'connect' 'plan' 'probe' 'hosts' 'status' 'completion' '--help' '-h' '--version'
      )
      ;;
    'completion')
      __tether_completion_filter "$current" < <(
        printf '%s\n' 'bash' 'zsh' 'fish' 'nu'
      )
      ;;
    'connect')
      __tether_completion_filter "$current" < <(
        printf '%s\n' '--session' '--mode' '--pin' '--dry-run' '--quiet' '--no-probe' '--help' '-h'
        __tether_completion_values_0
      )
      ;;
    'plan')
      __tether_completion_filter "$current" < <(
        printf '%s\n' '--host' '--session' '--native' '--native-raw' '--mode' '--pin' '--help' '-h'
      )
      ;;
    'probe')
      __tether_completion_filter "$current" < <(
        printf '%s\n' '--host' '--force' '--help' '-h'
      )
      ;;
    'hosts')
      __tether_completion_filter "$current" < <(
        printf '%s\n' '--json' '--names' '--help' '-h'
      )
      ;;
    'status')
      __tether_completion_filter "$current" < <(
        printf '%s\n' '--host' '--help' '-h'
      )
      ;;
    'completion')
      __tether_completion_filter "$current" < <(
      )
      ;;
  esac
}
complete -F _tether_complete tether