// Copyright 2025 Robin Burchell. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// debug logging
const dlog = false

// A filter specification, e.g. windows/arm64, windows/*, */arm64
// This is supposed to match against targets.
// NOTE: wildcarding must be the entire os or arch, partial matching is not supported.
type filter string

// goos/goarch string
type target string

// e.g. ${TARGET}_${GOOS}_${GOARCH}
type outputTemplate string

// All options for multibuild go here..
type options struct {
	// Output format
	Output outputTemplate

	// Targets to include
	Include []filter

	// Targets to exclude
	Exclude []filter
}

// Take targets, only allow 'Include', and then drop 'Exclude'.
func (this options) buildTargetList(targets []target) ([]target, error) {
	// Drop any matches that aren't included
	targets = filterSlice(targets, func(target target) bool {
		for _, filter := range this.Include {
			if filter.matches(target) {
				return true
			}
		}
		return false
	})

	// If exclude specified: We should remove matches from 'targets'
	targets = filterSlice(targets, func(target target) bool {
		for _, filter := range this.Exclude {
			if filter.matches(target) {
				return false
			}
		}
		return true
	})

	if dlog {
		log.Printf("Includes:")
		for _, filt := range this.Include {
			log.Printf("- %s", filt)
		}
		log.Printf("Excludes:")
		for _, filt := range this.Exclude {
			log.Printf("- %s", filt)
		}
		log.Printf("Decided targets:")
		for _, filt := range targets {
			log.Printf("- %s", filt)
		}
	}

	// Check includes still present
	for _, inc := range this.Include {
		found := false
		for _, t := range targets {
			if inc.matches(t) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("multibuild: required target %q was not found, or was excluded", inc)
		}
	}

	return targets, nil
}

// Returns true if this filter matches target.
func (this filter) matches(target target) bool {
	parts := strings.SplitN(string(this), "/", 2)
	if len(parts) != 2 {
		return string(target) == string(this)
	}
	filterOS, filterArch := parts[0], parts[1]
	targetParts := strings.SplitN(string(target), "/", 2)
	if len(targetParts) != 2 {
		return false
	}
	targetOS, targetArch := targetParts[0], targetParts[1]
	matchOS := filterOS == "*" || filterOS == targetOS
	matchArch := filterArch == "*" || filterArch == targetArch
	return matchOS && matchArch
}

// Validates that the 's' is a template, and builds a template from it.
func validateTemplate(s string) (outputTemplate, error) {
	if s == "" {
		return "", fmt.Errorf("empty string is not a valid template")
	}

	isAllowedPathChar := func(c byte) bool {
		switch {
		case c >= 'a' && c <= 'z':
			return true
		case c >= 'A' && c <= 'Z':
			return true
		case c >= '0' && c <= '9':
			return true
		case c == '_' || c == '-' || c == '/' || c == '.':
			return true
		default:
			return false
		}
	}

	isAllowedPlaceholderChar := func(c byte) bool {
		return (c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9')
	}

	found := make(map[string]struct{})

	var allowedPlaceholders = map[string]struct{}{
		"GOOS":   {},
		"GOARCH": {},
		"TARGET": {},
	}

	for i := 0; i < len(s); {
		c := s[i]

		switch {
		case isAllowedPathChar(c):
			i++

		// Placeholder start: ${...}
		case c == '$':
			if i+1 >= len(s) || s[i+1] != '{' {
				return "", fmt.Errorf("at %d: expected {, got %c", i+1, s[i+1])
			}
			j := i + 2 // start of ...

			for j < len(s) && s[j] != '}' {
				if !isAllowedPlaceholderChar(s[j]) {
					return "", fmt.Errorf("at %d: bad placeholder char %c", j, s[j])
				}
				j++
			}

			if j >= len(s) || s[j] != '}' {
				return "", fmt.Errorf("at %d: expected }, got %c", j, s[j])
			}

			name := s[i+2 : j]
			if _, ok := allowedPlaceholders[name]; !ok {
				return "", fmt.Errorf("at %d: unexpected placeholder %s", i, name)
			}

			found[name] = struct{}{}
			i = j + 1

		default:
			return "", fmt.Errorf("at %d: unexpected character: %c", i, s[i])
		}
	}

	// Ensure all required placeholders were found
	for name := range allowedPlaceholders {
		if _, ok := found[name]; !ok {
			return "", fmt.Errorf("placeholder %s was not found", name)
		}
	}

	return outputTemplate(s), nil
}

// Reads from 'io' on behalf of a path, and returns parsed options.
func scanBuildPath(reader io.Reader, path string) (options, error) {
	var opts options
	scanner := bufio.NewScanner(reader)
	i := 0
	for scanner.Scan() {
		i += 1
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:multibuild:") {
			continue
		}
		if strings.HasPrefix(line, "//go:multibuild:output=") {
			if dlog {
				log.Printf("Found output: %s:%d: %s", path, i, line)
			}
			rest := strings.TrimPrefix(line, "//go:multibuild:output=")
			if len(opts.Output) > 0 {
				return options{}, fmt.Errorf("%s:%d: go:multibuild:output was already set to %s, found: %q here", path, i, opts.Output, rest)
			}
			parsed, err := validateTemplate(rest)
			if err != nil {
				return options{}, fmt.Errorf("%s:%d: go:multibuild:output=%s is invalid: %s", path, i, rest, err)
			}
			opts.Output = parsed
		} else if strings.HasPrefix(line, "//go:multibuild:include=") {
			if dlog {
				log.Printf("Found include: %s:%d: %s", path, i, line)
			}
			rest := strings.Split(strings.TrimPrefix(line, "//go:multibuild:include="), ",")
			for _, v := range rest {
				opts.Include = append(opts.Include, filter(v))
			}
		} else if strings.HasPrefix(line, "//go:multibuild:exclude=") {
			if dlog {
				log.Printf("Found exclude: %s:%d: %s", path, i, line)
			}
			rest := strings.Split(strings.TrimPrefix(line, "//go:multibuild:exclude="), ",")
			for _, v := range rest {
				opts.Exclude = append(opts.Exclude, filter(v))
			}
		} else {
			return options{}, fmt.Errorf("%s:%d: bad go:multibuild instruction: %q", path, i, line)
		}
	}

	return opts, nil
}

// Scan all provided sources, and build options from them.
func scanBuildDir(sources []string) (options, error) {
	var opts options
	for _, path := range sources {
		f, err := os.Open(path)
		if err != nil {
			return options{}, fmt.Errorf("open: %s: %w", path, err)
		}
		defer f.Close()
		topts, err := scanBuildPath(f, path)
		if err != nil {
			return options{}, err
		}
		// TODO: Test we cover this case properly
		if len(opts.Output) > 0 && len(topts.Output) > 0 {
			return options{}, fmt.Errorf("%s: output= already set elsewhere", path)
		} else if len(topts.Output) > 0 {
			opts.Output = topts.Output
		}
		opts.Exclude = append(opts.Exclude, topts.Exclude...)
		opts.Include = append(opts.Include, topts.Include...)
	}

	// By default, we include everything.
	if len(opts.Include) == 0 {
		opts.Include = []filter{"*/*"}
	}

	// These require CGO_ENABLED=1, which I don't want to touch right now.
	// As I don't have a use for it, let's just disable them.
	opts.Exclude = append(opts.Exclude, "android/*", "ios/*")

	if len(opts.Output) == 0 {
		opts.Output = "${TARGET}-${GOOS}-${GOARCH}"
	}
	return opts, nil
}
