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

func TestMultibuildWithConfiguration(t *testing.T) {
	binTmp := t.TempDir()
	bin := filepath.Join(binTmp, "multibuild")

	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

func TestMultibuildDifferentStyles(t *testing.T) {
	type testCase struct {
		name              string
		numPackages       int
		numBinariesPerPkg int
		runDir            string
		args              []string
		expectErr         bool
		expectedBinaries  []string
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// TODO: A little too much magic generation in this test, but unsure how else to structure it.
	// TODO: We presently only test building inside a single module. That's probably OK, or do we need to test more?
	// TODO: We don't have tests to cover multiple source files that aren't binaries, and we should.
	testCases := []testCase{
		{
			// tests "multibuild" with no arguments should produce binaries
			name:              "build in source dir",
			numPackages:       1,
			numBinariesPerPkg: 1,
			runDir:            "pkg1",
			args:              []string{},
			expectErr:         false,
			expectedBinaries: []string{
				fmt.Sprintf("pkg1-%s-%s", goos, goarch),
			},
		},
		{
			// tests "multibuild pkg/" should produce binaries
			name:              "build via path/",
			numPackages:       1,
			numBinariesPerPkg: 1,
			runDir:            ".",
			args:              []string{"./pkg1"},
			expectErr:         false,
			expectedBinaries: []string{
				fmt.Sprintf("pkg1-%s-%s", goos, goarch),
			},
		},
		{
			// tests "multibuild pkg/main1.go" should produce binaries
			name:              "build via single .go file",
			numPackages:       1,
			numBinariesPerPkg: 1,
			runDir:            ".",
			args:              []string{"pkg1/main1.go"},
			expectErr:         false,
			expectedBinaries: []string{
				fmt.Sprintf("pkg1-%s-%s", goos, goarch),
			},
		},
		{
			// tests that currently, building two binaries should fail
			name:              "build two binaries by file",
			numPackages:       1,
			numBinariesPerPkg: 2,
			runDir:            ".",
			args:              []string{"pkg1/main1.go", "pkg1/main2.go"},
			expectErr:         true,
			expectedBinaries:  []string{},
		},
		{
			// tests that currently, building two packages should fail
			name:              "build two packages by path/",
			numPackages:       2,
			numBinariesPerPkg: 1,
			runDir:            ".",
			args:              []string{"pkg1", "pkg2"},
			expectErr:         true,
			expectedBinaries:  []string{},
		},
	}

	tmpRoot := t.TempDir()
	bin := filepath.Join(tmpRoot, "multibuild")

	cmd := exec.Command("go", "build", "-o", bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup packages and binaries
			gover := runtime.Version() // "go1.24..."
			if gover[0:2] != "go" {    // check for, and skip the "go" prefix
				t.Fatalf("unexpected go version: %s", gover)
			}
			gover = gover[2:]
			baseMod := fmt.Sprintf("module %s\n\ngo %s\n", "testmod", gover)
			if err := os.WriteFile(filepath.Join(tmpRoot, "go.mod"), []byte(baseMod), 0644); err != nil {
				t.Fatalf("failed to write go.mod: %v", err)
			}

			for p := 1; p <= tc.numPackages; p++ {
				pkgDir := filepath.Join(tmpRoot, fmt.Sprintf("pkg%d", p))
				os.RemoveAll(pkgDir)

				if err := os.Mkdir(pkgDir, 0755); err != nil {
					t.Fatalf("failed to mkdir: %v", err)
				}
				for b := 1; b <= tc.numBinariesPerPkg; b++ {
					mainSource := fmt.Sprintf(`package main
import "fmt"
func main() { fmt.Println("Hello from main%d in pkg%d") }
`, b, p)

					mainPath := filepath.Join(pkgDir, fmt.Sprintf("main%d.go", b))
					if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
						t.Fatalf("failed to write %s: %v", mainPath, err)
					}
					// Add multibuild config to the first file in each package
					if b == 1 {
						config := `//go:multibuild:include=` + goos + `/` + goarch + "\n"
						config += "//go:multibuild:output=${TARGET}-${GOOS}-${GOARCH}\n"
						buf, err := os.ReadFile(mainPath)
						if err != nil {
							t.Fatalf("failed to read file to inject config")
						}
						if err := os.WriteFile(mainPath, []byte(config+string(buf)), 0644); err != nil {
							t.Fatalf("failed to write config: %v", err)
						}
					}
				}
			}

			var runDir string
			if tc.runDir == "." {
				runDir = tmpRoot
			} else {
				runDir = filepath.Join(tmpRoot, tc.runDir)
			}

			cmd := exec.Command(bin, tc.args...)
			cmd.Dir = runDir
			out, err := cmd.CombinedOutput()

			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got success:\nOutput:\n%s", string(out))
				}
			} else {
				if err != nil {
					t.Fatalf("expected success, got error: %s\nOutput:\n%s", err, string(out))
				}

				if err != nil {
					t.Fatalf("expected success, got error: %v\nOutput:\n%s", err, string(out))
				}
				for _, binRel := range tc.expectedBinaries {
					var binPath string
					if tc.runDir == "." {
						binPath = filepath.Join(tmpRoot, binRel)
					} else {
						binPath = filepath.Join(runDir, binRel)
					}
					if _, err := os.Stat(binPath); err != nil {
						t.Errorf("expected binary %q not found", binPath)
					}
				}
			}
		})
	}
}
