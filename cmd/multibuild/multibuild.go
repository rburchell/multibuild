// Copyright 2025 Robin Burchell. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A simplistic tool to build Go binaries for multiple platforms.
package main

//go:multibuild:output=bin/${TARGET}-${GOOS}-${GOARCH}

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Discovers all source files for this package.
// This is smarter than Walk() looking for *.go, because it will obey build constraints.
func sourcesList() ([]string, error) {
	cmd := exec.Command("go", "list", "-compiled", "-json=CompiledGoFiles")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	var v struct {
		CompiledGoFiles []string `json:"CompiledGoFiles"`
	}
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return v.CompiledGoFiles, nil
}

// Returns a list of targets that can be built.
func targetList() ([]target, error) {
	cmd := exec.Command("go", "tool", "dist", "list")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	return mapSlice(lines, func(str string) target {
		return target(str)
	}), nil
}

// Returns the binary name/path that `go build` would produce.
func determineTargetName(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "-o" && i+1 < len(args) {
			return args[i+1], nil
		}

		if strings.HasPrefix(arg, "-o=") {
			return strings.TrimPrefix(arg, "-o="), nil
		}
	}

	var nonflags []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			nonflags = append(nonflags, arg)
		}
	}

	if len(nonflags) == 1 {
		target := nonflags[0]

		if strings.HasSuffix(target, ".go") {
			return strings.TrimSuffix(filepath.Base(target), ".go"), nil
		}

		return filepath.Base(target), nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Base(wd), nil
}

func displayUsageAndExit(self string) {
	fmt.Println(fmt.Sprintf("usage: %s [-o output] [build flags] [packages]", self))
	fmt.Println("multibuild is a thin wrapper around 'go build'.")
	fmt.Println("For documentation on multibuild's configuration, see https://github.com/rburchell/multibuild")
	fmt.Println("Otherwise, run 'go help build' for command line flags.")
	fmt.Println("")
	fmt.Println("multibuild-specific options:")
	fmt.Println("    --multibuild-configuration: display the multibuild configuration parsed from the package")
	fmt.Println("    --multibuild-targets: list targets that will be built")
	os.Exit(0)
}

func displayConfigAndExit(opts options) {
	fmt.Printf("//go:multibuild:include=%s\n", strings.Join(mapSlice(opts.Include, func(f filter) string { return string(f) }), ","))
	fmt.Printf("//go:multibuild:exclude=%s\n", strings.Join(mapSlice(opts.Exclude, func(f filter) string { return string(f) }), ","))
	fmt.Printf("//go:multibuild:output=%s\n", opts.Output)
	os.Exit(0)
}

func displayTargetsAndExit(targets []target) {
	for _, target := range targets {
		fmt.Println(target)
	}
	os.Exit(0)
}

func main() {
	self := filepath.Base(os.Args[0])
	args := os.Args[1:]
	displayConfig := false
	displayTargets := false

	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help":
			displayUsageAndExit(self)
		case arg == "--multibuild-configuration":
			displayConfig = true
		case arg == "--multibuild-targets":
			displayTargets = true
		case strings.HasPrefix(arg, "--multibuild"):
			log.Fatalf("multibuild: unrecognized argument %q", arg)
		}
	}

	output, err := determineTargetName(args)
	if err != nil {
		log.Fatalf("multibuild: failed to get target name: %s", err)
	}

	sources, err := sourcesList()
	if err != nil {
		log.Fatalf("multibuild: failed to discover sources: %s", err)
	}
	opts, err := scanBuildDir(sources)
	if err != nil {
		log.Fatalf("multibuild: failed to scan sources: %s", err)
	}

	targets, err := targetList()
	if err != nil {
		log.Fatalf("multibuild: failed to list targets: %s", err)
	}
	targets, err = opts.buildTargetList(targets)
	if err != nil {
		log.Fatalf("multibuild: failed to build target list: %s", err)
	}

	if displayConfig {
		displayConfigAndExit(opts)
	}
	if displayTargets {
		displayTargetsAndExit(targets)
	}

	// If there's an explicit GOOS/GOARCH, pass through.
	// We want to stay out of the way here.
	// TODO: But this might be a confusing mistake to fall over if you set it in .bashrc etc..
	if os.Getenv("GOOS") != "" || os.Getenv("GOARCH") != "" {
		runBuild(args, "", "")
		return
	}

	wg := sync.WaitGroup{}
	formattedOutput := string(opts.Output)
	formattedOutput = strings.ReplaceAll(formattedOutput, "${TARGET}", output)

	for _, t := range targets {
		parts := strings.Split(string(t), "/")
		goos, goarch := parts[0], parts[1]

		out := formattedOutput
		out = strings.ReplaceAll(out, "${GOOS}", goos)
		out = strings.ReplaceAll(out, "${GOARCH}", goarch)

		if goos == "windows" {
			out += ".exe"
		}

		buildArgs := slices.Clone(args)
		buildArgs = append(buildArgs, "-o", out)

		wg.Add(1)
		go func(goos, goarch string, buildArgs []string) {
			runBuild(buildArgs, goos, goarch)
			wg.Done()
		}(goos, goarch, buildArgs)
	}

	wg.Wait()
}

func runBuild(args []string, goos, goarch string) {
	cmd := exec.Command("go", append([]string{"build"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if goos != "" {
		cmd.Env = append(cmd.Env,
			"GOOS="+goos,
			"GOARCH="+goarch,
		)

		// multibuild is primarily a tool for cross compilation:
		// making a binary in one place, that will run in many other places.
		//
		// Building binaries that have libc dependencies by default (if you use e.g. 'net')
		// is suboptimal for this case, at best, given the binary won't be as portable:
		// On Linux, a libc dependency will often render a binary built on one machine
		// unusable on another machine due to glibc version differences, for example.
		//
		// Also, if your environment has a broken toolchain of some kind
		// (and thus, cgo won't work at all), see for example #2, this leads to a large
		// amount of unhelpful confusion.
		//
		// So, my executive decision is that we'll turn CGO_ENABLED off unless you explicitly turn it on.
		_, hasCgo := os.LookupEnv("CGO_ENABLED")
		if !hasCgo {
			cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
		}
	}

	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
