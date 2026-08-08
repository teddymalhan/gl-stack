package config

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestColorFuncsAreBackgroundAware verifies that, when color is enabled, the
// Config's color functions resolve to background-aware (adaptive) colors so plain
// command output adapts to the terminal like the TUIs do.
func TestColorFuncsAreBackgroundAware(t *testing.T) {
	// Force color on (even though tests have no tty) and a color-capable profile.
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	beforeProfile := lipgloss.ColorProfile()
	beforeBg := lipgloss.HasDarkBackground()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(beforeProfile)
		lipgloss.SetHasDarkBackground(beforeBg)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)

	cfg := New()
	require.True(t, cfg.Terminal.IsColorEnabled(), "CLICOLOR_FORCE should enable color")

	for name, fn := range map[string]func(string) string{
		"ColorSuccess": cfg.ColorSuccess,
		"ColorError":   cfg.ColorError,
		"ColorWarning": cfg.ColorWarning,
		"ColorCyan":    cfg.ColorCyan,
		"ColorBlue":    cfg.ColorBlue,
		"ColorMagenta": cfg.ColorMagenta,
		"ColorGray":    cfg.ColorGray,
	} {
		t.Run(name, func(t *testing.T) {
			lipgloss.SetHasDarkBackground(true)
			dark := fn("x")
			lipgloss.SetHasDarkBackground(false)
			light := fn("x")
			assert.Contains(t, dark, "x")
			assert.NotEqual(t, dark, light, "%s should adapt to the terminal background", name)
		})
	}
}

func TestParseRemoteRepository(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		host   string
		owner  string
		repo   string
	}{
		{name: "SSH", remote: "git@gitlab.com:group/project.git", host: "gitlab.com", owner: "group", repo: "project"},
		{name: "HTTPS subgroup", remote: "https://gitlab.example/platform/team/project.git", host: "gitlab.example", owner: "platform/team", repo: "project"},
		{name: "SSH URL", remote: "ssh://git@gitlab.example/platform/project.git", host: "gitlab.example", owner: "platform", repo: "project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRemoteRepository(tt.remote)
			require.NoError(t, err)
			assert.Equal(t, tt.host, got.Host)
			assert.Equal(t, tt.owner, got.Owner)
			assert.Equal(t, tt.repo, got.Name)
		})
	}
}
