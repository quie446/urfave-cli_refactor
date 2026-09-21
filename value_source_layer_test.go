package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayerOfBuiltinSources(t *testing.T) {
	r := require.New(t)

	r.Equal(LayerEnv, layerOf(EnvVar("X")))
	r.Equal(LayerFile, layerOf(File("/tmp/x")))
	ms := NewMapSource("cfg", map[any]any{"x": 1})
	r.Equal(LayerFile, layerOf(NewMapValueSource("x", ms)))
	r.Equal(LayerFile, layerOf(&staticValueSource{v: "s"}))

	r.Equal(0, layerPrecedence(LayerCLI))
	r.Equal(1, layerPrecedence(LayerEnv))
	r.Equal(2, layerPrecedence(LayerFile))
	r.Equal(3, layerPrecedence(LayerCode))
}

func TestValidateSourceOrder(t *testing.T) {
	t.Run("accepts env before file", func(t *testing.T) {
		chain := NewValueSourceChain(EnvVar("X"), File("/tmp/x"))
		require.NoError(t, chain.ValidateOrder("f"))
	})

	t.Run("accepts chain with only one layer", func(t *testing.T) {
		chain := NewValueSourceChain(File("/tmp/a"), File("/tmp/b"))
		require.NoError(t, chain.ValidateOrder("f"))
	})

	t.Run("rejects file before env", func(t *testing.T) {
		chain := NewValueSourceChain(File("/tmp/x"), EnvVar("X"))
		err := chain.ValidateOrder("f")
		require.Error(t, err)

		var orderErr *errInvalidSourceOrder
		require.ErrorAs(t, err, &orderErr)
		assert.Contains(t, err.Error(), `"f"`)
		assert.Contains(t, err.Error(), string(LayerFile))
		assert.Contains(t, err.Error(), string(LayerEnv))
	})
}

func newFileSource(t *testing.T, contents string) ValueSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cfg")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return File(path)
}

// runAndTrace executes cmd with args and traces the origin of flagName on
// the action command. It fails the test when Run returns an error.
func runAndTrace(t *testing.T, cmd *Command, args []string, flagName string) FlagOrigin {
	t.Helper()
	r := require.New(t)

	var traced FlagOrigin
	if cmd.Action == nil {
		cmd.Action = func(ctx context.Context, c *Command) error {
			o, err := c.TraceFlagOrigin(flagName)
			if err != nil {
				return err
			}
			traced = o
			return nil
		}
	}

	r.NoError(cmd.Run(context.Background(), append([]string{"app"}, args...)))
	return traced
}

func TestTraceOriginStringFlag(t *testing.T) {
	t.Run("code default when nothing set", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val"},
		}}
		o := runAndTrace(t, cmd, nil, "name")
		assert.Equal(t, LayerCode, o.Layer)
		assert.Equal(t, "code-val", o.Value)
		assert.False(t, o.Set)
	})

	t.Run("file layer", func(t *testing.T) {
		fileSrc := newFileSource(t, "file-val")
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val", Sources: NewValueSourceChain(fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "name")
		assert.Equal(t, LayerFile, o.Layer)
		assert.Equal(t, "file-val", o.Value)
		assert.True(t, o.Set)
	})

	t.Run("env beats file", func(t *testing.T) {
		t.Setenv("APP_NAME", "env-val")
		fileSrc := newFileSource(t, "file-val")
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val",
				Sources: NewValueSourceChain(EnvVar("APP_NAME"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "name")
		assert.Equal(t, LayerEnv, o.Layer)
		assert.Equal(t, "env-val", o.Value)
	})

	t.Run("file wins when env unset", func(t *testing.T) {
		fileSrc := newFileSource(t, "file-val")
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val",
				Sources: NewValueSourceChain(EnvVar("APP_NAME_UNSET_ZZ"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "name")
		assert.Equal(t, LayerFile, o.Layer)
		assert.Equal(t, "file-val", o.Value)
	})

	t.Run("command line beats env and file", func(t *testing.T) {
		t.Setenv("APP_NAME", "env-val")
		fileSrc := newFileSource(t, "file-val")
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val",
				Sources: NewValueSourceChain(EnvVar("APP_NAME"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, []string{"--name", "cli-val"}, "name")
		assert.Equal(t, LayerCLI, o.Layer)
		assert.Equal(t, "cli-val", o.Value)
		assert.Nil(t, o.Source)
	})

	t.Run("empty env text is set and beats file default", func(t *testing.T) {
		t.Setenv("APP_NAME", "")
		fileSrc := newFileSource(t, "file-val")
		cmd := &Command{Flags: []Flag{
			&StringFlag{Name: "name", Value: "code-val",
				Sources: NewValueSourceChain(EnvVar("APP_NAME"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "name")
		assert.Equal(t, LayerEnv, o.Layer)
		assert.Equal(t, "", o.Value)
		assert.True(t, o.Set, "empty text from env is still an explicitly set value")
	})
}

func TestTraceOriginBoolFlag(t *testing.T) {
	t.Run("inverse form on command line", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&BoolWithInverseFlag{Name: "debug", Value: true},
		}}
		o := runAndTrace(t, cmd, []string{"--no-debug"}, "debug")
		assert.Equal(t, LayerCLI, o.Layer)
		assert.Equal(t, false, o.Value)
	})

	t.Run("positive form on command line beats code default", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&BoolWithInverseFlag{Name: "debug", Value: false},
		}}
		o := runAndTrace(t, cmd, []string{"--debug"}, "debug")
		assert.Equal(t, LayerCLI, o.Layer)
		assert.Equal(t, true, o.Value)
	})

	t.Run("env bool with empty text resolves false", func(t *testing.T) {
		t.Setenv("DEBUG", "")
		cmd := &Command{Flags: []Flag{
			&BoolFlag{Name: "debug", Value: true, Sources: EnvVars("DEBUG")},
		}}
		o := runAndTrace(t, cmd, nil, "debug")
		assert.Equal(t, LayerEnv, o.Layer)
		assert.Equal(t, false, o.Value)
	})

	t.Run("code default false when unset", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&BoolFlag{Name: "debug", Value: false},
		}}
		o := runAndTrace(t, cmd, nil, "debug")
		assert.Equal(t, LayerCode, o.Layer)
		assert.Equal(t, false, o.Value)
	})
}

func TestTraceOriginIntFlag(t *testing.T) {
	t.Run("command line", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&IntFlag{Name: "n", Value: 1},
		}}
		o := runAndTrace(t, cmd, []string{"--n", "42"}, "n")
		assert.Equal(t, LayerCLI, o.Layer)
		assert.Equal(t, int(42), o.Value)
	})

	t.Run("env beats file beats code", func(t *testing.T) {
		t.Setenv("N", "7")
		fileSrc := newFileSource(t, "5")
		cmd := &Command{Flags: []Flag{
			&IntFlag{Name: "n", Value: 1,
				Sources: NewValueSourceChain(EnvVar("N"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "n")
		assert.Equal(t, LayerEnv, o.Layer)
		assert.Equal(t, int(7), o.Value)
	})

	t.Run("file value when env unset", func(t *testing.T) {
		fileSrc := newFileSource(t, "5")
		cmd := &Command{Flags: []Flag{
			&IntFlag{Name: "n", Value: 1,
				Sources: NewValueSourceChain(EnvVar("N_UNSET_ZZ"), fileSrc)},
		}}
		o := runAndTrace(t, cmd, nil, "n")
		assert.Equal(t, LayerFile, o.Layer)
		assert.Equal(t, int(5), o.Value)
	})

	t.Run("code default", func(t *testing.T) {
		cmd := &Command{Flags: []Flag{
			&IntFlag{Name: "n", Value: 1},
		}}
		o := runAndTrace(t, cmd, nil, "n")
		assert.Equal(t, LayerCode, o.Layer)
		assert.Equal(t, int(1), o.Value)
	})
}

func TestTraceUnknownFlagErrors(t *testing.T) {
	cmd := &Command{Flags: []Flag{&StringFlag{Name: "name"}}}
	var traceErr error
	cmd.Action = func(ctx context.Context, c *Command) error {
		_, traceErr = c.TraceFlagOrigin("missing")
		return nil
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"app"}))
	require.Error(t, traceErr)
	assert.Contains(t, traceErr.Error(), "not defined")
}

func TestSubcommandShadowsSameNamedFlag(t *testing.T) {
	// Both root and sub define "name"; the sub's own flag must win, and
	// tracing it on the sub resolves the sub's layer, not the parent's.
	cmd := &Command{
		Flags: []Flag{&StringFlag{Name: "name", Value: "root-code"}},
		Commands: []*Command{{
			Name:  "sub",
			Flags: []Flag{&StringFlag{Name: "name", Value: "sub-code"}},
		}},
	}

	var traced FlagOrigin
	cmd.Commands[0].Action = func(ctx context.Context, c *Command) error {
		o, err := c.TraceFlagOrigin("name")
		if err != nil {
			return err
		}
		traced = o
		return nil
	}

	require.NoError(t, cmd.Run(context.Background(), []string{"app", "sub"}))
	assert.Equal(t, LayerCode, traced.Layer)
	assert.Equal(t, "sub-code", traced.Value)

	// CLI value for the sub's shadowing flag resolves on the CLI layer.
	var cliTrace FlagOrigin
	cmd.Commands[0].Action = func(ctx context.Context, c *Command) error {
		o, err := c.TraceFlagOrigin("name")
		if err != nil {
			return err
		}
		cliTrace = o
		return nil
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"app", "sub", "--name", "sub-cli"}))
	assert.Equal(t, LayerCLI, cliTrace.Layer)
	assert.Equal(t, "sub-cli", cliTrace.Value)
}

func TestPersistentFlagTracedFromSubcommand(t *testing.T) {
	t.Setenv("PERSIST", "env-val")
	cmd := &Command{
		Flags: []Flag{&StringFlag{Name: "persist", Value: "code",
			Sources: EnvVars("PERSIST")}},
		Commands: []*Command{{Name: "sub"}},
	}

	var traced FlagOrigin
	cmd.Commands[0].Action = func(ctx context.Context, c *Command) error {
		o, err := c.TraceFlagOrigin("persist")
		if err != nil {
			return err
		}
		traced = o
		return nil
	}
	require.NoError(t, cmd.Run(context.Background(), []string{"app", "sub"}))
	assert.Equal(t, LayerEnv, traced.Layer)
	assert.Equal(t, "env-val", traced.Value)
}

func TestInvalidSourceOrderStopsInstall(t *testing.T) {
	// File placed before env: env can never win, contradicting the locked
	// order. Setup must fail and report both layers.
	fileSrc := newFileSource(t, "file-val")
	cmd := &Command{Flags: []Flag{
		&StringFlag{Name: "name",
			Sources: NewValueSourceChain(fileSrc, EnvVar("NAME"))},
	}}
	var actionRan bool
	cmd.Action = func(ctx context.Context, c *Command) error {
		actionRan = true
		return nil
	}
	err := cmd.Run(context.Background(), []string{"app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source order")
	assert.Contains(t, err.Error(), string(LayerFile))
	assert.Contains(t, err.Error(), string(LayerEnv))
	assert.False(t, actionRan, "action must not run when ordering fails")
}

func TestRequiredMissingStillReportsRequiredError(t *testing.T) {
	cmd := &Command{Flags: []Flag{
		&StringFlag{Name: "name", Required: true},
	}}
	err := cmd.Run(context.Background(), []string{"app"})
	require.Error(t, err)

	var reqErr requiredFlagsErr
	require.ErrorAs(t, err, &reqErr)
}

func TestRequiredSatisfiedBySource(t *testing.T) {
	t.Setenv("NAME", "env-val")
	cmd := &Command{Flags: []Flag{
		&StringFlag{Name: "name", Required: true, Sources: EnvVars("NAME")},
	}}
	require.NoError(t, cmd.Run(context.Background(), []string{"app"}))
}
