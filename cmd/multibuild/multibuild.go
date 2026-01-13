// Copyright 2025 Robin Burchell. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Discovers all source files for this package.
// This is smarter than Walk() looking for *.go, because it will obey build constraints.
func sourcesList(packagePath string) ([]string, error) {
	cmd := exec.Command("go", "list", "-compiled", "-json=CompiledGoFiles", packagePath)

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

	// We must prepend packagePath to each of the paths to scan, so that
	// we can actually find the paths in the case where we are building
	// a package from an unexpected location.
	for idx, p := range v.CompiledGoFiles {
		v.CompiledGoFiles[idx] = filepath.Join(packagePath, p)
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

func doMultibuild(args cliArgs) {
	sources := args.sources

	if len(sources) == 0 {
		var err error
		sources, err = sourcesList(args.packagePath)
		if err != nil {
			fatal("multibuild: failed to discover sources: %s", err)
		}
	}

	opts, err := scanBuildDir(sources)
	if err != nil {
		fatal("multibuild: failed to scan sources: %s", err)
	}

	targets, err := targetList()
	if err != nil {
		fatal("multibuild: failed to list targets: %s", err)
	}
	targets, err = opts.buildTargetList(targets)
	if err != nil {
		fatal("multibuild: failed to build target list: %s", err)
	}

	if args.displayConfig {
		displayConfigAndExit(opts)
	}
	if args.displayTargets {
		displayTargetsAndExit(targets)
	}

	// If there's an explicit GOOS/GOARCH, pass through.
	// We want to stay out of the way here.
	// TODO: But this might be a confusing mistake to fall over if you set it in .bashrc etc..
	if os.Getenv("GOOS") != "" || os.Getenv("GOARCH") != "" {
		runBuild(args.goBuildArgs, "", "")
		return
	}

	wg := sync.WaitGroup{}
	sem := make(chan struct{}, 4) // limit max parallel builds to save sanity...

	formattedOutput := string(opts.Output)
	formattedOutput = strings.ReplaceAll(formattedOutput, "${TARGET}", args.output)

	for _, t := range targets {
		parts := strings.Split(string(t), "/")
		goos, goarch := parts[0], parts[1]

		out := formattedOutput
		out = strings.ReplaceAll(out, "${GOOS}", goos)
		out = strings.ReplaceAll(out, "${GOARCH}", goarch)
		outBin := out

		if goos == "windows" {
			outBin += ".exe"
		}

		buildArgs := []string{"-o", outBin}
		buildArgs = append(buildArgs, args.goBuildArgs...)

		wg.Add(1) // acquire for global
		go func(out, outBin, goos, goarch string, buildArgs []string) {
			if args.verbose {
				fmt.Fprintf(os.Stderr, "%s/%s: waiting\n", goos, goarch)
			}
			sem <- struct{}{} // acquire for job
			if args.verbose {
				fmt.Fprintf(os.Stderr, "%s/%s: build\n", goos, goarch)
			}
			runBuild(buildArgs, goos, goarch)
			if args.verbose {
				fmt.Fprintf(os.Stderr, "%s/%s: archive\n", goos, goarch)
			}

			for _, format := range opts.Format {
				switch format {
				case formatRaw:
					// already built (obvs)..
				case formatZip:
					arPath := out + ".zip"
					f, err := os.Create(arPath)
					defer f.Close()
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to create archive %s: %s\n", goos, goarch, arPath, err)
						os.Exit(1)
					}

					zw := zip.NewWriter(f)
					defer zw.Close()

					w, err := zw.Create(outBin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to create header %s: %s\n", goos, goarch, arPath, err)
						os.Exit(1)
					}

					st, err := os.Stat(outBin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to stat raw %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					bin, err := os.Open(outBin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to open raw %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					defer bin.Close()
					sz, err := io.Copy(w, bin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to copy %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					if sz != st.Size() {
						fmt.Fprintf(os.Stderr, "%s/%s: size mismatch in copy of %s: (%d vs %d)\n", goos, goarch, outBin, sz, st.Size())
						os.Exit(1)
					}
				case formatTgz:
					arPath := out + ".tar.gz"
					f, err := os.Create(arPath)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to create archive %s: %s\n", goos, goarch, arPath, err)
						os.Exit(1)
					}
					defer f.Close()

					gz := gzip.NewWriter(f)
					defer gz.Close()

					tw := tar.NewWriter(gz)
					defer tw.Close()

					st, err := os.Stat(outBin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to stat raw %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					bin, err := os.Open(outBin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to open raw %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					defer bin.Close()

					hdr := &tar.Header{Name: outBin, Mode: 0755, Size: st.Size()}
					tw.WriteHeader(hdr)
					sz, err := io.Copy(tw, bin)
					if err != nil {
						fmt.Fprintf(os.Stderr, "%s/%s: failed to copy %s: %s\n", goos, goarch, outBin, err)
						os.Exit(1)
					}
					if sz != st.Size() {
						fmt.Fprintf(os.Stderr, "%s/%s: size mismatch in copy of %s: (%d vs %d)\n", goos, goarch, outBin, sz, st.Size())
						os.Exit(1)
					}
				}
			}

			// If the format list specifically excluded raw, remove the binary.
			// I don't know why one would want to do this, but nevertheless...
			if !slices.Contains(opts.Format, formatRaw) {
				err := os.Remove(outBin)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s/%s: failed to remove unwanted raw output %s: %s\n", goos, goarch, outBin, err)
				}
			}
			<-sem     // release for job
			wg.Done() // release for global
		}(out, outBin, goos, goarch, buildArgs)
	}

	wg.Wait()
}

func runBuild(args []string, goos, goarch string) {
	cmd := exec.Command("go", append([]string{"build"}, args...)...)
	cmd.Env = os.Environ()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	interceptor := func(source io.ReadCloser, dest io.Writer) {
		scanner := bufio.NewScanner(source)
		for scanner.Scan() {
			line := fmt.Sprintf("%s/%s: %s", goos, goarch, scanner.Text())
			fmt.Fprintln(dest, line)
		}
	}

	go interceptor(stdout, os.Stdout)
	go interceptor(stderr, os.Stderr)

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
