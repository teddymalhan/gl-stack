# gl-stack

<img width="960" height="894" alt="demo" src="https://github.com/user-attachments/assets/a418532f-81fa-4978-9add-f44388bd5910" />


A CLI for managing stacked branches and merge requests on GitLab.

## Install

You need [Git](https://git-scm.com/downloads) and
[Go](https://go.dev/dl/) installed. Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh | sh
```

The installer:

- builds the latest `gl-stack` version with `go install`
- installs the binary in `~/.local/bin`
- adds that directory to `PATH` in `~/.zshrc` or `~/.bashrc`
- generates and enables completions for zsh, bash, or fish
- prints the remaining GitLab token setup

Restart the shell after installation, or reload its configuration:

```sh
# zsh
source ~/.zshrc

# bash
source ~/.bashrc
```

Verify the result:

```sh
gl-stack --version
gl-stack --help
```

Running the installer again is safe. It refreshes the binary and generated
completion file without duplicating the shell configuration.

### Installer options

Environment variables can customize a non-default installation:

```sh
# Install a tag, branch, or commit.
curl -fsSL \
  https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh |
  GL_STACK_VERSION=v1.2.3 sh

# Choose the binary and completion data directories.
curl -fsSL https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh |
  GL_STACK_INSTALL_DIR="$HOME/bin" \
  GL_STACK_DATA_DIR="$HOME/.local/share/gl-stack" sh

# Install without changing a shell configuration file.
curl -fsSL \
  https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh |
  GL_STACK_SHELL_SETUP=0 sh
```

Running `./install.sh` from a cloned repository installs the current checkout
instead of downloading the latest module version.

## GitLab authentication

Create a [personal access token](https://docs.gitlab.com/user/profile/personal_access_tokens/)
with the `api` scope and at least the **Developer** role in each project you
will use. Open the configuration file for your shell and add:

```sh
# ~/.zshrc or ~/.bashrc
export GITLAB_TOKEN="glpat-..."
```

Do not commit that token to a repository. Reload the file with `source`, as
shown above. `gl-stack` reads `GITLAB_TOKEN` and falls back to `GLAB_TOKEN` when
`GITLAB_TOKEN` is unset.

The `read_api` scope supports only read-only operations such as listing and
viewing stacks. Creating, retargeting, and merging merge requests requires
`api`.

Commands such as `submit`, `push`, and `sync` also run ordinary Git fetch and
push operations. Their authentication comes from the repository's existing Git
configuration; setting `GITLAB_TOKEN` does not configure Git credentials.

| Token and Git remote | Required token scopes |
| --- | --- |
| Personal access token with an SSH remote | `api` |
| Personal access token with an HTTPS remote using the same token | `api` |
| Project or group access token with an SSH remote | `api` |
| Project or group access token with an HTTPS remote using the same token | `api`, `write_repository` |
| Read-only stack access | `read_api` |

For personal access tokens, `api` includes repository access over Git-over-HTTP.
For project and group access tokens, `write_repository` is separate and does
not provide API access, so both scopes are required when the token handles API
requests and HTTPS pushes.

A Developer can generally create merge requests and push unprotected feature
branches. Merging still depends on the target branch's **Allowed to merge**
rule, required approvals, pipeline status, and other project merge checks. If a
protected target branch permits only Maintainers to merge, the token's user or
bot must have the **Maintainer** role.

No `sudo`, `admin_mode`, `read_user`, registry, or runner scopes are required.

## Manual shell setup

The installer normally performs these steps. For a manual installation, first
put the binary on `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Then generate completion for the current shell:

```sh
# zsh
mkdir -p ~/.local/share/gl-stack/completions
gl-stack completion zsh > ~/.local/share/gl-stack/completions/gl-stack.zsh
printf '%s\n' \
  '[ -r "$HOME/.local/share/gl-stack/completions/gl-stack.zsh" ] && . "$HOME/.local/share/gl-stack/completions/gl-stack.zsh"' \
  >> ~/.zshrc

# bash
mkdir -p ~/.local/share/gl-stack/completions
gl-stack completion bash > ~/.local/share/gl-stack/completions/gl-stack.bash
printf '%s\n' \
  '[ -r "$HOME/.local/share/gl-stack/completions/gl-stack.bash" ] && . "$HOME/.local/share/gl-stack/completions/gl-stack.bash"' \
  >> ~/.bashrc

# fish (loaded automatically by fish)
mkdir -p ~/.config/fish/completions
gl-stack completion fish > ~/.config/fish/completions/gl-stack.fish
```
