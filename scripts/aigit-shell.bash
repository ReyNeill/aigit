# Aigit shell integration for bash
# Prints live Aigit events (AI summaries, checkpoints, applies) automatically
# by running a background follower in each Git repo you cd into.

# Usage: source this file from your ~/.bashrc or ~/.bash_profile
#   source /path/to/aigit/scripts/aigit-shell.bash

_AIGIT_EVENTS_PID=""
_AIGIT_EVENTS_ROOT=""

_aigit_watch_repo() {
  local top
  top=$(git rev-parse --show-toplevel 2>/dev/null) || top=""
  if [[ -z "$top" ]]; then
    if [[ -n "$_AIGIT_EVENTS_PID" ]]; then kill "$_AIGIT_EVENTS_PID" 2>/dev/null; _AIGIT_EVENTS_PID=""; _AIGIT_EVENTS_ROOT=""; fi
    return 0
  fi
  if [[ "$top" == "$_AIGIT_EVENTS_ROOT" ]] && kill -0 "$_AIGIT_EVENTS_PID" 2>/dev/null; then
    return 0
  fi
  if [[ -n "$_AIGIT_EVENTS_PID" ]]; then kill "$_AIGIT_EVENTS_PID" 2>/dev/null; fi
  _AIGIT_EVENTS_ROOT="$top"
  local sid
  sid="bash:${HOSTNAME}:${TTY}:$$:$top"
  (cd "$top" && aigit events -id "$sid" -n 50 --follow) &
  _AIGIT_EVENTS_PID=$!
}

# Hook into PROMPT_COMMAND without clobbering existing content
if [[ -n "$PROMPT_COMMAND" ]]; then
  PROMPT_COMMAND="_aigit_watch_repo; $PROMPT_COMMAND"
else
  PROMPT_COMMAND="_aigit_watch_repo"
fi

