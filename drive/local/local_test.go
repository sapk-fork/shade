package local

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"

	"github.com/asjoyner/shade/drive"
)

func TestFileRoundTrip(t *testing.T) {
	dir, err := ioutil.TempDir("", "localdiskTest")
	if err != nil {
		t.Fatal(err)
	}
	defer tearDown(dir)
	ld, err := NewClient(drive.Config{
		Provider:      "localdisk",
		FileParentID:  path.Join(dir, "files"),
		ChunkParentID: path.Join(dir, "chunks"),
		MaxFiles:      100,
	})
	if err != nil {
		t.Fatalf("initializing client: %s", err)
	}
	drive.TestFileRoundTrip(t, ld, 200)
}

func TestChunkRoundTrip(t *testing.T) {
	dir, err := ioutil.TempDir("", "localdiskTest")
	if err != nil {
		t.Fatal(err)
	}
	defer tearDown(dir)
	ld, err := NewClient(drive.Config{
		Provider:      "localdisk",
		FileParentID:  path.Join(dir, "files"),
		ChunkParentID: path.Join(dir, "chunks"),
		MaxChunkBytes: 100 * 256 * 50,
	})
	if err != nil {
		t.Fatalf("initializing client: %s", err)
	}
	drive.TestChunkRoundTrip(t, ld, 100)
}

func TestParallelRoundTrip(t *testing.T) {
	dir, err := ioutil.TempDir("", "localdiskTest")
	if err != nil {
		t.Fatal(err)
	}
	defer tearDown(dir)
	ld, err := NewClient(drive.Config{
		Provider:      "localdisk",
		FileParentID:  path.Join(dir, "files"),
		ChunkParentID: path.Join(dir, "chunks"),
		// MaxFiles and MaxChunkBytes must leave sufficient head room for the 10
		// copies to function in parallel
		MaxFiles: 1000,
	})
	if err != nil {
		t.Fatalf("initializing client: %s", err)
	}
	drive.TestParallelRoundTrip(t, ld, 100)
}

func TestDirRequired(t *testing.T) {
	_, err := NewClient(drive.Config{
		Provider:      "localdisk",
		FileParentID:  "/proc/sure/hope/this/fails",
		ChunkParentID: "/proc/sure/hope/this/fails",
	})
	if err == nil {
		t.Fatalf("expected error on inaccessible directory: %s", err)
	}
}

func tearDown(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("Could not clean up: %s", err)
	}
}
