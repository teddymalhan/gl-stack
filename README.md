# gl-stack

<img width="960" height="894" alt="demo" src="https://github.com/user-attachments/assets/a418532f-81fa-4978-9add-f44388bd5910" />

[GitHub Stacked MRs' CLI workflow](https://github.blog/changelog/2026-07-30-stacked-pull-requests-are-now-in-public-preview/), but for GitLab

## Install

You need [Git](https://git-scm.com/downloads) and
[Go](https://go.dev/dl/) installed. Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh | sh
```

Once completed, restart the shell or reload its configuration:

```sh
# zsh
source ~/.zshrc

# bash
source ~/.bashrc
```

## AI agent skill

Install the `gl-stack` skill in your current project so supported coding agents know how to create,
navigate, submit, and recover stacked GitLab merge requests:

```sh
npx skills add teddymalhan/gl-stack --skill gl-stack
```

The default installation is project-local and can be committed with your project. To make the skill
available across all projects instead, add `--global`:

```sh
npx skills add teddymalhan/gl-stack --skill gl-stack --global
```

## GitLab authentication

Create a [personal access token](https://docs.gitlab.com/user/profile/personal_access_tokens/)
with the `api` scope and at least the **Developer** role in each project you
will use. Open the configuration file for your shell and add:

```sh
# ~/.zshrc or ~/.bashrc
export GITLAB_TOKEN="glpat-..."
```
