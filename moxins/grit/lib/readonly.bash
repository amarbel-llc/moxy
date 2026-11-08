# Shared guard for the read-only grit tools (moxy#510): status, diff, log,
# rev-parse, tag-list, worktree-list. Sourced, never run on its own. It lives
# in lib/, not bin/: the moxin build wraps every file in bin/ in an exec
# wrapper, and sourcing a wrapper would replace the sourcing script.
#
# What makes these read-only by construction:
#   - each tool's script runs exactly one fixed git read verb; nothing the
#     caller sends selects the verb;
#   - every caller string that reaches git is a value (a ref, a pathspec, a
#     number, a glob) and refuses a leading "-", so it cannot become an option
#     such as --output=FILE (which makes `git diff` and `git log` write);
#   - no repo_path: the tool reads the repository of the session's working
#     directory, so it is not a way to read other repositories;
#   - git is told not to take optional locks (a plain `git status` rewrites
#     the index to refresh stat data) and not to run an fsmonitor hook.
#
# Not covered, and out of scope: config the repository itself carries (a
# textconv or external-diff driver, hooks that read-only verbs do not run).
# The diff tool turns the two diff drivers off.

export GIT_OPTIONAL_LOCKS=0
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0=core.fsmonitor
export GIT_CONFIG_VALUE_0=false

# refuse_option NAME VALUE: a caller-supplied value must not look like a flag.
refuse_option() {
  case "$2" in
    -*)
      echo "$1 must not start with '-': '$2'" >&2
      exit 2
      ;;
  esac
}

# refuse_non_number NAME VALUE: an integer argument is digits only.
refuse_non_number() {
  case "$2" in
    '' | *[!0-9]*)
      echo "$1 must be a non-negative integer: '$2'" >&2
      exit 2
      ;;
  esac
}

# read_paths JSON: fill the `files` array from a JSON array of pathspecs,
# refusing any that starts with "-" (they follow `--`, but stay uniform).
read_paths() {
  files=()
  if [ -n "$1" ] && [ "$1" != "null" ]; then
    local p
    while IFS= read -r p; do
      refuse_option "paths entry" "$p"
      files+=("$p")
    done < <(printf '%s' "$1" | jq -r '.[]')
  fi
}
