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
		name   string
		config string
		expect []string
	}

	var tests = []buildTest{
		{
			name: "include",
			config: `//go:multibuild:include=linux/amd64,linux/arm64
`,
			expect: []string{
				"${TARGET}-linux-amd64",
				"${TARGET}-linux-arm64",
			},
		},
		{
			name: "include+exclude",
			config: `//go:multibuild:include=*/arm64
//go:multibuild:exclude=android/arm64,darwin/arm64,freebsd/arm64,ios/arm64,netbsd/arm64,openbsd/arm64,windows/arm64
`,
			expect: []string{
				"${TARGET}-linux-arm64",
			},
		},
		{
			name: "output=",
			config: `//go:multibuild:include=linux/amd64,linux/arm64
//go:multibuild:output=bin/${TARGET}-hello-${GOOS}-world-${GOARCH}
`,
			expect: []string{
				filepath.Join("bin", "${TARGET}-hello-linux-world-amd64"),
				filepath.Join("bin", "${TARGET}-hello-linux-world-arm64"),
			},
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

			cmd := exec.Command(bin)
			cmd.Dir = testTmp
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("multibuild failed: %v\nOutput:\n%s", err, out)
			}

			for _, want := range test.expect {
				want := strings.ReplaceAll(want, "${TARGET}", filepath.Base(testTmp))
				if _, err := os.Stat(filepath.Join(testTmp, want)); err != nil {
					t.Errorf("expected binary %q not found", want)
				}
			}
		})
	}
}
