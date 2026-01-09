# multibuild

A simple tool to make cross compilation and releasing Go binaries a little simpler.

This is a wrapper around `go build`. At its core, it repeatedly runs `go build` with 
`GOOS`/`GOARCH` set for you.

**NOTE**: For the time being, there is no backwards compatibility guaranteed. This is still a work in progress.

# Usage

multibuild is designed to be installed as a Go tool (Go 1.24+):

	go get -tool github.com/rburchell/multibuild/cmd/multibuild@master

And then you can build your binaries from within your module using `go tool multibuild`,
which will build the current package for the configured targets.

# Configuration

multibuild can be configured by comments in the source code of the package you're building, for example:

```go
//go:multibuild:output=bin/${TARGET}-${GOOS}-${GOARCH}
//go:multibuild:include=linux/*
//go:multibuild:exclude=linux/ppc64,linux/ppc64le
```

Any comment on a line like this:

`//go:multibuild:`

... will be looked at by multibuild for it to configure something.

## Build targets

By default, multibuild will build for all available `GOOS`/`GOARCH` pairs, as discovered by
`go tool dist list`. This will produce quite a lot of binaries; many of them might not be
applicable, depending on what your project is. Fortunately, this behaviour can be
configured to suit your liking, by adding filters. For example, to build all Linux targets
except 32-bit x86:

```go
//go:multibuild:include=linux/*
//go:multibuild:exclude=linux/386
```

A filter is either an exact match or a partial match: an exact match must exactly
match the available targets. With a partial match, you are able to use `*` in a filter
to match all `GOOS` or all `GOARCH` depending on if it appears before or after the `/`.

Filters are stored in a whitelist (`include`), a blacklist (`exclude`), or a combination of both.
The ordering is such that `include` filters apply to gather a subset of `go tool dist list`,
and then `exclude` filters remove any remaining entries that are unwanted.

Input and output filters are merged across all source files in the package.

### Include target filters

If you want to only build on certain platforms, you can use an `include` directive,
like this one, which will build for all Linux platforms:

`//go:multibuild:include=linux/*`

If you don't specify any `include`, then the default is to build for all GOOS/GOARCH,
i.e. `go:multibuild:include=*/*`.

### Exclude target filters

If you want to narrow down the selection, you can use an `exclude` directive,
like this one, which will remove the darwin/arm64 build:

`//go:multibuild:exclude=darwin/arm64`

## Output naming

By default, binaries are named e.g. mytarget-linux-amd64. This is configurable, for example:

`//go:multibuild:output=bin/${TARGET}-${GOOS}-${GOARCH}`

This configuration will use the same naming, but place all binaries in a `bin/` subdirectory.
An `output` configuration must have all three `${TARGET}`, `${GOOS}`, `${GOARCH}`
placeholders present, but the ordering can change.

Windows, as a special case, will always have ".exe" appended to the filename.

The `TARGET` placeholder expands to the default build target name that `go build` would produce.
The `GOOS` placeholder is expands to the `GOOS` under build.
The `GOARCH` placeholder expands to the `GOARCH` under build.

Only a single `output` directive may be found in a package.

# Differences to `go build`

As multibuild is a wrapper around `go build`, most of the behaviour you will see come from there.
This section is an attempt to document the areas where there are differences, and why.

## Verbose

multibuild adds its own verbose output indicating when different targets start/finish if you pass `-v`.

## Output Prefixing

Output from all builds is prefixed with `GOOS/GOARCH: `, e.g. instead of `go build saying stuff`,
you will see `linux/arm64: go build saying stuff`.

I think this is generally useful, and the only way to get sane output you can act on,
so there is no configuration knob to disable it at this time.

## Cgo

Since the primary purpose of `multibuild` is to cross compile, the use of cgo isn't really
something I have thought about or focused on: I personally just switch it off and call it a day,
so that my binaries run in more places.

So `multibuild` forces `CGO_ENABLED=0` by default.

This choice might not be for everyone, though, so `multibuild` will not complain if you explicitly
choose to enable it, e.g. by running `CGO_ENABLED=1 go tool multibuild`.

There is presently no source code configuration for this - if such a thing would be useful, I would
be interested to hear about it.

# Non-goals

I want multibuild to be fairly focused. I like the premise of tools like Goreleaser,
but I think that they try to do too much, and require too much hand holding.

* I don't want to start generating changelogs.
* I don't want to upload any binaries.
* I don't want to send any notifications.
* I don't want to run tests, or vet/lint checks, etc.
* My sole focus is on simple Go binaries you generally work on via `go build`.

At the end of the day, this is intended to be a build/package tool, and while I'm open
to new ideas and contributions, I think the focus should stay on things towards that
end of the spectrum.

# Future

Not set in stone, but some ideas for the future...

## iOS / Android

Some platforms (particularly `android` and `ios`) require `CGO_ENABLED=1`
to build. As this tool is primarily intended as an aid for cross compilation, and
`CGO_ENABLED` complicates those cases, for the time being, these platforms are
disabled, as I don't use them anyway. Thoughts about how to handle them are welcome.

## Signing

I have no solid thoughts here, but I guess it's inevitable that sooner or later I'm
going to have to deal with binary signing, at least for the platforms where that's a thing.

## Archiving

I think supporting optional archiving would also be a nice touch,
and probably go a long way to making this a capable release tool.

Something like this:

```go
//go:multibuild:format=raw,tar.gz                # binary + archive
//go:multibuild:embed=README.md                  # embeds in the archive
//go:multibuild:embed=LICENSE.txt@doc/           # embeds in the archive, at a specified path.
```

`format` here would specify the type(s) of output (e.g. `format=raw,tar.gz`).
The default would be `raw`, i.e. binary, unless embeds are found(?)

`embed` would allow files to be placed inside the archive.

One complication that I haven't yet thought through is how to handle
files that should
only be distributed on a subset of platforms - needs a bit more thought.

## Docker

Haven't really thought this through, but in conjunction with the archiving idea,
it might not be impossible to think about building a Go binary, and at the same time
producing a docker image containing it.

But this might be scope creep, and something for a separate tool.
