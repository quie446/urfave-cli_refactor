package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlagValueSourcePrecedenceAndTracking(t *testing.T) {
	tests := []struct {
		name      string
		flag      Flag
		args      []string
		env       string
		fileValue string
		expected  any
		layer     FlagValueSourceLayer
		set       bool
	}{
		{name: "string command line overrides env file and code", flag: &StringFlag{Name: "name", Value: "code"}, args: []string{"--name", "cli"}, env: "env", fileValue: "file", expected: "cli", layer: CommandLineFlagValueSourceLayer, set: true},
		{name: "string env overrides file even when file is declared first", flag: &StringFlag{Name: "name", Value: "code"}, env: "env", fileValue: "file", expected: "env", layer: EnvironmentValueSourceLayer, set: true},
		{name: "string file overrides code default", flag: &StringFlag{Name: "name", Value: "code"}, fileValue: "file", expected: "file", layer: FileValueSourceLayer, set: true},
		{name: "string empty source is set and distinct from unset", flag: &StringFlag{Name: "name", Value: "code"}, env: "", expected: "", layer: EnvironmentValueSourceLayer, set: true},
		{name: "bool command line negation overrides env and code", flag: &BoolFlag{Name: "enabled", Value: true}, args: []string{"--enabled=false"}, env: "true", fileValue: "true", expected: false, layer: CommandLineFlagValueSourceLayer, set: true},
		{name: "bool env false overrides true code default", flag: &BoolFlag{Name: "enabled", Value: true}, env: "false", expected: false, layer: EnvironmentValueSourceLayer, set: true},
		{name: "int command line overrides env file and code", flag: &IntFlag{Name: "count", Value: 4}, args: []string{"--count", "1"}, env: "2", fileValue: "3", expected: 1, layer: CommandLineFlagValueSourceLayer, set: true},
		{name: "int env overrides file even when file is declared first", flag: &IntFlag{Name: "count", Value: 4}, env: "2", fileValue: "3", expected: 2, layer: EnvironmentValueSourceLayer, set: true},
		{name: "int file overrides code default", flag: &IntFlag{Name: "count", Value: 4}, fileValue: "3", expected: 3, layer: FileValueSourceLayer, set: true},
		{name: "unset optional flag remains code default", flag: &IntFlag{Name: "count", Value: 4}, expected: 4, layer: CodeDefaultValueSourceLayer, set: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sources := []ValueSource{}
			if test.fileValue != "" {
				path := filepath.Join(t.TempDir(), "value")
				require.NoError(t, os.WriteFile(path, []byte(test.fileValue), 0o600))
				sources = append(sources, File(path))
			}
			if test.env != "" || test.name == "string empty source is set and distinct from unset" || test.name == "bool env false overrides true code default" {
				t.Setenv("TEST_FLAG_SOURCE", test.env)
				sources = append(sources, EnvVar("TEST_FLAG_SOURCE"))
			}

			switch flag := test.flag.(type) {
			case *StringFlag:
				flag.Sources = NewValueSourceChain(sources...)
			case *BoolFlag:
				flag.Sources = NewValueSourceChain(sources...)
			case *IntFlag:
				flag.Sources = NewValueSourceChain(sources...)
			}

			var source FlagValueSource
			cmd := &Command{
				Name:  "test",
				Flags: []Flag{test.flag},
				Action: func(_ context.Context, cmd *Command) error {
					var err error
					source, err = cmd.FlagValueSource("name")
					if source.Layer == "" {
						source, err = cmd.FlagValueSource("enabled")
					}
					if source.Layer == "" {
						source, err = cmd.FlagValueSource("count")
					}
					return err
				},
			}

			require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, test.args...)))
			require.Equal(t, test.layer, source.Layer)
			require.Equal(t, test.set, source.Set)
			require.Equal(t, test.expected, source.Value)
			require.Equal(t, test.expected, cmd.Value(source.FlagName))
		})
	}
}

func TestFlagValueSourceInvalidValueReportsLayer(t *testing.T) {
	t.Setenv("TEST_FLAG_SOURCE", "not-an-int")
	cmd := &Command{
		Name: "test",
		Flags: []Flag{
			&IntFlag{Name: "count", Sources: EnvVars("TEST_FLAG_SOURCE")},
		},
	}

	err := cmd.Run(context.Background(), []string{"test"})
	require.ErrorContains(t, err, "layer: environment variable")
	require.ErrorContains(t, err, "TEST_FLAG_SOURCE")
}

func TestFlagValueSourceRequiredMissingError(t *testing.T) {
	cmd := &Command{
		Name: "test",
		Flags: []Flag{
			&StringFlag{Name: "name", Required: true},
		},
	}

	err := cmd.Run(context.Background(), []string{"test"})
	require.ErrorContains(t, err, `Required flag "name" not set`)
}

func TestBoolWithInverseNegativeFormSource(t *testing.T) {
	var source FlagValueSource
	cmd := &Command{
		Name: "test",
		Flags: []Flag{
			&BoolWithInverseFlag{Name: "enabled", Value: true},
		},
		Action: func(_ context.Context, cmd *Command) error {
			var err error
			source, err = cmd.FlagValueSource("enabled")
			return err
		},
	}

	require.NoError(t, cmd.Run(context.Background(), []string{"test", "--no-enabled"}))
	require.False(t, cmd.Bool("enabled"))
	require.Equal(t, CommandLineFlagValueSourceLayer, source.Layer)
	require.True(t, source.Set)
}

func TestSubcommandShadowsSameNamedParentFlag(t *testing.T) {
	var parentSource, childSource FlagValueSource
	cmd := &Command{
		Name: "name",
		Flags: []Flag{
			&StringFlag{Name: "name", Value: "parent-code"},
		},
		Commands: []*Command{
			{
				Name: "child",
				Flags: []Flag{
					&StringFlag{Name: "name", Value: "child-code"},
				},
				Action: func(_ context.Context, cmd *Command) error {
					var err error
					childSource, err = cmd.FlagValueSource("name")
					if err != nil {
						return err
					}
					parentSource, err = cmd.parent.FlagValueSource("name")
					return err
				},
			},
		},
	}

	require.NoError(t, cmd.Run(context.Background(), []string{"name", "child", "--name", "child-cli"}))
	require.Equal(t, "child-cli", childSource.Value)
	require.Equal(t, CommandLineFlagValueSourceLayer, childSource.Layer)
	require.Equal(t, "parent-code", parentSource.Value)
	require.Equal(t, CodeDefaultValueSourceLayer, parentSource.Layer)
}

func TestFlagValueSourceMismatchStopsQuery(t *testing.T) {
	flag := &StringFlag{Name: "name", Value: "code"}
	cmd := &Command{Name: "test", Flags: []Flag{flag}}
	require.NoError(t, flag.PreParse())
	flag.setFlagValueSource(FlagValueSource{
		Layer:    EnvironmentValueSourceLayer,
		Value:    "env",
		Set:      true,
		FlagName: "name",
	})
	require.NoError(t, flag.Set("name", "changed"))

	_, err := cmd.FlagValueSource("name")
	require.ErrorContains(t, err, "source mismatch")
	require.ErrorContains(t, err, "environment variable")
}
