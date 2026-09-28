// dg-release-check verifies the files made by the publish-free release build.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const product = "delegator"

type target struct {
	os, arch, extension, binary string
}

var targets = []target{
	{"darwin", "amd64", ".tar.gz", "dg"},
	{"darwin", "arm64", ".tar.gz", "dg"},
	{"linux", "amd64", ".tar.gz", "dg"},
	{"linux", "arm64", ".tar.gz", "dg"},
	{"windows", "amd64", ".zip", "dg.exe"},
	{"windows", "arm64", ".zip", "dg.exe"},
}

type member struct {
	data    []byte
	mode    os.FileMode
	modTime time.Time
}

func main() {
	dist := flag.String("dist", "dist", "directory containing GoReleaser output")
	version := flag.String("version", "", "artifact version without a leading v")
	flag.Parse()

	if *version == "" {
		fmt.Fprintln(os.Stderr, "dg-release-check: -version is required")
		os.Exit(2)
	}
	if err := check(*dist, *version); err != nil {
		fmt.Fprintln(os.Stderr, "dg-release-check:", err)
		os.Exit(1)
	}
	fmt.Printf("release artifacts for v%s are valid\n", *version)
}

func check(dist, version string) error {
	names := artifactNames(version)
	checksumName := fmt.Sprintf("%s_%s_checksums.txt", product, version)
	if err := checkTopLevelNames(dist, append(names, checksumName)); err != nil {
		return err
	}
	checksumNames := append([]string(nil), names...)
	sort.Strings(checksumNames)
	if err := checkChecksums(dist, checksumName, checksumNames); err != nil {
		return err
	}

	for i, target := range targets {
		name := names[i]
		members, err := readArchive(filepath.Join(dist, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := checkBinaryArchive(name, target, members, version); err != nil {
			return err
		}
	}
	return checkSourceArchive(filepath.Join(dist, names[len(names)-1]), version)
}

func artifactNames(version string) []string {
	names := make([]string, 0, len(targets)+1)
	for _, target := range targets {
		names = append(names, fmt.Sprintf("%s_%s_%s_%s%s", product, version, target.os, target.arch, target.extension))
	}
	return append(names, fmt.Sprintf("%s_%s_source.tar.gz", product, version))
}

func checkTopLevelNames(dist string, want []string) error {
	entries, err := os.ReadDir(dist)
	if err != nil {
		return err
	}
	var got []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, "_checksums.txt")) {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("release files are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	return nil
}

func checkChecksums(dist, checksumName string, want []string) error {
	contents, err := os.ReadFile(filepath.Join(dist, checksumName))
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != len(want) {
		return fmt.Errorf("%s has %d entries, want %d", checksumName, len(lines), len(want))
	}
	for i, name := range want {
		fields := strings.Fields(lines[i])
		if len(fields) != 2 || fields[1] != name {
			return fmt.Errorf("%s line %d is %q, want a digest and %s", checksumName, i+1, lines[i], name)
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || fields[0] != strings.ToLower(fields[0]) {
			return fmt.Errorf("%s line %d has an invalid SHA-256 digest", checksumName, i+1)
		}
		artifact, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			return err
		}
		got := sha256.Sum256(artifact)
		if !bytes.Equal(digest, got[:]) {
			return fmt.Errorf("SHA-256 mismatch for %s", name)
		}
	}
	return nil
}

func checkBinaryArchive(name string, target target, members map[string]member, version string) error {
	want := []string{"LICENSE", "TRADEMARKS.md", target.binary}
	var got []string
	for memberName := range members {
		got = append(got, memberName)
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("%s contains %v, want %v", name, got, want)
	}
	if members[target.binary].mode.Perm() != 0755 || members["LICENSE"].mode.Perm() != 0644 || members["TRADEMARKS.md"].mode.Perm() != 0644 {
		return fmt.Errorf("%s has unexpected file modes", name)
	}
	stamp := members[target.binary].modTime.Unix()
	if stamp <= 0 || members["LICENSE"].modTime.Unix() != stamp || members["TRADEMARKS.md"].modTime.Unix() != stamp {
		return fmt.Errorf("%s does not give every member the source timestamp", name)
	}
	if err := checkEmbeddedVersion(members[target.binary].data, version); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if target.os == runtime.GOOS && target.arch == runtime.GOARCH {
		if err := checkReportedVersion(members[target.binary].data, target.binary, version); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func checkEmbeddedVersion(binary []byte, version string) error {
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return err
	}
	if info.Path != "github.com/alcubie/delegator/cmd/dg" {
		return fmt.Errorf("main package is %q", info.Path)
	}
	want := []byte("v" + version)
	if !bytes.Contains(binary, want) {
		return fmt.Errorf("binary does not contain the injected version %q", want)
	}
	return nil
}

func checkReportedVersion(binary []byte, name, version string) error {
	dir, err := os.MkdirTemp("", "dg-release-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, binary, 0755); err != nil {
		return err
	}
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run dg version: %w: %s", err, out)
	}
	if got, want := string(out), "dg v"+version+"\n"; got != want {
		return fmt.Errorf("dg version wrote %q, want %q", got, want)
	}
	return nil
}

func checkSourceArchive(path, version string) error {
	members, err := readArchive(path)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s_%s/", product, version)
	for _, required := range []string{"LICENSE", "TRADEMARKS.md"} {
		if _, ok := members[prefix+required]; !ok {
			return fmt.Errorf("source archive has no %s", required)
		}
	}
	return nil
}

func readArchive(path string) (map[string]member, error) {
	if strings.HasSuffix(path, ".zip") {
		return readZIP(path)
	}
	if strings.HasSuffix(path, ".tar.gz") {
		return readTarGZ(path)
	}
	return nil, errors.New("unsupported archive format")
}

func readZIP(path string) (map[string]member, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	members := make(map[string]member)
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		members[file.Name] = member{data, file.Mode(), file.Modified}
	}
	return members, nil
}

func readTarGZ(path string) (map[string]member, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	members := make(map[string]member)
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return members, nil
		}
		if err != nil {
			return nil, err
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		members[header.Name] = member{data, os.FileMode(header.Mode), header.ModTime}
	}
}
