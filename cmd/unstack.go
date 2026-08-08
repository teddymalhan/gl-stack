package cmd

import (
	"errors"
	"strconv"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/spf13/cobra"
	"github.com/teddymalhan/gl-stack/internal/config"
	"github.com/teddymalhan/gl-stack/internal/gitlab"
	"github.com/teddymalhan/gl-stack/internal/modify"
	"github.com/teddymalhan/gl-stack/internal/stack"
)

type unstackOptions struct {
	local       bool
	stackNumber int
}

func UnstackCmd(cfg *config.Config) *cobra.Command {
	opts := &unstackOptions{}

	cmd := &cobra.Command{
		Use:     "unstack [<stack-number>]",
		Aliases: []string{"delete"},
		Short:   "Remove a stack locally and on GitLab",
		Long: `Remove a stack from local tracking and dissolve its MR chain on GitLab.

With no argument, the active stack is dissolved and removed from local tracking.
Provide a stack number—the bottom MR's IID—to target a remote stack from
anywhere in the repository. If it is also tracked locally, that tracking is
removed after the GitLab update succeeds.

Dissolving a GitLab stack retargets each open merge request directly to the
stack's trunk branch; it does not close or delete merge requests. Merged MRs are
left unchanged. Use --local to remove only local tracking.`,
		Example: `  # Unstack the current stack locally and on GitLab
  $ gl-stack unstack

  # Unstack a specific stack by its number
  $ gl-stack unstack 7

  # Only remove local tracking (keep the stack on GitLab)
  $ gl-stack unstack --local`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil || n <= 0 {
					cfg.Errorf("invalid stack number %q", args[0])
					return ErrInvalidArgs
				}
				opts.stackNumber = n
			}
			return runUnstack(cfg, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.local, "local", false, "Only delete the stack locally")

	return cmd
}

func runUnstack(cfg *config.Config, opts *unstackOptions) error {
	// A stack number targets a specific stack. It is unstacked directly on
	// GitLab by number (remote-first), so this works from anywhere in the
	// repository whether or not the stack is tracked locally.
	if opts.stackNumber > 0 {
		// --local must never contact GitLab, so it uses a strictly local lookup
		result, ok, err := lookupStackByNumber(cfg, opts.stackNumber, !opts.local)
		if err != nil {
			return ErrNotInStack
		}
		if !ok {
			// The stack number isn't tracked locally.
			if opts.local {
				// --local never contacts GitLab, and there is nothing to remove
				// locally, so there is nothing to do.
				cfg.Errorf("stack #%d is not tracked locally", opts.stackNumber)
				cfg.Printf("Omit %s to unstack it on GitLab", cfg.ColorCyan("--local"))
				return ErrNotInStack
			}
			return runRemoteUnstack(cfg, opts.stackNumber)
		}
		return unstackTrackedStack(cfg, opts, result)
	}

	// No argument: operate on the active stack for the current branch.
	result, err := loadStack(cfg, "")
	if err != nil {
		return ErrNotInStack
	}
	return unstackTrackedStack(cfg, opts, result)
}

// unstackTrackedStack unstacks a locally tracked stack: it removes the stack on
// GitLab (unless --local) and then removes it from local tracking.
func unstackTrackedStack(cfg *config.Config, opts *unstackOptions, result *loadStackResult) error {
	gitDir := result.GitDir

	if err := modify.CheckStateGuard(gitDir); err != nil {
		cfg.Errorf("%s", err)
		return ErrModifyRecovery
	}

	sf := result.StackFile
	s := result.Stack

	// Unstack on GitLab first (unless --local). The server decides which PRs
	// can be unstacked; PRs that are queued for merge or have auto-merge enabled
	// are left in place and the stack is kept. Local tracking is only removed
	// when the remote stack is fully dissolved.
	if !opts.local {
		if s.ID == "" && s.Number == 0 {
			cfg.Warningf("Stack has no remote ID — skipping server-side unstack")
		} else {
			client, err := cfg.GitLabClient()
			if err != nil {
				cfg.Errorf("failed to create GitLab client: %s", err)
				return ErrAPIFailure
			}

			number, err := ensureStackNumber(client, s)
			if err != nil {
				cfg.Errorf("failed to look up stack on GitLab: %s", err)
				return ErrAPIFailure
			}

			if number == 0 {
				cfg.Warningf("Stack not found on GitLab — continuing with local unstack")
			} else {
				keepLocal, err := unstackNumberOnGitLab(cfg, client, number, true)
				if err != nil {
					return err
				}
				if keepLocal {
					// Some PRs remain stacked, so the stack still exists on
					// GitLab. Keep local tracking so it continues to reflect it.
					return nil
				}
			}
		}
	}

	// Remove the exact resolved stack from local tracking by pointer identity,
	// not by branch name — avoids removing the wrong stack when a trunk
	// branch is shared across multiple stacks.
	for i := range sf.Stacks {
		if &sf.Stacks[i] == s {
			sf.RemoveStack(i)
			break
		}
	}
	if err := stack.Save(gitDir, sf); err != nil {
		return handleSaveError(cfg, err)
	}
	cfg.Successf("Stack removed from local tracking")

	return nil
}

// runRemoteUnstack unstacks a stack on GitLab purely by its number, without any
// local tracking. This is the remote-first path that lets `gl-stack unstack
// <number>` run from anywhere in the repository (like `gl-stack link`), whether
// or not the stack is checked out locally.
func runRemoteUnstack(cfg *config.Config, number int) error {
	client, err := cfg.GitLabClient()
	if err != nil {
		cfg.Errorf("failed to create GitLab client: %s", err)
		return ErrAPIFailure
	}
	if _, err := unstackNumberOnGitLab(cfg, client, number, false); err != nil {
		return err
	}
	return nil
}

// unstackNumberOnGitLab calls the Unstack API for the given stack number and
// reports the outcome. hasLocalTracking indicates whether the caller has a
// locally tracked stack to reconcile, which changes how a 404 and a partial
// unstack are handled: with local tracking a 404 is an idempotent success (the
// caller finishes removing local state) and a partial unstack keeps local
// tracking; without it a 404 is a hard error because the user targeted a stack
// that does not exist on GitLab.
//
// It returns keepLocal=true when local tracking should be preserved because a
// partial unstack left some PRs stacked. keepLocal is only meaningful when
// hasLocalTracking is true.
func unstackNumberOnGitLab(cfg *config.Config, client gitlab.ClientOps, number int, hasLocalTracking bool) (keepLocal bool, err error) {
	_, dissolved, err := client.Unstack(number)
	if err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) {
			switch httpErr.StatusCode {
			case 404:
				if hasLocalTracking {
					// Stack already gone on GitLab — treat as success and let
					// the caller finish removing local tracking.
					cfg.Warningf("Stack not found on GitLab — continuing with local unstack")
					return false, nil
				}
				// Remote-first: the targeted stack does not exist on GitLab.
				cfg.Errorf("stack #%d not found on GitLab", number)
				return false, ErrNotInStack
			case 422:
				// The server refused: every PR is queued for merge or has
				// auto-merge enabled, so nothing can be unstacked.
				cfg.Errorf("Unstacking not allowed: %s", httpErr.Message)
				return false, ErrInvalidArgs
			default:
				cfg.Errorf("Failed to unstack on GitLab (HTTP %d): %s", httpErr.StatusCode, httpErr.Message)
				return false, ErrAPIFailure
			}
		}
		cfg.Errorf("Failed to unstack on GitLab: %v", err)
		return false, ErrAPIFailure
	}

	if !dissolved {
		// Some PRs (queued for merge or with auto-merge enabled) remain stacked
		// on GitLab, so the stack still exists.
		cfg.Warningf("Some merge requests are queued for merge or have auto-merge enabled and remain stacked on GitLab")
		if hasLocalTracking {
			cfg.Printf("The stack was left in place — local tracking is unchanged")
			return true, nil
		}
		return false, nil
	}

	cfg.Successf("Stack removed on GitLab%s", stackLabel(number))
	return false, nil
}
