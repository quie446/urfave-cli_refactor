# Value sources: override order and origin tracing

This page is the migration note for the parameter override tracking
refactor. It pins down the merge order, how to trace where a flag value
came from, and what changes when upgrading.

## Locked override order

Every flag value is resolved through the **existing** merge chain. There is
no separate parser. From highest precedence to lowest:

```
command line > environment > file/external source > code default
```

1. A value passed on the command line always wins.
2. Otherwise the first matching entry in the flag's `Sources` chain wins.
   Within that chain, environment sources (`EnvVar`/`EnvVars`) must come
   before file/external sources (`File`/`Files`/`NewMapValueSource` or a
   custom `ValueSource`).
3. If no source resolves, the code default (`Value`) is used.

Sources that do not implement `LayeredValueSource` are treated as
file/external sources, so existing custom sources keep working.

## What now fails loudly

Placing a file/external source **before** an environment source used to let
the file silently shadow the environment variable. It is now rejected while
the flag is being set up (during `PostParse`), before any action runs:

```
flag "name" has invalid source order: file "cfg" (layer file) is placed
before environment variable "NAME" (layer env); locked order is
command line > environment > file/external > code default
```

The error names both the flag and the two layers involved, so it is clear
which layer the chain got stuck at. Reorder the chain so environment
variables come first.

## Tracing where a value came from

After a command has parsed, call `Command.TraceFlagOrigin` with the flag
name:

```go
origin, err := cmd.TraceFlagOrigin("name")
if err != nil {
    // the flag does not exist, or the recorded source does not agree
    // with the actual merge
    return err
}
fmt.Println(origin.Layer) // cli, env, file or code
fmt.Println(origin.Value) // the real merged value
```

`FlagOrigin.Layer` is one of `LayerCLI`, `LayerEnv`, `LayerFile` or
`LayerCode`. `FlagOrigin.Source` carries the concrete source for the env
and file layers and is nil for the command line and code default layers.

String, bool and integer flags are all covered. Empty text is
distinguishable from "not set": an empty environment value is a resolved
env value (`Set == true`), while an unset flag falls through to the code
default (`Set == false`, layer `code`).

Tracing never guesses. If the flag is unknown, or the recorded source can
no longer be reconciled with the flag's current state, `TraceFlagOrigin`
returns an error instead of reporting a layer.

## Same-named flags on parent and subcommand

A subcommand flag shadows a same-named parent flag, exactly as during
parsing. Tracing the name on the subcommand reports the subcommand's layer.
Persistent parent flags are traced through the same chain when the
subcommand does not shadow them.

## Migration checklist

- Keep existing `Sources` usages; chains that already list environment
  variables before files behave exactly as before.
- Audit any chain that places a `File`/map source before an `EnvVar` and
  reorder it. Such chains previously shadowed environment variables and now
  return an explicit error.
- Required flags may still be satisfied by any layer, including env and
  file sources; a missing required value still reports the required-flags
  error.
- Use `TraceFlagOrigin` for diagnostics; it is read-only and does not
  change parsing.
- Shell completion behavior and the public flag constructors are
  unchanged.
