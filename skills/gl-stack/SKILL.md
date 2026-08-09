---
name: gl-stack
description: >
  Manages stacked GitLab merge requests and splits multi-part work into reviewable branches with
  gl-stack. Use for stack creation, viewing, edits, push, submit, sync, rebase, merge, checkout, or
  unstack; when asked to split or isolate work for review; whenever a user mentions a stack, branch
  layers, dependent MRs, stacked merge requests, or gl-stack; or when a gl-stack is checked out.
metadata:
  author: teddymalhan
  version: "0.1.0"
---

# gl-stack

`gl-stack` is a standalone CLI for stacked branches and merge requests on GitLab. A stack is an
ordered chain of branches rooted on a trunk, where each branch has one MR targeting the branch below
it, so a reviewer sees only that layer's diff.

`gl-stack` prints a stack trunk-first, left to right:

```text
(main) <- auth <- api <- frontend
```

Left is the **bottom**, right is the **top**. `auth` is based on `main` and merges first;
`frontend` merges last. `up` moves toward the top, away from trunk; `down` moves toward it.
Foundational work belongs at the bottom, code that depends on it above. Before choosing layers, read
`references/stack-design.md`.

## Setup

Install `gl-stack` as documented by the project, then configure authentication:

```bash
curl -fsSL https://raw.githubusercontent.com/teddymalhan/gl-stack/main/install.sh | sh
export GITLAB_TOKEN="glpat-..."   # GLAB_TOKEN is also accepted
git config rerere.enabled true    # remember conflict resolutions
```

The token needs the `api` scope and at least the Developer role in the project. When a repository
has multiple remotes, pass `--remote <name>` to remote operations or configure one of:
`branch.<name>.pushRemote`, `remote.pushDefault`, `branch.<name>.remote`, or `gl-stack.remote`.

## Non-interactive use

Interactive invocations can open prompts or full-screen TUIs and block an agent. Always use the
explicit forms below instead of relying on TTY detection.

| Always run | Never run bare | Why |
|---|---|---|
| `gl-stack view --json` | `gl-stack view` | opens a TUI in an interactive terminal |
| `gl-stack submit --auto` | `gl-stack submit` | opens the MR editor interactively |
| `gl-stack merge <target> --yes` | `gl-stack merge` | opens a merge wizard interactively |
| `gl-stack init <branch>...` | `gl-stack init` | prompts for branch names interactively |
| `gl-stack add <branch>` | `gl-stack add` | prompts for a branch name interactively |
| `gl-stack checkout <target>` | `gl-stack checkout` | opens a local/remote stack picker |
| `gl-stack up` / `down` / `top` / `bottom` | `gl-stack switch` | `switch` is menu-only |
| — | `gl-stack modify` | TUI-only except `--continue` and `--abort` recovery |

`view --short` is safe but formatted for humans. Use `--json` for automation. Pass
`--remote <name>` to `push`, `submit`, `sync`, `rebase`, and `link` when remote selection is not
configured.

## Branch placement

- **Starting multi-part work:** create the stack before writing files. Put one dependent concern in
  each layer, bottom to top.
- **Editing an existing stack:** check out the layer that owns the change before editing. Never
  commit a lower layer's concern on the current top branch. Run `gl-stack view --json`, check out the
  owner, edit and commit, rebase upstack, then return to the top.

```bash
gl-stack checkout api
git add ... && git commit -m "Add get-user endpoint"
gl-stack rebase --upstack
gl-stack top
gl-stack push
```

## Core loop

```bash
gl-stack init auth              # create the stack and check out its bottom branch
git add ... && git commit -m "Add auth middleware"
gl-stack add api                # add a layer from the current top
git add ... && git commit -m "Add API routes"
gl-stack submit --auto          # push all branches and create draft MRs
gl-stack view --json            # verify local and MR state
```

Add `--open` to `submit` to make new and existing MRs ready for review. Branch names are verbatim:
`gl-stack add refactor/foo` creates `refactor/foo`.

## Staying in sync

```bash
gl-stack sync                   # fetch, reconcile, rebase, push, and refresh MR targets/state
gl-stack sync --prune           # also delete local branches for merged MRs
```

`sync` never opens MRs. If local and GitLab chains diverge, non-interactive sync aborts without
pushing branches or changing MRs. See `references/troubleshooting.md`.

## Merging

Scope every non-interactive merge explicitly:

```bash
gl-stack merge 42 --yes          # MR !42 plus every unmerged MR below it
gl-stack merge 7 --yes           # every unmerged MR in stack #7
gl-stack merge 42 --yes --squash # or --merge / --merge-method <method>
```

A bare number resolves as a stack number first, then an MR IID. GitLab has no atomic stack-merge API:
MRs merge bottom-to-top, and if a later MR fails, earlier MRs remain merged. Each MR is retargeted to
the trunk immediately before it merges. Do not claim an all-or-nothing result.

## Reading state

`gl-stack view --json` writes JSON to stdout. Status and errors go to stderr; branch on exit codes.

```text
trunk           string
currentBranch   string
branches[]      name, head, base, isCurrent, isMerged, isQueued, needsRebase
branches[].pr   number, url, state (absent when no MR exists)
```

`base` is the saved SHA of the parent branch that the branch was last known to contain. It can be
older than the parent's current tip. `needsRebase` is true when the current parent tip is no longer
an ancestor of the branch.

## Exit codes

| Code | Meaning | Recovery |
|---|---|---|
| 0 | Success | — |
| 1 | Generic error already printed | Read stderr |
| 2 | Branch or stack not found | `gl-stack init`, or `gl-stack checkout <target>` |
| 3 | Conflict | Follow the conflict recovery below |
| 4 | GitLab API failure | Check the token, project access, and network; retry |
| 5 | Invalid arguments | Fix the invocation; use `<command> --help` |
| 6 | Disambiguation required | Check out a branch unique to the intended stack or pass a target |
| 7 | Rebase already in progress | `gl-stack rebase --continue` or `--abort` |
| 8 | Stack state lock/staleness | Wait and rerun the command |
| 9 | Stack APIs unavailable | Verify GitLab/project support and access |
| 10 | Modify recovery required | `gl-stack modify --continue` or `--abort` |

For a `rebase` conflict: resolve files, `git add` them, then run
`gl-stack rebase --continue`; `gl-stack rebase --abort` restores the stack. A failed `sync` restores
all branches first, so rerun with `gl-stack rebase` to stop at a resolvable conflict.

## Constraints

- Stacks are strictly linear: one parent and at most one child. Use separate stacks for parallel work.
- `push` uses per-branch `--force-with-lease` and is not atomic; some refs can update before a later
  lease fails. `sync` uses an atomic multi-ref push.
- `merge` is sequential and not atomic on GitLab.
- There is no non-interactive reorder or removal. Use `unstack`, rewrite ancestry, and `init` again.
- MR titles and descriptions are auto-generated with `submit --auto`; edit them through GitLab or
  another GitLab client afterwards.
- `checkout <branch-name>` resolves locally tracked stacks only. Use a stack number, MR IID, or MR URL
  to import a remote stack.
- Local metadata is stored in `.git/gl-stack`; it is repository-local and must not be committed.

## More detail

`gl-stack <command> --help` is authoritative for flags and arguments. Open only the reference that
matches the task:

- `references/stack-design.md` — read before creating a stack or choosing layers.
- `references/commands.md` — read for preconditions, side effects, ordering, and failure modes.
- `references/troubleshooting.md` — read for conflicts, divergence, restructuring, or recovery.
