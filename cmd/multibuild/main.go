// Copyright 2025 Robin Burchell. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A simplistic tool to build Go binaries for multiple platforms.
package main

//go:multibuild:output=bin/${TARGET}-${GOOS}-${GOARCH}

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func displayUsageAndExit(self string) {
	fmt.Fprintf(os.Stderr, "usage: %s [-o output] [build flags] [packages]\n", self)
	fmt.Fprintln(os.Stderr, "multibuild is a thin wrapper around 'go build'.")
	fmt.Fprintln(os.Stderr, "For documentation on multibuild's configuration, see https://github.com/rburchell/multibuild")
	fmt.Fprintln(os.Stderr, "Otherwise, run 'go help build' for command line flags.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "multibuild-specific options:")
	fmt.Fprintln(os.Stderr, "    -v: enable verbose logs during building. this will also imply `go build -v`")
	fmt.Fprintln(os.Stderr, "    --multibuild-configuration: display the multibuild configuration parsed from the package")
	fmt.Fprintln(os.Stderr, "    --multibuild-targets: list targets that will be built")
	os.Exit(0)
}

func displayConfigAndExit(opts options) {
	fmt.Fprintf(os.Stderr, "//go:multibuild:include=%s\n", strings.Join(mapSlice(opts.Include, func(f filter) string { return string(f) }), ","))
	fmt.Fprintf(os.Stderr, "//go:multibuild:exclude=%s\n", strings.Join(mapSlice(opts.Exclude, func(f filter) string { return string(f) }), ","))
	fmt.Fprintf(os.Stderr, "//go:multibuild:output=%s\n", opts.Output)
	fmt.Fprintf(os.Stderr, "//go:multibuild:format=%s\n", strings.Join(mapSlice(opts.Format, func(f format) string { return string(f) }), ","))
	os.Exit(0)
}

func displayTargetsAndExit(targets []target) {
	for _, target := range targets {
		fmt.Fprintln(os.Stderr, target)
	}
	os.Exit(0)
}

type cliArgs struct {
	// The current binary name.
	self string

	// The args for go build, with [0] (this binary) stripped off.
	goBuildArgs []string

	// -o arg, or -o=
	// In case it's not specified explicitly, it is autodetected.
	output string

	// The package path being built
	// In case it's not specified explicitly, it is set to ".".
	packagePath string

	// The sources to be built
	// This will usually, but not always, be empty.
	// (e.g. multibuild foo/main.go)
	sources []string

	displayUsage   bool
	displayConfig  bool
	displayTargets bool
	verbose        bool
}

func buildArgs() (cliArgs, error) {
	args := cliArgs{}
	args.self = filepath.Base(os.Args[0])
	args.goBuildArgs = os.Args[1:]
	expectOutput := false // seen -o, waiting for the rest

	for _, arg := range args.goBuildArgs {
		switch {
		case expectOutput:
			args.output = arg
			expectOutput = false
		case arg == "-o":
			expectOutput = true
		case strings.HasPrefix(arg, "-o="):
			args.output = strings.TrimPrefix(arg, "-o=")

		case arg == "-h" || arg == "--help":
			args.displayUsage = true

		case arg == "-v":
			args.verbose = true
		case arg == "--multibuild-configuration":
			args.displayConfig = true
		case arg == "--multibuild-targets":
			args.displayTargets = true
		case strings.HasPrefix(arg, "--multibuild"):
			return cliArgs{}, fmt.Errorf("multibuild: unrecognized argument %q", arg)
		case !strings.HasPrefix(arg, "-"):
			if args.packagePath != "" {
				// For now, I'm cowardly refusing to handle this.
				// I think we need to refactor some to handle two cases:
				// - specifying a list of .go sources in a single ultimate package
				// - specifying a list of packages
				//
				// The former is handled quite easily I think, the latter will
				// require some additional thought and handling, as it's essentially
				// another level of looping on top of what we have now.
				//
				// We will need to discover sources for each package, scan independently,
				// and build independently.
				return cliArgs{}, fmt.Errorf("multibuild: cannot build multiple packages")
			}
			args.packagePath = arg
		}
	}

	if args.packagePath == "" {
		args.packagePath = "."
	}

	if args.output == "" {
		if args.packagePath == "." {
			// implicit case: multibuild on the current dir -> multibuild .
			args.packagePath = "."
			wd, err := os.Getwd()
			if err != nil {
				fatal("multibuild: failed to get cwd: %s", err)
			}
			args.output = filepath.Base(wd)
		} else {
			t := args.packagePath
			if strings.HasSuffix(t, ".go") {
				// multibuild cmd/foo.go
				args.packagePath = filepath.Dir(t)
				args.output = strings.TrimSuffix(filepath.Base(t), ".go")
				args.sources = append(args.sources, t)
			} else {
				// multibuild cmd/foo
				args.packagePath = t
				args.output = filepath.Base(t)
			}
		}
	}

	return args, nil
}

func main() {
	args, err := buildArgs()
	if err != nil {
		fatal(err.Error())
	}

	if args.displayUsage {
		displayUsageAndExit(args.self)
	}

	doMultibuild(args)
}
