# Aigit shell integration for zsh
# Prints new Aigit events (AI summaries, checkpoints, applies) on each prompt.

# Usage: source this file from your ~/.zshrc
#   source /path/to/repo/scripts/aigit-shell.zsh

typeset -g AIGIT_EVENTS_PID=""
typeset -g AIGIT_EVENTS_ROOT=""

function _aigit_watch_repo() {
  local top
  top=$(git rev-parse --show-toplevel 2>/dev/null) || top=""
  if [[ -z "$top" ]]; then
    # Not in a repo; stop follower if running
    if [[ -n "$AIGIT_EVENTS_PID" ]]; then kill "$AIGIT_EVENTS_PID" 2>/dev/null; AIGIT_EVENTS_PID=""; AIGIT_EVENTS_ROOT=""; fi
    return 0
  fi
  if [[ "$top" == "$AIGIT_EVENTS_ROOT" ]] && kill -0 "$AIGIT_EVENTS_PID" 2>/dev/null; then
    return 0
  fi
  # Repo changed or follower missing; restart
  if [[ -n "$AIGIT_EVENTS_PID" ]]; then kill "$AIGIT_EVENTS_PID" 2>/dev/null; fi
  AIGIT_EVENTS_ROOT="$top"
  local sid
  sid="zsh:${HOST}:${TTY}:${$}:$top"
  (cd "$top" && aigit events -id "$sid" -n 50 --follow) &!
  AIGIT_EVENTS_PID=$!
}

autoload -Uz add-zsh-hook
add-zsh-hook precmd _aigit_watch_repo
