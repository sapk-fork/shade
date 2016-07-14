package shade

import (
	"log"
	"os"
	"path"
	"runtime"
)

// ConfigDir identifies the correct path to store persistent configuration data
// on various operating systems.
func ConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		return path.Join(os.Getenv("HOME"), "Library", "Application Support", "shade")
	case "linux", "freebsd":
		return path.Join(os.Getenv("HOME"), ".shade")
	default:
		log.Printf("TODO: ConfigDir on GOOS %q", runtime.GOOS)
		return "."
	}
}
