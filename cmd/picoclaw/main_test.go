package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
)

func TestNewPicoclawCommand(t *testing.T) {
	cmd := NewPicoclawCommand()

	require.NotNil(t, cmd)

	short := fmt.Sprintf("%s picoclaw - Personal AI Assistant v%s\n\n", internal.Logo, internal.GetVersion())

	assert.Equal(t, "picoclaw", cmd.Use)
	assert.Equal(t, short, cmd.Short)

	assert.True(t, cmd.HasSubCommands())
	assert.True(t, cmd.HasAvailableSubCommands())

	assert.False(t, cmd.HasFlags())

	assert.Nil(t, cmd.Run)
	assert.Nil(t, cmd.RunE)

	assert.Nil(t, cmd.PersistentPreRun)
	assert.Nil(t, cmd.PersistentPostRun)

	allowedCommands := []string{
		"agent",
		"auth",
		"cron",
		"gateway",
		"mcp-feishu-doc",
		"migrate",
		"onboard",
		"skills",
		"status",
		"version",
	}

	subcommands := cmd.Commands()
	assert.Len(t, subcommands, len(allowedCommands))

	for _, subcmd := range subcommands {
		found := slices.Contains(allowedCommands, subcmd.Name())
		assert.True(t, found, "unexpected subcommand %q", subcmd.Name())

		assert.False(t, subcmd.Hidden)
	}
}

func TestShouldPrintBanner(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "default command",
			args: []string{"picoclaw"},
			want: true,
		},
		{
			name: "regular subcommand",
			args: []string{"picoclaw", "version"},
			want: true,
		},
		{
			name: "regular subcommand with global flag",
			args: []string{"picoclaw", "--help"},
			want: true,
		},
		{
			name: "mcp feishu doc root command",
			args: []string{"picoclaw", "mcp-feishu-doc"},
			want: false,
		},
		{
			name: "mcp feishu doc serve subcommand",
			args: []string{"picoclaw", "mcp-feishu-doc", "serve"},
			want: false,
		},
		{
			name: "mcp feishu doc with trailing flag",
			args: []string{"picoclaw", "mcp-feishu-doc", "--help"},
			want: false,
		},
		{
			name: "double dash stops command detection",
			args: []string{"picoclaw", "--", "mcp-feishu-doc"},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shouldPrintBanner(tc.args))
		})
	}
}
