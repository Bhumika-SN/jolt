package main
import (
        "encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/schollz/progressbar/v3"
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

func extractJavaVersion(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//JAVA") {
			version := strings.TrimSpace(strings.TrimPrefix(line, "//JAVA"))
			version = strings.TrimSuffix(version, "+")
			return version
		}
	}
	return ""
}

func getInstalledJavaVersion() string {
	cmd := exec.Command("java", "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	output := string(out)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "version") {
			parts := strings.Split(line, "\"")
			if len(parts) >= 2 {
				version := parts[1]
				if strings.HasPrefix(version, "1.") {
					return strings.Split(version, ".")[1]
				}
				return strings.Split(version, ".")[0]
			}
		}
	}
	return ""
}

func checkJava() bool {
	cmd := exec.Command("java", "-version")
	return cmd.Run() == nil
}

func checkJavac() bool {
	cmd := exec.Command("javac", "-version")
	return cmd.Run() == nil
}

func downloadJDK() error {
	jdkDir := filepath.Join(os.Getenv("USERPROFILE"), ".jolt", "jdks", "21")
	if _, err := os.Stat(jdkDir); err == nil {
		fmt.Println("✅ JDK 21 already downloaded.")
		os.Setenv("JAVA_HOME", jdkDir)
		os.Setenv("PATH", jdkDir+`\bin;`+os.Getenv("PATH"))
		return nil
	}
	os.MkdirAll(jdkDir, 0755)
	url := "https://api.adoptium.net/v3/binary/latest/21/ga/windows/x64/jdk/hotspot/normal/eclipse?project=jdk"
	zipPath := filepath.Join(os.Getenv("USERPROFILE"), ".jolt", "jdk21.zip")
	fmt.Println("⬇️  Downloading JDK 21 from Adoptium...")
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer out.Close()
	io.Copy(out, resp.Body)
	fmt.Println("✅ JDK downloaded!")
	fmt.Println("💡 Please install Java manually from: https://adoptium.net")
	fmt.Println("   Then run jolt again.")
	os.Exit(0)
	return nil
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home dir: %w", err)
	}
	cacheDir := filepath.Join(homeDir, ".jolt", "cache")
	os.MkdirAll(cacheDir, 0755)
	jarPath := filepath.Join(cacheDir, jarName)
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
	bar := progressbar.DefaultBytes(resp.ContentLength, jarName)
	io.Copy(io.MultiWriter(out, bar), resp.Body)
	fmt.Println("\n✅ Downloaded:", jarName)
	return jarPath, nil
}
type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}

type pomProject struct {
	Dependencies []pomDependency `xml:"dependencies>dependency"`
}

func fetchPomDeps(group, artifact, version string) []pomDependency {
	groupPath := strings.ReplaceAll(group, ".", "/")
	url := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s/%s-%s.pom",
		groupPath, artifact, version, artifact, version)

	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var pom pomProject
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil
	}
	return pom.Dependencies
}

// resolveTransitive walks the dependency tree starting from the direct //DEPS
// entries and returns every "group:artifact:version" coordinate that needs
// to be downloaded, including transitive runtime dependencies.
func resolveTransitive(directDeps []string) []string {
	seen := make(map[string]bool)
	var resolved []string

	var visit func(dep string)
	visit = func(dep string) {
		parts := strings.Split(dep, ":")
		if len(parts) != 3 {
			return
		}
		group, artifact, version := parts[0], parts[1], parts[2]
		key := group + ":" + artifact
		if seen[key] {
			return
		}
		seen[key] = true
		resolved = append(resolved, dep)

		children := fetchPomDeps(group, artifact, version)
		for _, c := range children {
			if c.Scope == "test" || c.Scope == "provided" || c.Optional == "true" {
				continue
			}
			if strings.Contains(c.Version, "${") || c.Version == "" {
				// Can't resolve property-placeholder or missing versions
				// without full POM inheritance — skip rather than guess wrong.
				continue
			}
			childKey := c.GroupID + ":" + c.ArtifactID
			if !seen[childKey] {
				visit(fmt.Sprintf("%s:%s:%s", c.GroupID, c.ArtifactID, c.Version))
			}
		}
	}

	for _, d := range directDeps {
		visit(d)
	}
	return resolved
}

func initFile(filename string) {
	if _, err := os.Stat(filename); err == nil {
		fmt.Println("❌ File already exists:", filename)
		os.Exit(1)
	}
	className := strings.TrimSuffix(filepath.Base(filename), ".java")
	template := fmt.Sprintf(`//DEPS com.google.code.gson:gson:2.10.1
//JAVA 21

public class %s {
    public static void main(String[] args) {
        System.out.println("Hello from %s!");
    }
}
`, className, className)
	err := os.WriteFile(filename, []byte(template), 0644)
	if err != nil {
		fmt.Println("❌ Could not create file:", err)
		os.Exit(1)
	}
	fmt.Println("✅ Created:", filename)
	fmt.Println("💡 Run it with: jolt", filename)
}

func clearCache() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("❌ Could not find home dir:", err)
		os.Exit(1)
	}
	cacheDir := filepath.Join(homeDir, ".jolt", "cache")
	err = os.RemoveAll(cacheDir)
	if err != nil {
		fmt.Println("❌ Could not clear cache:", err)
		os.Exit(1)
	}
	fmt.Println("✅ Cache cleared!")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("⚡ jolt - Java, but instant.")
		fmt.Println("")
		fmt.Println("Usage:")
		fmt.Println("  jolt <file.java>        Run a Java file")
		fmt.Println("  jolt init <file.java>   Create a new Java file")
		fmt.Println("  jolt cache clear        Clear the global cache")
		fmt.Println("  jolt version            Show jolt version")
		os.Exit(1)
	}

	// Handle commands
	if os.Args[1] == "init" {
		if len(os.Args) < 3 {
			fmt.Println("Usage: jolt init <file.java>")
			os.Exit(1)
		}
		initFile(os.Args[2])
		os.Exit(0)
	}

	if os.Args[1] == "version" {
		fmt.Println("jolt version 0.1.0")
		os.Exit(0)
	}

	if os.Args[1] == "cache" && len(os.Args) > 2 && os.Args[2] == "clear" {
		clearCache()
		os.Exit(0)
	}

	file := os.Args[1]

	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Println("❌ Error reading file:", err)
		os.Exit(1)
	}

	// Check Java is installed
	if !checkJava() || !checkJavac() {
		fmt.Println("❌ Java not found on your system.")
		fmt.Println("⬇️  Auto-downloading JDK 21...")
		err := downloadJDK()
		if err != nil {
			fmt.Println("❌ Failed to download JDK:", err)
			os.Exit(1)
		}
	}

	// Check //JAVA version requirement
	requiredVersion := extractJavaVersion(string(content))
	if requiredVersion != "" {
		installedVersion := getInstalledJavaVersion()
		if installedVersion == "" {
			fmt.Println("⚠️  Could not detect installed Java version.")
		} else if installedVersion != requiredVersion {
			fmt.Printf("⚠️  This script needs Java %s but you have Java %s\n", requiredVersion, installedVersion)
			fmt.Printf("💡 Install Java %s from: https://adoptium.net\n", requiredVersion)
			os.Exit(1)
		} else {
			fmt.Printf("✅ Java %s detected.\n", installedVersion)
		}
	}

// Resolve dependencies (including transitive runtime deps)
	directDeps := extractDeps(string(content))
	var jarPaths []string

	if len(directDeps) == 0 {
		fmt.Println("📦 No dependencies found.")
	} else {
		fmt.Println("📦 Resolving dependencies...")
		allDeps := resolveTransitive(directDeps)
		if len(allDeps) > len(directDeps) {
			fmt.Printf("   Found %d transitive dependency(ies)\n", len(allDeps)-len(directDeps))
		}
		for _, dep := range allDeps {
			jarPath, err := downloadDep(dep)
			if err != nil {
				fmt.Println("❌ Failed:", dep, err)
				os.Exit(1)
			}
			jarPaths = append(jarPaths, jarPath)
		}
	}	// Build classpath
	classpath := strings.Join(jarPaths, ";")

	// Create temp output dir for .class files
	tmpDir, err := os.MkdirTemp("", "jolt-*")
	if err != nil {
		fmt.Println("❌ Could not create temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Compile
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

	className := strings.TrimSuffix(filepath.Base(file), ".java")

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
