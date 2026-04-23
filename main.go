package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func extractDeps(content string) []string {
	var deps []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//DEPS") {
			dep := strings.TrimSpace(strings.TrimPrefix(line, "//DEPS"))
			deps = append(deps, dep)
		}
	}
	return deps
}

func downloadDep(dep string) (string, error) {
	parts := strings.Split(dep, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid dependency format: %s", dep)
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version := parts[2]
	url := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s/%s-%s.jar",
		group, artifact, version, artifact, version)
	jarName := fmt.Sprintf("%s-%s.jar", artifact, version)
	cacheDir := ".jolt-cache"
	os.MkdirAll(cacheDir, 0755)
	jarPath := cacheDir + "/" + jarName
	if _, err := os.Stat(jarPath); err == nil {
		fmt.Println("✅ Cached:", jarName)
		return jarPath, nil
	}
	fmt.Println("⬇️  Downloading:", jarName)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, err := os.Create(jarPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	io.Copy(out, resp.Body)
	fmt.Println("✅ Downloaded:", jarName)
	return jarPath, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: jolt <file.java>")
		os.Exit(1)
	}

	file := os.Args[1]

	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Println("❌ Error reading file:", err)
		os.Exit(1)
	}

	deps := extractDeps(string(content))
	var jarPaths []string

	if len(deps) == 0 {
		fmt.Println("📦 No dependencies found.")
	} else {
		fmt.Println("📦 Resolving dependencies...")
		for _, dep := range deps {
			jarPath, err := downloadDep(dep)
			if err != nil {
				fmt.Println("❌ Failed:", dep, err)
				os.Exit(1)
			}
			jarPaths = append(jarPaths, jarPath)
		}
	}

	// Build classpath
	classpath := strings.Join(jarPaths, ";")

	// Create temp output dir for .class files
	tmpDir, err := os.MkdirTemp("", "jolt-*")
	if err != nil {
		fmt.Println("❌ Could not create temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir) // auto cleanup after run

	// Compile into temp dir
	fmt.Println("🔨 Compiling", file, "...")
	compileArgs := []string{"-d", tmpDir, file}
	if classpath != "" {
		compileArgs = append([]string{"-cp", classpath}, compileArgs...)
	}

	compileCmd := exec.Command("javac", compileArgs...)
	compileCmd.Stdout = os.Stdout
	compileCmd.Stderr = os.Stderr
	if err := compileCmd.Run(); err != nil {
		fmt.Println("❌ Compilation failed.")
		os.Exit(1)
	}
	fmt.Println("✅ Compiled successfully!")

	// Get class name from filename (strip path, just the name)
	className := strings.TrimSuffix(filepath.Base(file), ".java")

	// Run
	fmt.Println("🚀 Running", className, "...")
	fmt.Println("-----------------------------------")

	runArgs := []string{className}
	if classpath != "" {
		runArgs = append([]string{"-cp", tmpDir + ";" + classpath}, runArgs...)
	} else {
		runArgs = append([]string{"-cp", tmpDir}, runArgs...)
	}

	runCmd := exec.Command("java", runArgs...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Println("❌ Run failed.")
		os.Exit(1)
	}
}