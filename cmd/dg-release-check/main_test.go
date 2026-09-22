package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestArtifactNamesAreThePublicContract(t *testing.T) {
	want := []string{
		"alcubi-delegator_1.2.3_darwin_amd64.tar.gz",
		"alcubi-delegator_1.2.3_darwin_arm64.tar.gz",
		"alcubi-delegator_1.2.3_linux_amd64.tar.gz",
		"alcubi-delegator_1.2.3_linux_arm64.tar.gz",
		"alcubi-delegator_1.2.3_windows_amd64.zip",
		"alcubi-delegator_1.2.3_windows_arm64.zip",
		"alcubi-delegator_1.2.3_source.tar.gz",
	}
	if got := artifactNames("1.2.3"); !reflect.DeepEqual(got, want) {
		t.Errorf("artifact names = %v, want %v", got, want)
	}
}

func TestChecksumsCoverTheArtifactsInOrder(t *testing.T) {
	dir := t.TempDir()
	names := []string{"first.tar.gz", "second.zip"}
	var lines string
	for _, name := range names {
		contents := []byte("contents of " + name)
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		lines += fmt.Sprintf("%x  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "sums.txt"), []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkChecksums(dir, "sums.txt", names); err != nil {
		t.Fatal(err)
	}
}

func TestReadTarGZPreservesNamesModesAndTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	stamp := time.Unix(1234567890, 0)
	contents := []byte("binary")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "dg", Mode: 0755, Size: int64(len(contents)), ModTime: stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	members, err := readArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := members["dg"]; string(got.data) != "binary" || got.mode.Perm() != 0755 || !got.modTime.Equal(stamp) {
		t.Errorf("dg = %#v", got)
	}
}

func TestReadZIPPreservesNamesModesAndTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(file)
	stamp := time.Date(2009, 2, 13, 23, 31, 30, 0, time.UTC)
	header := &zip.FileHeader{Name: "dg.exe", Method: zip.Deflate}
	header.SetMode(0755)
	header.Modified = stamp
	memberWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memberWriter.Write([]byte("binary")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	members, err := readArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := members["dg.exe"]; string(got.data) != "binary" || got.mode.Perm() != 0755 || !got.modTime.Equal(stamp) {
		t.Errorf("dg.exe = %#v", got)
	}
}
