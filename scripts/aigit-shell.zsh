# Aigit shell integration for zsh
# Prints new Aigit events (AI summaries, checkpoints, applies) on each prompt.

# Usage: source this file from your ~/.zshrc
#   source /path/to/repo/scripts/aigit-shell.zsh

function _aigit_print_events() {
  # Find repo top-level; if not in a git repo, skip.
  local top
  top=$(git rev-parse --show-toplevel 2>/dev/null) || return 0
  # Unique session id per shell/tty/host
  local sid
  sid="zsh:${HOST}:${TTY}:${$}"
  # Print new events since last prompt
  aigit events -id "$sid" -n 80 2>/dev/null
}

autoload -Uz add-zsh-hook
add-zsh-hook precmd _aigit_print_events

