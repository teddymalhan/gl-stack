# Using `github/gh-stack`

Research current as of 2026-08-08. This guide uses GitHub's official `github/gh-stack` documentation and GitHub CLI documentation.

GitHub currently labels stacked pull requests and this CLI as a public preview; behavior and commands may change. Canonical product documentation: [About stacked pull requests](https://docs.github.com/en/pull-requests/reference/stacked-pull-requests) and [Stacked PR CLI commands](https://docs.github.com/en/pull-requests/reference/stacked-prs-cli-commands).

## Mental model

A stack is an ordered chain of branches. The bottom branch starts from the trunk (usually `main`); each higher branch starts from the branch below it.

```text
main <- auth <- api <- frontend
         PR 1   PR 2     PR 3
base:    main   auth     api
```

Every higher branch contains the lower branches' commits in its Git history. GitHub makes each pull request reviewable by setting its base to the preceding branch, so each PR shows only that layer's diff. `up` means away from trunk; `down` means toward trunk.

Source: [GitHub gh-stack README](https://github.com/github/gh-stack#how-it-works).

## Install and verify prerequisites

The official prerequisites are authenticated GitHub CLI 2.0+, Git 2.20+, and push access to the repository.

```sh
gh --version
gh auth status
gh extension install github/gh-stack
gh stack --help
```

Upgrade later with:

```sh
gh extension upgrade stack
```

Sources: [official quick start](https://github.com/github/gh-stack/blob/main/docs/src/content/docs/getting-started/quick-start.md), [GitHub CLI extension manual](https://cli.github.com/manual/gh_extension). The latest published release observed on 2026-08-08 was [`v0.1.0`](https://github.com/github/gh-stack/releases/tag/v0.1.0), published 2026-07-29.

## First stack: explicit workflow

Use one focused review unit per branch.

```sh
# Start from your repository. This creates/checks out the bottom branch.
gh stack init auth

# Build the bottom layer.
# edit files
git add .
git commit -m "Add authentication model"

# Create the next branch at the current HEAD and check it out.
gh stack add api
# edit files
git add .
git commit -m "Add authentication API"

# Add a third layer.
gh stack add frontend
# edit files
git add .
git commit -m "Add sign-in UI"

# Inspect the branch order.
gh stack view

# Push branches, edit PR titles/bodies/status in the TUI, and create/link PRs.
gh stack submit
```

`submit` pushes branches itself, so a separate `gh stack push` is optional before the initial submission. In an interactive terminal, `submit` opens an editor for PR titles, descriptions, inclusion, and draft/ready status. For non-interactive use, `gh stack submit --auto` generates titles and creates new PRs as drafts; add `--open` to make them ready for review.

Source: [`submit` and typical workflow](https://github.com/github/gh-stack/blob/main/README.md#gh-stack-submit).

## Daily review/update loop

Put a requested change on the branch whose PR should own that diff. Then cascade-rebase all descendants and push the rewritten branches.

```sh
# Select the affected layer.
gh stack checkout auth
# Alternatives: gh stack bottom, gh stack down, gh stack switch

# Make and commit the review fix.
git add .
git commit -m "Address authentication review"

# Rebase this layer's descendants onto its new tip.
gh stack rebase --upstack

# Update all active remote branches with lease-protected pushes.
gh stack push
```

For a full refresh from trunk, use `gh stack rebase`, then `gh stack push`. `gh stack sync` combines fetch, remote/local reconciliation, trunk fast-forward, cascading rebase, push, PR-state sync, remote-stack sync, and optional pruning.

Source: [official workflows guide](https://github.com/github/gh-stack/blob/main/docs/src/content/docs/guides/workflows.md#responding-to-review-feedback).

## Rebase conflicts

```sh
gh stack rebase
# resolve conflict markers in the files
git add <resolved-files>
gh stack rebase --continue

# Or restore every branch to its pre-rebase state:
gh stack rebase --abort
```

`gh stack push` uses explicit `--force-with-lease` checks after rebasing. Its multi-branch push is not atomic: some branches can update while another is rejected. Fix the rejected branch and rerun the command.

Source: [official rebase documentation](https://github.com/github/gh-stack/blob/main/README.md#gh-stack-rebase).

## Merge and clean up

Do not use ordinary `gh pr merge` for a stacked merge. `gh stack merge` merges the selected PR and every unmerged PR below it. A direct stack merge is all-or-nothing; with a merge queue, PRs enter together but may land in separate groups.

```sh
# Interactive: choose how far up the stack to merge and the merge method.
gh stack merge

# Examples:
gh stack merge 42                  # merge through PR 42
gh stack merge --yes --squash     # whole current stack, no prompt

# Refresh local state and prune merged local branches.
gh stack sync --prune
```

All selected PRs and all PRs below them must satisfy reviews, checks, rules, and linear-history requirements. Bypassing merge requirements and auto-merge are currently unsupported for stacked PRs.

Sources: [official merge workflow](https://github.com/github/gh-stack/blob/main/docs/src/content/docs/guides/workflows.md#merging-your-stack), [official FAQ](https://github.com/github/gh-stack/blob/main/docs/src/content/docs/faq.md#merging-stacked-prs).

## Commands worth memorizing

```sh
gh stack view                    # branches, ordering, PR links, statuses
gh stack up [n]                  # away from trunk
gh stack down [n]                # toward trunk
gh stack top
gh stack bottom
gh stack trunk
gh stack switch                  # interactive branch picker
gh stack checkout <PR|URL|name>  # fetch/check out a local or remote stack
gh stack modify                  # reorder/drop/fold/insert/rename in a TUI
gh stack unstack                 # remove local tracking and GitHub association
gh stack unstack --local         # remove only local tracking
```

`gh stack modify` requires a clean working tree, no rebase in progress, linear history, and no queued PR. After modifying a submitted stack, rerun `gh stack submit`.

Source: [complete official CLI reference](https://github.com/github/gh-stack/blob/main/README.md#commands).

## Existing branches or PRs

Adopt already chained branches in bottom-to-top order:

```sh
gh stack init feature-auth feature-api feature-ui
```

If another tool manages local branches, link branches or existing PRs without creating local gh-stack metadata:

```sh
gh stack link feature-auth feature-api feature-ui
gh stack link 10 20 30
```

`link` arguments are bottom-to-top. It pushes branch arguments, creates missing PRs, corrects bases, and creates/updates the GitHub stack association.

Source: [`init`](https://github.com/github/gh-stack/blob/main/README.md#gh-stack-init), [`link`](https://github.com/github/gh-stack/blob/main/README.md#gh-stack-link).

## Important constraints and traps

- A stack requires fully linear ancestry. Avoid merge commits between stack layers; use `gh stack rebase`.
- Stacks can contain at most 100 PRs.
- Every branch must be in the same repository; cross-fork stacks are unsupported.
- GitHub Desktop does not currently support stacked pull requests.
- A closed middle PR blocks every PR above it until the stack is restructured.
- `gh stack push` only pushes; it does not create or update PRs. `submit` does both.
- Local stack metadata lives in `.git/gh-stack` and is not committed.
- Changes to a lower layer rewrite descendant commit SHAs after rebasing. This is expected; lease-protected force pushes update the PR branches.
- GitHub Actions can run once per PR in the stack, increasing CI usage. GitHub evaluates checks, rules, CODEOWNERS, and reviews as if each PR targeted the stack trunk.
- `gh stack sync` can reconcile a remote-ahead stack automatically, but truly divergent local and remote compositions require an explicit choice in an interactive terminal.

Source: [official FAQ](https://github.com/github/gh-stack/blob/main/docs/src/content/docs/faq.md), [official sync docs](https://github.com/github/gh-stack/blob/main/README.md#gh-stack-sync).
