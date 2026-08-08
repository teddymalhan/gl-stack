package cmd

import (
	"net/url"
	"strings"

	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/teddymalhan/github-stacker-prs/internal/config"
)

const (
	feedbackURL     = "https://github.com/teddymalhan/github-stacker-prs/issues"
	feedbackFormURL = "https://github.com/teddymalhan/github-stacker-prs/issues/new"
)

func FeedbackCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feedback [title]",
		Short: "Submit feedback for gl-stack",
		Long:  "Opens the gl-stack issue tracker to submit feedback. Optionally provide a title for the new issue.",
		Example: `  # Open the feedback form in your browser
  $ gl-stack feedback

  # Open with a pre-filled title
  $ gl-stack feedback "My feature request"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeedback(cfg, args)
		},
	}

	return cmd
}

func runFeedback(cfg *config.Config, args []string) error {
	targetURL := feedbackURL

	if len(args) > 0 {
		title := strings.Join(args, " ")
		targetURL = feedbackFormURL + "?title=" + url.QueryEscape(title)
	}

	b := browser.New("", cfg.Out, cfg.Err)
	if err := b.Browse(targetURL); err != nil {
		return err
	}

	cfg.Successf("Opening feedback form in your browser...")
	return nil
}
