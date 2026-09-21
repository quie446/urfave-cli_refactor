package cli

import "fmt"

// FlagOrigin records where the final value of a flag came from after the
// merge chain (command line > environment > file/external > code default)
// has run.
type FlagOrigin struct {
	// Name is the resolved flag name.
	Name string
	// Layer is the merge layer that supplied the final value.
	Layer SourceLayer
	// Source describes the concrete source within the layer. It is nil for
	// the command line and code default layers.
	Source ValueSource
	// Value is the final resolved value held by the flag.
	Value any
	// Set reports whether the flag carries an explicitly assigned value.
	Set bool
}

// String renders the origin, including the concrete source when present.
func (o FlagOrigin) String() string {
	if o.Source != nil {
		return fmt.Sprintf("flag %q value %v from layer %s (%s)", o.Name, o.Value, o.Layer, o.Source)
	}
	return fmt.Sprintf("flag %q value %v from layer %s", o.Name, o.Value, o.Layer)
}

// originTracker is implemented by flags that remember which source in
// their Sources chain supplied the value during the PostParse merge.
type originTracker interface {
	resolvedValueSource() ValueSource
}

func (f *FlagBase[T, C, V]) resolvedValueSource() ValueSource { return f.resolvedSource }

func (bif *BoolWithInverseFlag) resolvedValueSource() ValueSource { return bif.resolvedSource }

// TraceFlagOrigin traces which merge layer supplied the final value of the
// named flag for cmd after parsing. It resolves the flag through the same
// lookup the merge chain uses, so a subcommand flag shadows a same-named
// flag on a parent command.
//
// It returns an error instead of guessing when the flag does not exist or
// when the recorded source cannot be reconciled with the flag's current
// state.
func (cmd *Command) TraceFlagOrigin(name string) (FlagOrigin, error) {
	if cmd == nil {
		return FlagOrigin{}, fmt.Errorf("cannot trace flag %q on nil command", name)
	}

	f := cmd.lookupAppliedFlag(name)
	if f == nil {
		return FlagOrigin{}, fmt.Errorf("flag %q is not defined for command %q", name, cmd.Name)
	}

	return flagOrigin(cmd, name, f)
}

func flagOrigin(_ *Command, name string, f Flag) (FlagOrigin, error) {
	origin := FlagOrigin{
		Name:  name,
		Value: f.Get(),
		Set:   f.IsSet(),
	}

	var onCommandLine bool
	if cm, ok := f.(cliSetMarker); ok {
		onCommandLine = cm.wasCLISet()
	}

	var tracked ValueSource
	if ot, ok := f.(originTracker); ok {
		tracked = ot.resolvedValueSource()
	}

	switch {
	case onCommandLine:
		if f.IsSet() {
			origin.Layer = LayerCLI
			return origin, nil
		}
		// The merge chain marks every command line assignment as set; a
		// recorded CLI hit that is unset is an inconsistent merge.
		return FlagOrigin{}, fmt.Errorf(
			"flag %q was recorded on the command line but is not set; merge state is inconsistent",
			name,
		)

	case tracked != nil:
		// The value came from the Sources chain. Cross-check that the
		// recorded source still resolves and agrees with the chain, so the
		// reported origin can never disagree with the real merge.
		if !f.IsSet() {
			return FlagOrigin{}, fmt.Errorf(
				"flag %q records source %s but is not set; merge state is inconsistent",
				name, tracked,
			)
		}
		_, src, ok := sourceChainOf(f).LookupWithSource()
		if !ok {
			return FlagOrigin{}, fmt.Errorf(
				"flag %q claims source %s but no source resolves; merge state is inconsistent",
				name, tracked,
			)
		}
		if src != tracked {
			return FlagOrigin{}, fmt.Errorf(
				"flag %q was merged from %s but %s resolves now; merge state is inconsistent",
				name, tracked, src,
			)
		}
		origin.Layer = layerOf(tracked)
		origin.Source = tracked
		return origin, nil

	default:
		if f.IsSet() {
			// Set without a CLI hit and without a tracked source means the
			// merge state cannot be explained by the locked chain.
			return FlagOrigin{}, fmt.Errorf(
				"flag %q is set but its source layer cannot be determined; merge state is inconsistent",
				name,
			)
		}
		origin.Layer = LayerCode
		return origin, nil
	}
}

func sourceChainOf(f Flag) *ValueSourceChain {
	type chainCarrier interface {
		getSources() *ValueSourceChain
	}
	if c, ok := f.(chainCarrier); ok {
		return c.getSources()
	}
	return &ValueSourceChain{}
}

func (f *FlagBase[T, C, V]) getSources() *ValueSourceChain { return &f.Sources }

func (bif *BoolWithInverseFlag) getSources() *ValueSourceChain { return &bif.Sources }

// cliSetMarker is implemented by flags that can record a command line
// assignment separately from an env/file assignment.
type cliSetMarker interface {
	markCLISet()
	wasCLISet() bool
}

func (f *FlagBase[T, C, V]) markCLISet()     { f.cliSet = true }
func (f *FlagBase[T, C, V]) wasCLISet() bool { return f.cliSet }

func (bif *BoolWithInverseFlag) markCLISet()     { bif.cliSet = true }
func (bif *BoolWithInverseFlag) wasCLISet() bool { return bif.cliSet }
