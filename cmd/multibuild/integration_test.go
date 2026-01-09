package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	binTmp := t.TempDir()
	bin := filepath.Join(binTmp, "multibuild")

	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	expected := fmt.Sprintf(`usage: %s [-o output] [build flags] [packages]
multibuild is a thin wrapper around 'go build'.
For documentation on multibuild's configuration, see https://github.com/rburchell/multibuild
Otherwise, run 'go help build' for command line flags.

multibuild-specific options:
    -v: enable verbose logs during building. this will also imply %s
    --multibuild-configuration: display the multibuild configuration parsed from the package
    --multibuild-targets: list targets that will be built
`, filepath.Base(bin), "`go build -v`" /* silly workaround for `s in a raw string literal */)

	for _, test := range []string{"-h", "--help"} {
		t.Run(test, func(t *testing.T) {
			cmd = exec.Command(bin, test)
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("multibuild failed: %v\nOutput:\n%s", err, out)
			}

			if string(out) != expected {
				t.Log("Expected:")
				for _, line := range strings.Split(expected, "\n") {
					t.Log(line)
				}
				t.Log("Got")
				for _, line := range strings.Split(string(out), "\n") {
					t.Log(line)
				}
				t.Fatalf("output mismatch")
			}
		})
	}
}

func TestMultibuild(t *testing.T) {
	binTmp := t.TempDir()
	bin := filepath.Join(binTmp, "multibuild")

	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	gover := runtime.Version() // "go1.24..."
	if gover[0:2] != "go" {    // check for, and skip the "go" prefix
		t.Fatalf("unexpected go version: %s", gover)
	}
	gover = gover[2:]

	baseModSource := fmt.Sprintf(`module main

go %s`, gover)

	const baseTestSource = `package main

import "fmt"

func main() {
        fmt.Println("Hello world")
}
`

	type buildTest struct {
		name             string
		config           string
		expectedBinaries []string
		expectedConfig   string
		expectedTargets  string
	}

	var tests = []buildTest{
		{
			name: "include",
			config: `//go:multibuild:include=linux/amd64,linux/arm64
`,
			expectedBinaries: []string{
				"${TARGET}-linux-amd64",
				"${TARGET}-linux-arm64",
			},
			expectedConfig: `//go:multibuild:include=linux/amd64,linux/arm64
//go:multibuild:exclude=android/*,ios/*
//go:multibuild:output=${TARGET}-${GOOS}-${GOARCH}
`,
			expectedTargets: "linux/amd64\nlinux/arm64\n",
		},
		{
			name: "include+exclude",
			config: `//go:multibuild:include=*/arm64
//go:multibuild:exclude=android/arm64,darwin/arm64,freebsd/arm64,ios/arm64,netbsd/arm64,openbsd/arm64,windows/arm64
`,
			expectedBinaries: []string{
				"${TARGET}-linux-arm64",
			},
			expectedConfig: `//go:multibuild:include=*/arm64
//go:multibuild:exclude=android/arm64,darwin/arm64,freebsd/arm64,ios/arm64,netbsd/arm64,openbsd/arm64,windows/arm64,android/*,ios/*
//go:multibuild:output=${TARGET}-${GOOS}-${GOARCH}
`,
			expectedTargets: "linux/arm64\n",
		},
		{
			name: "output=",
			config: `//go:multibuild:include=linux/amd64,linux/arm64
//go:multibuild:output=bin/${TARGET}-hello-${GOOS}-world-${GOARCH}
`,
			expectedBinaries: []string{
				filepath.Join("bin", "${TARGET}-hello-linux-world-amd64"),
				filepath.Join("bin", "${TARGET}-hello-linux-world-arm64"),
			},
			expectedConfig: `//go:multibuild:include=linux/amd64,linux/arm64
//go:multibuild:exclude=android/*,ios/*
//go:multibuild:output=bin/${TARGET}-hello-${GOOS}-world-${GOARCH}
`,
			expectedTargets: "linux/amd64\nlinux/arm64\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testTmp := t.TempDir()

			src := test.config + baseTestSource

			mainPath := filepath.Join(testTmp, "main.go")
			if err := os.WriteFile(mainPath, []byte(src), 0644); err != nil {
				t.Fatalf("failed to write %s: %v", mainPath, err)
			}

			modPath := filepath.Join(testTmp, "go.mod")
			if err := os.WriteFile(modPath, []byte(baseModSource), 0644); err != nil {
				t.Fatalf("failed to write %s: %v", baseModSource, err)
			}

			cmd := exec.Command(bin, "--multibuild-configuration")
			cmd.Dir = testTmp
			gotConfig, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("failed to read configuration: %v\nOutput:\n%s", err, gotConfig)
			}
			if string(gotConfig) != test.expectedConfig {
				t.Fatalf("configuration mismatch:\ngot: %s\nwanted: %s\n", gotConfig, test.expectedConfig)
			}

			cmd = exec.Command(bin, "--multibuild-targets")
			cmd.Dir = testTmp
			gotTargets, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("failed to read targets: %v\nOutput:\n%s", err, gotTargets)
			}
			if string(gotTargets) != test.expectedTargets {
				t.Fatalf("targets mismatch:\ngot: %s\nwanted: %s\n", gotTargets, test.expectedTargets)
			}

			cmd = exec.Command(bin)
			cmd.Dir = testTmp

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("failed to multibuild: %v\nOutput:\n%s", err, out)
			}

			for _, want := range test.expectedBinaries {
				want := strings.ReplaceAll(want, "${TARGET}", filepath.Base(testTmp))
				if _, err := os.Stat(filepath.Join(testTmp, want)); err != nil {
					t.Errorf("expected binary %q not found", want)
				}
			}
		})
	}
}
