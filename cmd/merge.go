package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/spf13/cobra"
	"github.com/teddymalhan/github-stacker-prs/internal/config"
	"github.com/teddymalhan/github-stacker-prs/internal/git"
	"github.com/teddymalhan/github-stacker-prs/internal/gitlab"
	"github.com/teddymalhan/github-stacker-prs/internal/stack"
	"github.com/teddymalhan/github-stacker-prs/internal/tui/mergeview"
)

type mergeOptions struct {
	mergeMethod string
	squash      bool
	merge       bool
	yes         bool

	// pollInterval and maxPolls control the status polling loop in the
	// non-interactive path. Zero values fall back to sane defaults; tests set
	// them to keep runs fast.
	pollInterval time.Duration
	maxPolls     int
}

// mergeTarget describes an explicitly requested merge request to merge up to.
type mergeTarget struct {
	prNumber int
	hasPR    bool
}

// MergeCmd builds the `gl-stack merge` command.
func MergeCmd(cfg *config.Config) *cobra.Command {
	opts := &mergeOptions{}

	cmd := &cobra.Command{
		Use:   "merge [<stack-number> | <mr-iid>]",
		Short: "Merge a stack of merge requests",
		Long: `Merge some or all of a stack of merge requests from bottom to top.
Before each merge, gl-stack retargets that merge request to the stack's trunk.
GitLab has no atomic stack-merge API: if a later MR fails, earlier MRs remain
merged and the command reports the failure.

With no argument, the stack for the current branch is used. Pass a stack number
to merge a stack you don't have checked out, or a merge request number to merge
directly up to that MR. A bare number is treated first as a stack number, then
as a merge request number.

In an interactive terminal, a short wizard lets you choose how far up the stack
to merge (everything below your selection is always included), pick the merge
method, and confirm, then shows live progress. In a non-interactive terminal, or
with --yes, the whole stack (or everything up to the given MR) is merged without
prompting, using your last-used merge method unless one is specified.

Each merge request must be open and not a draft. GitLab evaluates branch
protection, approvals, pipeline status, and repository rules for each merge.
Bypassing merge requirements is not supported.`,
		Example: `  # Merge the current stack (interactive picker)
  $ gl-stack merge

  # Merge a stack you don't have checked out, by stack number
  $ gl-stack merge 7

  # Merge everything up to and including MR !42
  $ gl-stack merge 42

  # Merge the whole current stack without prompting, squashing
  $ gl-stack merge --yes --squash`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMerge(cfg, opts, args)
		},
	}

	cmd.Flags().StringVar(&opts.mergeMethod, "merge-method", "", "Merge method to use: merge or squash")
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "Merge with a merge commit")
	cmd.Flags().BoolVar(&opts.squash, "squash", false, "Squash and merge")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Merge without prompting for confirmation")

	return cmd
}

func runMerge(cfg *config.Config, opts *mergeOptions, args []string) error {
	method, err := resolveMergeMethodFlag(opts)
	if err != nil {
		cfg.Errorf("%s", err)
		return ErrInvalidArgs
	}

	client, err := cfg.GitLabClient()
	if err != nil {
		cfg.Errorf("failed to create GitLab client: %s", err)
		return ErrAPIFailure
	}

	remoteStack, target, err := resolveMergeStack(cfg, client, args)
	if err != nil {
		return err
	}

	candidates, blocker := mergeCandidates(remoteStack)

	preselectIndex := -1
	targetPR := 0
	if target.hasPR {
		idx := indexOfPR(candidates, target.prNumber)
		if idx < 0 {
			return explainNonMergeableTarget(cfg, remoteStack, target.prNumber, blocker)
		}
		preselectIndex = idx
		targetPR = candidates[idx].Number
	} else if len(candidates) == 0 {
		return explainNothingToMerge(cfg, remoteStack, blocker)
	}

	base := remoteStack.Base.Ref

	// Detect whether the base branch merges through a merge queue so the wizard
	// can skip the merge-method step and enqueue instead of merging directly.
	usesMergeQueue := baseBranchUsesMergeQueue(client, base)

	var mergeCfg *gitlab.RepoMergeConfig
	var allowed []string
	if usesMergeQueue {
		// The queue picks the merge method from its own configuration, so a
		// requested method does not apply.
		if method != "" {
			cfg.Warningf("the base branch %q uses a merge queue; ignoring the merge method", base)
			method = ""
		}
	} else {
		mergeCfg, err = client.RepoMergeConfig()
		if err != nil {
			cfg.Errorf("failed to fetch repository merge settings: %s", err)
			return ErrAPIFailure
		}
		allowed = mergeCfg.AllowedMethods()
		if len(allowed) == 0 {
			cfg.Errorf("this repository does not allow any merge methods")
			return ErrAPIFailure
		}
		if method != "" && !mergeCfg.Allows(method) {
			cfg.Errorf("this repository does not allow %s merges", method)
			return ErrInvalidArgs
		}
	}

	if cfg.IsInteractive() && !opts.yes {
		defaultMethod := ""
		if mergeCfg != nil {
			defaultMethod = mergeCfg.DefaultMethod
		}
		return runMergeInteractive(cfg, client, remoteStack.Number, base, candidates, allowed, defaultMethod, method, preselectIndex, usesMergeQueue, opts)
	}

	// Non-interactive (or --yes): merge the whole stack (or up to the given PR)
	// without prompting.
	if !target.hasPR {
		// A draft or closed merge request partway up the stack blocks everything
		// above it. Rather than silently merging only the portion below it,
		// refuse and let the user target an explicit merge request.
		if blocker != nil {
			top := candidates[len(candidates)-1].Number
			cfg.Errorf("cannot merge the whole stack: merge request !%d is %s", blocker.Number, blockerState(blocker))
			cfg.Printf("Merge up to !%d with `%s`", top, cfg.ColorCyan(fmt.Sprintf("gl-stack merge %d", top)))
			return ErrInvalidArgs
		}
		targetPR = candidates[len(candidates)-1].Number
	}
	if !usesMergeQueue && method == "" {
		method = mergeCfg.DefaultMethod
		if !mergeCfg.Allows(method) {
			method = allowed[0]
		}
	}
	return runMergeHeadless(cfg, client, base, candidates, targetPR, method, usesMergeQueue, opts)
}

// resolveMergeStack determines the remote stack (and any explicitly targeted PR)
// from the command arguments. It never reads local PR state: the local stack
// file is consulted only to discover the stack number when no argument is given.
func resolveMergeStack(cfg *config.Config, client gitlab.ClientOps, args []string) (*gitlab.RemoteStack, mergeTarget, error) {
	if len(args) == 0 {
		rs, err := resolveActiveRemoteStack(cfg, client)
		return rs, mergeTarget{}, err
	}

	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n <= 0 {
		cfg.Errorf("invalid argument %q: expected a stack number or merge request number", args[0])
		return nil, mergeTarget{}, ErrInvalidArgs
	}

	// Try as a stack number first (mirrors `gl-stack checkout`).
	rs, err := client.GetStack(n)
	if err == nil && rs != nil {
		return rs, mergeTarget{}, nil
	}
	if err != nil && !isNotFound(err) {
		cfg.Errorf("failed to fetch stack #%d: %s", n, err)
		return nil, mergeTarget{}, ErrAPIFailure
	}

	// Not a stack number: try as a merge request number.
	rs, err = client.FindStackForPR(n)
	if err != nil {
		if isNotFound(err) {
			warnStacksUnavailable(cfg)
			return nil, mergeTarget{}, ErrStacksUnavailable
		}
		cfg.Errorf("failed to look up merge request !%d: %s", n, err)
		return nil, mergeTarget{}, ErrAPIFailure
	}
	if rs == nil {
		cfg.Errorf("!%d is not a stack number or a stacked merge request", n)
		return nil, mergeTarget{}, ErrNotInStack
	}
	return rs, mergeTarget{prNumber: n, hasPR: true}, nil
}

// resolveActiveRemoteStack reads only the local stack number for the current
// branch, then fetches the full stack (and its PR states) from GitLab.
func resolveActiveRemoteStack(cfg *config.Config, client gitlab.ClientOps) (*gitlab.RemoteStack, error) {
	gitDir, err := git.GitDir()
	if err != nil {
		cfg.Errorf("not a git repository")
		return nil, ErrNotInStack
	}
	sf, err := stack.Load(gitDir)
	if err != nil {
		cfg.Errorf("failed to load stack state: %s", err)
		return nil, ErrNotInStack
	}
	currentBranch, err := git.CurrentBranch()
	if err != nil {
		cfg.Errorf("failed to get current branch: %s", err)
		return nil, ErrNotInStack
	}

	stacks := sf.FindAllStacksForBranch(currentBranch)
	if len(stacks) == 0 {
		cfg.Errorf("current branch %q is not part of a stack", currentBranch)
		cfg.Printf("Checkout a stack first, or specify which stack or merge request to merge with `%s`", cfg.ColorCyan("gl-stack merge [number]"))
		return nil, ErrNotInStack
	}
	if len(stacks) > 1 {
		cfg.Errorf("branch %q belongs to multiple stacks", currentBranch)
		cfg.Printf("Checkout a stack first, or specify which stack or merge request to merge with `%s`", cfg.ColorCyan("gl-stack merge [number]"))
		return nil, ErrDisambiguate
	}
	s := stacks[0]
	if s.ID == "" && s.Number == 0 {
		cfg.Errorf("this stack has not been submitted to GitLab yet; run `gl-stack submit` first")
		return nil, ErrNotInStack
	}

	number, err := ensureStackNumber(client, s)
	if err != nil {
		if isNotFound(err) {
			warnStacksUnavailable(cfg)
			return nil, ErrStacksUnavailable
		}
		cfg.Errorf("failed to resolve stack number: %s", err)
		return nil, ErrAPIFailure
	}
	if number == 0 {
		cfg.Errorf("could not determine the stack number for the current stack")
		return nil, ErrNotInStack
	}

	rs, err := client.GetStack(number)
	if err != nil {
		if isNotFound(err) {
			warnStacksUnavailable(cfg)
			return nil, ErrStacksUnavailable
		}
		cfg.Errorf("failed to fetch stack #%d: %s", number, err)
		return nil, ErrAPIFailure
	}
	return rs, nil
}

func runMergeInteractive(cfg *config.Config, client gitlab.ClientOps, stackNumber int, base string, candidates []mergeview.PRItem, allowed []string, viewerDefault, methodFlag string, preselectIndex int, usesMergeQueue bool, opts *mergeOptions) error {
	defaultMethod := viewerDefault
	if methodFlag != "" {
		defaultMethod = methodFlag
	}

	// Enrich the picker with PR titles (best-effort; the branch is shown either way).
	nums := make([]int, len(candidates))
	for i, c := range candidates {
		nums[i] = c.Number
	}
	if titles, err := client.PRTitles(nums); err == nil {
		for i := range candidates {
			if t := titles[candidates[i].Number]; t != "" {
				candidates[i].Title = t
			}
		}
	}

	submit, poll := mergeFuncs(client, mergeActionFor(usesMergeQueue))

	model := mergeview.New(mergeview.Options{
		PRs:               candidates,
		StackNumber:       stackNumber,
		BaseRef:           base,
		AllowedMethods:    allowed,
		DefaultMethod:     defaultMethod,
		PreselectTopIndex: preselectIndex,
		UsesMergeQueue:    usesMergeQueue,
		Submit:            submit,
		Poll:              poll,
		PollInterval:      opts.pollInterval,
	})

	final, err := tea.NewProgram(model, tea.WithInput(cfg.In), tea.WithOutput(cfg.Out)).Run()
	if err != nil {
		cfg.Errorf("failed to run merge: %s", err)
		return ErrSilent
	}

	out := final.(mergeview.Model).Outcome()
	switch {
	case out.Err != nil:
		if errors.Is(out.Err, gitlab.ErrAsyncMergeUnavailable) {
			warnAsyncMergeUnavailable(cfg)
			return ErrStacksUnavailable
		}
		cfg.Errorf("merge failed: %s", out.Err)
		return ErrAPIFailure
	case out.Merged:
		mergedSuccess(cfg, prNumberList(out.MergedPRs), base, out.SHA)
		return nil
	case out.Enqueued:
		enqueuedSuccess(cfg, prNumberList(out.MergedPRs), base)
		return nil
	case out.Failed:
		cfg.Errorf("merge failed: %s", out.Message)
		cfg.Printf("Stack merges are atomic, so nothing was merged.")
		return mergeFailureExit(out.Message)
	case out.WatchStopped:
		cfg.Infof("Stopped watching. Merge is still in progress. Check the merge requests on GitLab.")
		return ErrSilent
	default:
		// Cancelled via esc/ctrl+c before submitting.
		cfg.Infof("Cancelled operation, nothing merged")
		return ErrSilent
	}
}

func runMergeHeadless(cfg *config.Config, client gitlab.ClientOps, base string, candidates []mergeview.PRItem, targetPR int, method string, usesMergeQueue bool, opts *mergeOptions) error {
	nums := numbersUpTo(candidates, targetPR)
	list := prNumberList(nums)

	if usesMergeQueue {
		cfg.Printf("Adding %s to the merge queue for %s...", list, base)
	} else {
		cfg.Printf("Merging %s into %s via %s...", list, base, method)
	}

	res, err := client.MergeStackAsync(targetPR, method, mergeActionFor(usesMergeQueue))
	if err != nil {
		if errors.Is(err, gitlab.ErrAsyncMergeUnavailable) {
			warnAsyncMergeUnavailable(cfg)
			return ErrStacksUnavailable
		}
		cfg.Errorf("failed to start merge: %s", err)
		return ErrAPIFailure
	}

	if res.IsMerged() {
		mergedSuccess(cfg, list, base, res.Details.SHA)
		return nil
	}
	if res.IsEnqueued() {
		enqueuedSuccess(cfg, list, base)
		return nil
	}
	if res.IsFailed() {
		cfg.Errorf("merge failed: %s", res.Details.Message)
		cfg.Printf("Stack merges are atomic, so nothing was merged.")
		return mergeFailureExit(res.Details.Message)
	}

	uuid := res.Details.UUID
	if uuid == "" {
		cfg.Errorf("merge did not start as expected")
		return ErrAPIFailure
	}
	interval := opts.pollInterval
	if interval <= 0 {
		interval = time.Second
	}
	maxPolls := opts.maxPolls
	if maxPolls <= 0 {
		maxPolls = 600
	}

	for i := 0; i < maxPolls; i++ {
		time.Sleep(interval)

		status, err := client.GetAsyncMergeResult(targetPR, uuid)
		if err != nil {
			cfg.Errorf("failed to check merge status: %s", err)
			return ErrAPIFailure
		}
		if status.IsMerged() {
			mergedSuccess(cfg, list, base, status.Details.SHA)
			return nil
		}
		if status.IsEnqueued() {
			enqueuedSuccess(cfg, list, base)
			return nil
		}
		if status.IsFailed() {
			cfg.Errorf("merge failed: %s", status.Details.Message)
			cfg.Printf("Stack merges are atomic, so nothing was merged.")
			return mergeFailureExit(status.Details.Message)
		}
	}

	cfg.Warningf("Merge is still in progress. Check the merge requests on GitLab.")
	return ErrAPIFailure
}

// baseBranchUsesMergeQueue reports whether the stack's base branch merges through
// a merge queue. Detection tailors the wizard (skipping the method step and
// switching to enqueue wording) and selects the explicit merge_action. On a
// lookup failure it falls back to the direct-merge flow (method step shown,
// "direct_merge" sent), matching what the user is shown.
func baseBranchUsesMergeQueue(client gitlab.ClientOps, base string) bool {
	uses, err := client.BaseBranchUsesMergeQueue(base)
	if err != nil {
		return false
	}
	return uses
}

// mergeActionFor maps merge-queue detection to the explicit async-merge action: a
// detected queue forces "merge_queue" (the server rejects it when the branch has
// no queue, guarding against a wrong guess), and everything else forces a
// "direct_merge". Being explicit ensures the merge matches the wizard the user
// saw rather than letting the server choose the routing.
func mergeActionFor(usesMergeQueue bool) string {
	if usesMergeQueue {
		return gitlab.MergeActionMergeQueue
	}
	return gitlab.MergeActionDirectMerge
}

// mergeFuncs returns submit/poll closures that adapt the GitLab client to the
// mergeview injection points. The merge action is fixed for the session.
func mergeFuncs(client gitlab.ClientOps, mergeAction string) (mergeview.SubmitFunc, mergeview.PollFunc) {
	submit := func(targetPR int, method string) (mergeview.MergeStatus, error) {
		res, err := client.MergeStackAsync(targetPR, method, mergeAction)
		if err != nil {
			return mergeview.MergeStatus{}, err
		}
		return toMergeStatus(res), nil
	}
	poll := func(targetPR int, uuid string) (mergeview.MergeStatus, error) {
		res, err := client.GetAsyncMergeResult(targetPR, uuid)
		if err != nil {
			return mergeview.MergeStatus{}, err
		}
		return toMergeStatus(res), nil
	}
	return submit, poll
}

func toMergeStatus(res *gitlab.AsyncMergeResult) mergeview.MergeStatus {
	status := mergeview.StatusPending
	switch {
	case res.IsMerged():
		status = mergeview.StatusMerged
	case res.IsEnqueued():
		status = mergeview.StatusEnqueued
	case res.IsFailed():
		status = mergeview.StatusFailed
	}
	return mergeview.MergeStatus{
		Status:  status,
		Message: res.Details.Message,
		UUID:    res.Details.UUID,
		SHA:     res.Details.SHA,
	}
}

// mergeCandidates returns the merge requests that can be merged, ordered bottom to
// top: the contiguous run of open, non-draft PRs starting from the bottom of the
// stack (already-merged PRs at the bottom are skipped). The first draft or
// closed PR blocks everything above it and is returned as the blocker.
func mergeCandidates(rs *gitlab.RemoteStack) (items []mergeview.PRItem, blocker *gitlab.RemoteStackPR) {
	if rs == nil {
		return nil, nil
	}
	for i := range rs.PRDetails {
		pr := rs.PRDetails[i]
		if pr.IsMerged() {
			continue
		}
		if pr.Draft || pr.State == "closed" {
			b := pr
			return items, &b
		}
		items = append(items, mergeview.PRItem{Number: pr.Number, Branch: pr.Head.Ref})
	}
	return items, nil
}

func explainNothingToMerge(cfg *config.Config, rs *gitlab.RemoteStack, blocker *gitlab.RemoteStackPR) error {
	if allMerged(rs) {
		cfg.Successf("This stack is already fully merged.")
		return nil
	}
	if blocker != nil {
		cfg.Errorf("nothing to merge: merge request !%d is %s", blocker.Number, blockerState(blocker))
		return ErrNotInStack
	}
	cfg.Errorf("this stack has no open merge requests to merge")
	return ErrNotInStack
}

func explainNonMergeableTarget(cfg *config.Config, rs *gitlab.RemoteStack, prNumber int, blocker *gitlab.RemoteStackPR) error {
	pr := findRemotePR(rs, prNumber)
	switch {
	case pr == nil:
		cfg.Errorf("merge request !%d is not part of this stack", prNumber)
		return ErrInvalidArgs
	case pr.IsMerged():
		cfg.Successf("merge request !%d is already merged", prNumber)
		return nil
	case pr.Draft:
		cfg.Errorf("merge request !%d is a draft; mark it ready for review before merging", prNumber)
		return ErrInvalidArgs
	case pr.State == "closed":
		cfg.Errorf("merge request !%d is closed", prNumber)
		return ErrInvalidArgs
	case blocker != nil:
		cfg.Errorf("merge request !%d cannot be merged yet: !%d below it is %s", prNumber, blocker.Number, blockerState(blocker))
		return ErrInvalidArgs
	default:
		cfg.Errorf("merge request !%d cannot be merged", prNumber)
		return ErrInvalidArgs
	}
}

func warnAsyncMergeUnavailable(cfg *config.Config) {
	cfg.Warningf("Async stack merge is not available for this repository")
}

// mergeFailureExit maps a merge failure message to an exit code: rebase/merge
// conflicts get ErrConflict, everything else ErrAPIFailure.
func mergeFailureExit(message string) error {
	if strings.Contains(strings.ToLower(message), "conflict") {
		return ErrConflict
	}
	return ErrAPIFailure
}

func resolveMergeMethodFlag(opts *mergeOptions) (string, error) {
	var picks []string
	if opts.merge {
		picks = append(picks, gitlab.MergeMethodMerge)
	}
	if opts.squash {
		picks = append(picks, gitlab.MergeMethodSquash)
	}
	if opts.mergeMethod != "" {
		mm := strings.ToLower(strings.TrimSpace(opts.mergeMethod))
		switch mm {
		case gitlab.MergeMethodMerge, gitlab.MergeMethodSquash:
			picks = append(picks, mm)
		default:
			return "", fmt.Errorf("invalid --merge-method %q: must be merge or squash", opts.mergeMethod)
		}
	}

	distinct := map[string]struct{}{}
	for _, p := range picks {
		distinct[p] = struct{}{}
	}
	if len(distinct) > 1 {
		return "", errors.New("only one merge method may be specified")
	}
	for p := range distinct {
		return p, nil
	}
	return "", nil
}

func indexOfPR(items []mergeview.PRItem, number int) int {
	for i, it := range items {
		if it.Number == number {
			return i
		}
	}
	return -1
}

func numbersUpTo(items []mergeview.PRItem, targetPR int) []int {
	var nums []int
	for _, it := range items {
		nums = append(nums, it.Number)
		if it.Number == targetPR {
			break
		}
	}
	return nums
}

func prNumberList(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("!%d", n)
	}
	return strings.Join(parts, ", ")
}

func findRemotePR(rs *gitlab.RemoteStack, number int) *gitlab.RemoteStackPR {
	if rs == nil {
		return nil
	}
	for i := range rs.PRDetails {
		if rs.PRDetails[i].Number == number {
			return &rs.PRDetails[i]
		}
	}
	return nil
}

func allMerged(rs *gitlab.RemoteStack) bool {
	if rs == nil || len(rs.PRDetails) == 0 {
		return false
	}
	for i := range rs.PRDetails {
		if !rs.PRDetails[i].IsMerged() {
			return false
		}
	}
	return true
}

func blockerState(pr *gitlab.RemoteStackPR) string {
	if pr.Draft {
		return "a draft"
	}
	if pr.State == "closed" {
		return "closed"
	}
	return "not mergeable"
}

func shortMergeSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// mergedSuccess prints the merge success line, appending the merge commit SHA in
// parentheses when known: "Merged #1, #2 into main (abc1234)".
func mergedSuccess(cfg *config.Config, list, base, sha string) {
	if sha != "" {
		cfg.Successf("Merged %s into %s (%s)", list, base, shortMergeSHA(sha))
		return
	}
	cfg.Successf("Merged %s into %s", list, base)
}

// enqueuedSuccess prints the success line when the base branch uses a merge
// queue: the stack was added to the queue and will merge once it's processed.
func enqueuedSuccess(cfg *config.Config, list, base string) {
	cfg.Successf("Added %s to the merge queue for %s", list, base)
	cfg.Printf("They will merge once the queue processes them.")
}

func isNotFound(err error) bool {
	var httpErr *api.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}
