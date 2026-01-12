// Copyright 2025 Robin Burchell. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestFilterMatches(t *testing.T) {
	tests := []struct {
		filter filter
		target string
		want   bool
	}{
		// Exact match
		{"windows/arm64", "windows/arm64", true},
		{"linux/amd64", "linux/amd64", true},

		// Partial match
		{"linux/amd64", "windows/amd64", false},
		{"linux/amd64", "linux/arm64", false},

		// Wildcard arch
		{"windows/*", "windows/arm64", true},
		{"windows/*", "windows/amd64", true},
		{"windows/*", "linux/amd64", false},

		// Wildcard os
		{"*/arm64", "windows/arm64", true},
		{"*/arm64", "linux/arm64", true},
		{"*/arm64", "linux/amd64", false},

		// Full wildcard
		{"*/*", "windows/amd64", true},
		{"*/*", "linux/arm64", true},
	}

	for _, tt := range tests {
		got := tt.filter.matches(target(tt.target))
		if got != tt.want {
			t.Errorf("filter(%q).matches(%q) = %v; want %v", tt.filter, tt.target, got, tt.want)
		}
	}
}

func TestBuildTargetList(t *testing.T) {
	allTargets := []target{
		"windows/amd64",
		"windows/arm64",
		"linux/amd64",
		"linux/arm64",
	}

	tests := []struct {
		name    string
		options options
		want    []target
		wantErr bool
	}{
		{
			name:    "Include windows/arm64 only",
			options: options{Include: []filter{"windows/arm64"}},
			want:    []target{"windows/arm64"},
			wantErr: false,
		},
		{
			name:    "Include all arm64",
			options: options{Include: []filter{"*/arm64"}},
			want:    []target{"windows/arm64", "linux/arm64"},
			wantErr: false,
		},
		{
			name:    "Include windows/*, exclude windows/arm64",
			options: options{Include: []filter{"windows/*"}, Exclude: []filter{"windows/arm64"}},
			want:    []target{"windows/amd64"},
			wantErr: false,
		},
		{
			name:    "Exclude all windows",
			options: options{Include: []filter{"*/*"}, Exclude: []filter{"windows/*"}},
			want:    []target{"linux/amd64", "linux/arm64"},
			wantErr: false,
		},
		{
			name:    "Required include missing",
			options: options{Include: []filter{"darwin/amd64"}}, // not in allTargets
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.options.buildTargetList(allTargets)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("want/got mismatch: %+v vs %+v", got, tt.want)
			}
			for idx := range got {
				got := got[idx]
				want := tt.want[idx]
				if got != want {
					t.Errorf("mismatch at %d; got %v, want %v", idx, got, tt.want)
				}
			}
		})
	}
}

func TestScanBuildPath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      options
		wantError bool
	}{
		{
			name:      "empty input",
			input:     "",
			want:      options{},
			wantError: false,
		},
		{
			name:  "include only",
			input: `//go:multibuild:include=windows/arm64,linux/*`,
			want: options{
				Include: []filter{"windows/arm64", "linux/*"},
			},
			wantError: false,
		},
		{
			name:  "exclude only",
			input: `//go:multibuild:exclude=darwin/amd64,*/arm64`,
			want: options{
				Exclude: []filter{"darwin/amd64", "*/arm64"},
			},
			wantError: false,
		},
		{
			name: "both include and exclude",
			input: `
				//go:multibuild:include=windows/arm64
				//go:multibuild:exclude=darwin/amd64
				`,
			want: options{
				Include: []filter{"windows/arm64"},
				Exclude: []filter{"darwin/amd64"},
			},
			wantError: false,
		},
		{
			name: "ignores unrelated go: comments",
			input: `//go:generate something
//go:otherkey:value
//go:multibuild:include=windows/amd64`,
			want: options{
				Include: []filter{"windows/amd64"},
			},
			wantError: false,
		},
		{
			name:      "invalid instruction",
			input:     `//go:multibuild:badtag=foobar`,
			want:      options{},
			wantError: true,
		},
		{
			name:      "invalid filter",
			input:     `//go:multibuild:include=linux/amd*`,
			want:      options{},
			wantError: true,
		},
	}

	equalOptions := func(a, b options) bool {
		if len(a.Include) != len(b.Include) || len(a.Exclude) != len(b.Exclude) {
			return false
		}
		for i := range a.Include {
			if a.Include[i] != b.Include[i] {
				return false
			}
		}
		for i := range a.Exclude {
			if a.Exclude[i] != b.Exclude[i] {
				return false
			}
		}
		return true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			got, err := scanBuildPath(r, "fake.go")
			if (err != nil) != tt.wantError {
				t.Fatalf("scanBuildPath() error = %v, wantErr %v", err, tt.wantError)
			}
			if !tt.wantError && !equalOptions(got, tt.want) {
				t.Errorf("scanBuildPath() got = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func makeTempFile(t *testing.T, contents string) string {
	tmp, err := os.CreateTemp("", "multibuildtest")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tmp.WriteString(contents)
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	return tmp.Name()
}

func TestScanBuildDir_BasicInclude(t *testing.T) {
	file := makeTempFile(t, `//go:multibuild:include=windows/amd64,linux/*`)
	defer os.Remove(file)

	opts, err := scanBuildDir([]string{file})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantIncludes := []filter{"windows/amd64", "linux/*"}
	if len(opts.Include) != 2 || opts.Include[0] != wantIncludes[0] || opts.Include[1] != wantIncludes[1] {
		t.Errorf("got includes %v, want %v", opts.Include, wantIncludes)
	}
}

func TestScanBuildDir_MergeMultipleFiles(t *testing.T) {
	f1 := makeTempFile(t, `//go:multibuild:include=windows/*`)
	defer os.Remove(f1)
	f2 := makeTempFile(t, `//go:multibuild:include=darwin/*`)
	defer os.Remove(f2)

	opts, err := scanBuildDir([]string{f1, f2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantIncludes := []filter{"windows/*", "darwin/*"}
	if len(opts.Include) != 2 || opts.Include[0] != wantIncludes[0] || opts.Include[1] != wantIncludes[1] {
		t.Errorf("got includes %v, want %v", opts.Include, wantIncludes)
	}
}

func TestScanBuildDir_ExcludeDefaultCGO(t *testing.T) {
	file := makeTempFile(t, "")
	defer os.Remove(file)

	// Unset CGO_ENABLED
	os.Setenv("CGO_ENABLED", "0")
	opts, _ := scanBuildDir([]string{file})
	found := slices.Contains(opts.Exclude, "android/*")
	if !found {
		t.Errorf("expected android/* to be excluded when CGO_ENABLED=0, got excludes %v", opts.Exclude)
	}
}

func TestScanBuildDir_EmptyIncludeGetsAll(t *testing.T) {
	file := makeTempFile(t, "")
	defer os.Remove(file)

	opts, _ := scanBuildDir([]string{file})
	if len(opts.Include) != 1 || opts.Include[0] != "*/*" {
		t.Errorf("expected default include of */*, got %v", opts.Include)
	}
}

func TestScanBuildDir_FileOpenError(t *testing.T) {
	_, err := scanBuildDir([]string{"/not/exist"})
	if err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("expected open failure, got %v", err)
	}
}

func TestScanBuildDir_BadDirective(t *testing.T) {
	file := makeTempFile(t, "//go:multibuild:oops=foo")
	defer os.Remove(file)

	_, err := scanBuildPath(strings.NewReader("//go:multibuild:oops=foo\n"), "path.go")
	if err == nil {
		t.Errorf("expected error on bad directive")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// --- valid ---
		{
			name:    "simple valid path",
			input:   "bin/${GOOS}/${GOARCH}/${TARGET}",
			wantErr: false,
		},
		{
			name:    "dots dashes and underscores",
			input:   "./out/${GOOS}-${GOARCH}_${TARGET}.bin",
			wantErr: false,
		},
		{
			name:    "nested directories",
			input:   "build/${GOOS}/${GOARCH}/v1/${TARGET}",
			wantErr: false,
		},

		// --- missing placeholders ---
		{
			name:    "missing GOOS",
			input:   "bin/${GOARCH}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "missing GOARCH",
			input:   "bin/${GOOS}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "missing TARGET",
			input:   "bin/${GOOS}/${GOARCH}",
			wantErr: true,
		},

		// --- unknown placeholders ---
		{
			name:    "unknown placeholder",
			input:   "bin/${GOOS}/${ARCH}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "lowercase placeholder",
			input:   "bin/${goos}/${GOARCH}/${TARGET}",
			wantErr: true,
		},

		// --- stray or malformed $ ---
		{
			name:    "stray dollar",
			input:   "bin/$/${GOARCH}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "dollar without braces",
			input:   "bin/$GOOS/${GOARCH}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "unterminated placeholder",
			input:   "bin/${GOOS/${GOARCH}/${TARGET}",
			wantErr: true,
		},
		{
			name:    "empty placeholder",
			input:   "bin/${}/${GOARCH}/${TARGET}",
			wantErr: true,
		},

		// --- invalid characters ---
		{
			name:    "space not allowed",
			input:   "bin/${GOOS}/${GOARCH}/${TARGET} debug",
			wantErr: true,
		},
		{
			name:    "exclamation mark",
			input:   "bin/${GOOS}/${GOARCH}/${TARGET}!",
			wantErr: true,
		},

		// --- path edge cases ---
		{
			name:    "dot only",
			input:   ".",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "nul byte",
			input:   "bin/\x00/${GOOS}/${GOARCH}/${TARGET}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := validateTemplate(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (output=%q)", out)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Successful result must preserve input
			if string(out) != tt.input {
				t.Fatalf("output mismatch: got %q, want %q", out, tt.input)
			}
		})
	}
}

func TestValidateFilters_Valid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []filter
	}{
		{
			name: "single entry",
			in:   "linux/amd64",
			want: []filter{filter("linux/amd64")},
		},
		{
			name: "multiple entries",
			in:   "linux/amd64,darwin/arm64",
			want: []filter{
				filter("linux/amd64"),
				filter("darwin/arm64"),
			},
		},
		{
			name: "wildcard os",
			in:   "*/amd64",
			want: []filter{filter("*/amd64")},
		},
		{
			name: "wildcard arch",
			in:   "linux/*",
			want: []filter{filter("linux/*")},
		},
		{
			name: "both wildcards",
			in:   "*/*",
			want: []filter{filter("*/*")},
		},
		{
			name: "mixed wildcards",
			in:   "linux/amd64,*/arm64",
			want: []filter{
				filter("linux/amd64"),
				filter("*/arm64"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateFilterString(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %d want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("entry %d: got %+v want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidateFilters_Invalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"missing slash", "linuxamd64"},
		{"missing os", "/amd64"},
		{"missing arch", "linux/"},
		{"double slash", "linux//amd64"},
		{"leading comma", ",linux/amd64"},
		{"trailing comma", "linux/amd64,"},
		{"double comma", "linux/amd64,,darwin/arm64"},
		{"unexpected char", "linux/amd$64"},
		{"wildcard partial os", "*nix/amd64"},
		{"wildcard partial arch", "linux/amd*"},
		{"wildcard mixed os", "l*/amd64"},
		{"wildcard mixed arch", "linux/*64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateFilterString(tt.in)
			if err == nil {
				t.Fatalf("expected error for %q", tt.in)
			}
		})
	}
}
