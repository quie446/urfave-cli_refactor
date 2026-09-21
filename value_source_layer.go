package cli

import (
	"fmt"
	"strings"
)

// SourceLayer identifies which layer of the value merge chain a value
// originates from. The merge order, from highest precedence to lowest, is:
//
//	LayerCLI  (command line flags)
//	LayerEnv  (environment variables)
//	LayerFile (files and other external value sources)
//	LayerCode (the value assigned to the flag in code, the code default)
//
// A value resolved from a higher layer always wins over a value from a
// lower layer.
type SourceLayer string

const (
	// LayerCLI is the command line layer: flags passed as program arguments.
	LayerCLI SourceLayer = "cli"
	// LayerEnv is the environment variable layer.
	LayerEnv SourceLayer = "env"
	// LayerFile is the file/external-source layer.
	LayerFile SourceLayer = "file"
	// LayerCode is the in-code default layer (the flag's configured Value).
	LayerCode SourceLayer = "code"
)

// layerPrecedence maps a layer to its precedence rank. Lower numbers win.
func layerPrecedence(l SourceLayer) int {
	switch l {
	case LayerCLI:
		return 0
	case LayerEnv:
		return 1
	case LayerFile:
		return 2
	case LayerCode:
		return 3
	default:
		// Unknown external sources behave as file/external sources.
		return 2
	}
}

// LayeredValueSource is an optional interface a ValueSource can implement
// to declare the merge layer it belongs to. Built-in sources created via
// [EnvVar], [File] and [NewMapValueSource] implement it. Sources that do
// not implement it are treated as [LayerFile].
type LayeredValueSource interface {
	ValueSource
	Layer() SourceLayer
}

// layerOf reports the merge layer of a ValueSource. Sources that do not
// implement LayeredValueSource default to the file/external layer so that
// externally implemented sources keep working.
func layerOf(src ValueSource) SourceLayer {
	if ls, ok := src.(LayeredValueSource); ok {
		return ls.Layer()
	}
	return LayerFile
}

// errInvalidSourceOrder is returned during flag setup when the Sources
// chain of a flag contradicts the locked merge order: an environment
// source must never be shadowed by a file/external source placed earlier
// in the chain.
type errInvalidSourceOrder struct {
	flagName string
	earlier  ValueSource
	later    ValueSource
}

func (e *errInvalidSourceOrder) Error() string {
	return fmt.Sprintf(
		"flag %[1]q has invalid source order: %[2]s (layer %[3]s) is placed before %[4]s (layer %[5]s); "+
			"locked order is command line > environment > file/external > code default",
		e.flagName, e.earlier, layerOf(e.earlier), e.later, layerOf(e.later),
	)
}

// validateSourceOrder enforces the locked layer ordering inside a single
// Sources chain. Within the chain a higher-precedence layer (env) must not
// appear after a lower-precedence layer (file/external), because the first
// matching source wins and that ordering would let a file silently shadow
// an environment variable.
func validateSourceOrder(flagName string, chain []ValueSource) error {
	for i := 1; i < len(chain); i++ {
		for j := 0; j < i; j++ {
			if layerPrecedence(layerOf(chain[j])) > layerPrecedence(layerOf(chain[i])) {
				return &errInvalidSourceOrder{
					flagName: flagName,
					earlier:  chain[j],
					later:    chain[i],
				}
			}
		}
	}
	return nil
}

// Layer returns the merge layer of the source that produced the chain's
// resolved value. It must only be called after a successful Lookup; when no
// source resolves it returns [LayerFile].
func (vsc *ValueSourceChain) Layer() SourceLayer {
	_, src, ok := vsc.LookupWithSource()
	if !ok || src == nil {
		return LayerFile
	}
	return layerOf(src)
}

// ValidateOrder checks that the chain honours the locked merge order.
func (vsc *ValueSourceChain) ValidateOrder(flagName string) error {
	return validateSourceOrder(flagName, vsc.Chain)
}

// describeLayer renders a layer for error messages, including its source
// when known.
func describeLayer(layer SourceLayer, src ValueSource) string {
	var b strings.Builder
	b.WriteString(string(layer))
	if src != nil {
		b.WriteString(" (")
		b.WriteString(src.String())
		b.WriteString(")")
	}
	return b.String()
}

func (e *envVarValueSource) Layer() SourceLayer { return LayerEnv }

func (f *fileValueSource) Layer() SourceLayer { return LayerFile }

func (mvs *mapValueSource) Layer() SourceLayer { return LayerFile }
