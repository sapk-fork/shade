package fusefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/asjoyner/shade"
	"github.com/asjoyner/shade/drive"
)

// Node is a very compact representation of a file
type Node struct {
	// Filename is the complete path to a node, with no leading or trailing
	// slash.
	Filename     string
	Filesize     uint64 // in bytes
	ModifiedTime time.Time
	Sha256sum    []byte // the sha of the full shade.File
	// Children is a map indicating the presence of a node immediately
	// below the current node in the tree.  The key is only the name of that
	// node, a relative path, not fully qualified.
	// TODO(asjoyner): use a struct{} here for efficiency?
	// unsafe.Sizeof indicates it would save 1 byte per FS entry
	Children map[string]bool
	// TODO(asjoyner): update LastSeen each poll, timeout entries so deleted
	// files eventually disappear from Tree.
	// LastSeen time.Time
}

// Synthetic returns true for synthetically created directories.
func (n *Node) Synthetic() bool {
	if n.Sha256sum == nil {
		return true
	}
	return false
}

// Tree is a representation of all files known to the provided drive.Client.
// It initially downlods, then periodically refreshes, the set of files by
// calling ListFiles and GetChunk.  It presents methods to query for what file
// object currently describes what path, and can return a Node or shade.File
// struct representing that node in the tree.
type Tree struct {
	client drive.Client
	nodes  map[string]Node // full path to node
	nm     sync.RWMutex    // protects nodes
	debug  bool
}

// NewTree queries client to discover all the shade.File(s).  It returns a Tree
// object which is ready to answer questions about the nodes in the file tree.
// If the initial query fails, an error is returned instead.
func NewTree(client drive.Client, refresh *time.Ticker) (*Tree, error) {
	t := &Tree{
		client: client,
		nodes: map[string]Node{
			"/": {
				Filename: "/",
				Children: make(map[string]bool),
			}},
	}
	if err := t.Refresh(); err != nil {
		return nil, fmt.Errorf("initializing Tree: %s", err)
	}
	if refresh != nil {
		go t.periodicRefresh(refresh)
	}
	return t, nil
}

// NodeByPath returns a Node describing the given path.
func (t *Tree) NodeByPath(p string) (Node, error) {
	t.nm.RLock()
	defer t.nm.RUnlock()
	if n, ok := t.nodes[p]; ok {
		return n, nil
	}
	t.log("known nodes:\n")
	for _, n := range t.nodes {
		t.log(fmt.Sprintf("%+v\n", n))
	}
	return Node{}, fmt.Errorf("no such node: %q", p)
}

func unmarshalChunk(fj, sha []byte) (*shade.File, error) {
	file := &shade.File{}
	if err := json.Unmarshal(fj, file); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal sha256sum %x: %s", sha, err)
	}
	return file, nil
}

// FileByNode returns the full shade.File object for a given node.
func (t *Tree) FileByNode(n Node) (*shade.File, error) {
	if n.Synthetic() {
		return nil, errors.New("no shade.File defined")
	}
	fj, err := t.client.GetChunk(n.Sha256sum)
	if err != nil {
		return nil, fmt.Errorf("GetChunk(%x): %s", n.Sha256sum, err)
	}
	if fj == nil || len(fj) == 0 {
		return nil, fmt.Errorf("Could not find JSON for node: %q", n.Filename)
	}

	f := &shade.File{}
	if err := f.FromJSON(fj); err != nil {
		return nil, err
	}
	return f, nil
}

// HasChild returns true if child exists immediately below parent in the file
// tree.
func (t *Tree) HasChild(parent, child string) bool {
	t.nm.RLock()
	defer t.nm.RUnlock()
	return t.nodes[parent].Children[child]
}

// NumNodes returns the number of nodes (files + synthetic directories) in the
// system.
func (t *Tree) NumNodes() int {
	t.nm.RLock()
	defer t.nm.RUnlock()
	return len(t.nodes)
}

// GetChunk is not yet implemented.
func (t *Tree) GetChunk(sha256sum []byte) {
}

// Refresh updates the cached view of the Tree by calling ListFiles and
// processing the result.
func (t *Tree) Refresh() error {
	t.log("Begining cache refresh cycle.")
	// key is a string([]byte) representation of the file's SHA2
	knownNodes := make(map[string]bool)
	newFiles, err := t.client.ListFiles()
	if err != nil {
		return fmt.Errorf("%q ListFiles(): %s", t.client.GetConfig().Provider, err)
	}
	t.log(fmt.Sprintf("Found %d file(s) via %s", len(newFiles), t.client.GetConfig().Provider))
	// fetch all those files into the local disk cache
	for _, sha256sum := range newFiles {
		// check if we have already processed this Node
		if knownNodes[string(sha256sum)] {
			continue // we've already processed this file
		}

		// fetch the file Chunk
		f, err := t.client.GetChunk(sha256sum)
		if err != nil {
			// TODO(asjoyner): if !client.Local()... retry?
			log.Printf("Failed to fetch file %x: %s", sha256sum, err)
			continue
		}
		// unmarshal and populate t.nodes as the shade.files go by
		file := &shade.File{}
		if err := file.FromJSON(f); err != nil {
			log.Printf("%v", err)
			continue
		}
		node := Node{
			Filename:     file.Filename,
			Filesize:     uint64(file.Filesize),
			ModifiedTime: file.ModifiedTime,
			Sha256sum:    sha256sum,
			Children:     nil,
		}
		t.nm.Lock()
		// TODO(asjoyner): handle file + directory collisions
		if existing, ok := t.nodes[node.Filename]; ok && existing.ModifiedTime.After(node.ModifiedTime) {
			t.nm.Unlock()
			continue
		}
		t.nodes[node.Filename] = node
		t.addParents(node.Filename)
		t.nm.Unlock()
		knownNodes[string(sha256sum)] = true
	}
	t.log(fmt.Sprintf("Refresh complete with %d file(s).", len(knownNodes)))
	return nil
}

// recursive function to update parent dirs
func (t *Tree) addParents(filepath string) {
	dir, f := path.Split(filepath)
	if dir == "" {
		dir = "/"
	} else {
		dir = strings.TrimSuffix(dir, "/")
	}
	t.log(fmt.Sprintf("adding %q as a child of %q", f, dir))
	// TODO(asjoyner): handle file + directory collisions
	if parent, ok := t.nodes[dir]; !ok {
		// if the parent node doesn't yet exist, initialize it
		t.nodes[dir] = Node{
			Filename: dir,
			Children: map[string]bool{f: true},
		}
	} else {
		parent.Children[f] = true
	}
	if dir != "/" {
		t.addParents(dir)
	}
}

func (t *Tree) periodicRefresh(refresh *time.Ticker) {
	for {
		<-refresh.C
		t.Refresh()
	}
}

// Debug causes the client to print helpful message via the log library.
func (t *Tree) Debug() {
	t.debug = true
}

func (t *Tree) log(msg string) {
	if t.debug {
		log.Printf("CACHE: %s\n", msg)
	}
}
