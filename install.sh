#!/bin/sh

set -eu

REPOSITORY="github.com/teddymalhan/gl-stack"
VERSION="${GL_STACK_VERSION:-latest}"
INSTALL_DIR="${GL_STACK_INSTALL_DIR:-$HOME/.local/bin}"
DATA_DIR="${GL_STACK_DATA_DIR:-$HOME/.local/share/gl-stack}"
SHELL_SETUP="${GL_STACK_SHELL_SETUP:-1}"

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'gl-stack installer: %s\n' "$*" >&2
  exit 1
}

command -v go >/dev/null 2>&1 || fail "Go is required. Install it from https://go.dev/dl/ and run this command again."
command -v git >/dev/null 2>&1 || fail "Git is required. Install Git and run this command again."

mkdir -p "$INSTALL_DIR" "$DATA_DIR/completions"

# A checked-out copy installs its current source. A script piped to sh installs the
# requested module version instead.
SCRIPT_DIR=""
case "$0" in
  */*) SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd) || SCRIPT_DIR="" ;;
esac

if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/go.mod" ]; then
  say "Installing gl-stack from $SCRIPT_DIR..."
  (cd "$SCRIPT_DIR" && GOBIN="$INSTALL_DIR" go install .)
else
  case "$VERSION" in
    @*) TARGET="$REPOSITORY$VERSION" ;;
    *) TARGET="$REPOSITORY@$VERSION" ;;
  esac
  say "Installing gl-stack $VERSION..."
  GOBIN="$INSTALL_DIR" go install "$TARGET"
fi

BINARY="$INSTALL_DIR/gl-stack"
[ -x "$BINARY" ] || fail "installation completed without creating $BINARY"

DETECTED_SHELL=${SHELL:-}
DETECTED_SHELL=${DETECTED_SHELL##*/}
RC_FILE=""
COMPLETION_FILE=""
case "$DETECTED_SHELL" in
  zsh)
    RC_FILE="$HOME/.zshrc"
    COMPLETION_FILE="$DATA_DIR/completions/gl-stack.zsh"
    "$BINARY" completion zsh > "$COMPLETION_FILE"
    ;;
  bash)
    RC_FILE="$HOME/.bashrc"
    COMPLETION_FILE="$DATA_DIR/completions/gl-stack.bash"
    "$BINARY" completion bash > "$COMPLETION_FILE"
    ;;
  fish)
    COMPLETION_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions/gl-stack.fish"
    mkdir -p "$(dirname -- "$COMPLETION_FILE")"
    "$BINARY" completion fish > "$COMPLETION_FILE"
    RC_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
    ;;
  *)
    say "Shell '$DETECTED_SHELL' is not configured automatically; see the README for manual completion setup."
    ;;
esac

if [ "$SHELL_SETUP" = "1" ] && [ -n "$RC_FILE" ]; then
  touch "$RC_FILE"
  if ! grep -F '# >>> gl-stack >>>' "$RC_FILE" >/dev/null 2>&1; then
    {
      printf '\n# >>> gl-stack >>>\n'
      if [ "$DETECTED_SHELL" = "fish" ]; then
        printf 'fish_add_path "%s"\n' "$INSTALL_DIR"
      else
        printf 'export PATH="%s:$PATH"\n' "$INSTALL_DIR"
        printf '[ -r "%s" ] && . "%s"\n' "$COMPLETION_FILE" "$COMPLETION_FILE"
      fi
      printf '# <<< gl-stack <<<\n'
    } >> "$RC_FILE"
    say "Updated $RC_FILE with PATH and $DETECTED_SHELL completions."
  else
    say "$RC_FILE already contains gl-stack shell setup; left it unchanged."
  fi
elif [ "$SHELL_SETUP" != "1" ]; then
  say "Skipped shell configuration (GL_STACK_SHELL_SETUP=$SHELL_SETUP)."
fi

say ""
say "Installed: $BINARY"
say ""
say "GitLab authentication is still required. Add this line to your shell file:"
if [ "$DETECTED_SHELL" = "fish" ]; then
  say "  set -gx GITLAB_TOKEN \"glpat-your-token\""
else
  say "  export GITLAB_TOKEN=\"glpat-your-token\""
fi
say ""
say "Create the token with the GitLab 'api' scope."
if [ "$SHELL_SETUP" = "1" ] && [ -n "$RC_FILE" ]; then
  say "Then restart your shell or reload its configuration:"
  if [ "$DETECTED_SHELL" = "fish" ]; then
    say "  source $RC_FILE"
  else
    say "  . $RC_FILE"
  fi
else
  say "Add the install directory to PATH for the current shell:"
  if [ "$DETECTED_SHELL" = "fish" ]; then
    say "  fish_add_path \"$INSTALL_DIR\""
  else
    say "  export PATH=\"$INSTALL_DIR:\$PATH\""
  fi
fi
say ""
say "Verify the installation with:"
say "  gl-stack --version"
