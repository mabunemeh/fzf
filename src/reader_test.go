package fzf

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junegunn/fzf/src/util"
)

func TestReadFromCommand(t *testing.T) {
	strs := []string{}
	eb := util.NewEventBox()
	exec := util.NewExecutor("")
	reader := NewReader(
		func(s []byte) bool { strs = append(strs, string(s)); return true },
		eb, exec, false, true)

	reader.startEventPoller()

	// Check EventBox
	if eb.Peek(EvtReadNew) {
		t.Error("EvtReadNew should not be set yet")
	}

	// Normal command
	counter := 0
	ready := func() {
		counter++
	}
	reader.fin(reader.readFromCommand(`echo abc&&echo def`, nil, ready))
	if len(strs) != 2 || strs[0] != "abc" || strs[1] != "def" || counter != 1 {
		t.Errorf("%s", strs)
	}

	// Check EventBox again
	eb.WaitFor(EvtReadFin)

	// Wait should return immediately
	eb.Wait(func(events *util.Events) {
		events.Clear()
	})

	// EventBox is cleared
	if eb.Peek(EvtReadNew) {
		t.Error("EvtReadNew should not be set yet")
	}

	// Make sure that event poller is finished
	time.Sleep(readerPollIntervalMax)

	// Restart event poller
	reader.startEventPoller()

	// Failing command
	reader.fin(reader.readFromCommand(`no-such-command`, nil, ready))
	strs = []string{}
	if len(strs) > 0 || counter != 2 {
		t.Errorf("%s", strs)
	}

	// Check EventBox again
	if eb.Peek(EvtReadNew) {
		t.Error("Command failed. EvtReadNew should not be set")
	}
	if !eb.Peek(EvtReadFin) {
		t.Error("EvtReadFin should be set")
	}
}

func TestReadFilesSymlinkAcrossRoots(t *testing.T) {
	base := t.TempDir()
	app := filepath.Join(base, "app")
	shared := filepath.Join(base, "shared")
	frontend := filepath.Join(shared, "frontend")
	for _, dir := range []string{app, frontend} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(shared, "docs.txt"), filepath.Join(frontend, "index.js")} {
		if err := os.WriteFile(file, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("..", "shared"), filepath.Join(app, "shared")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	for _, tc := range []struct {
		name  string
		roots []string
	}{
		{"single-root", []string{app}},
		{"app-first", []string{app, frontend}},
		{"frontend-first", []string{frontend, app}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := tc.roots
			var mu sync.Mutex
			var files []string
			reader := NewReader(func(data []byte) bool {
				mu.Lock()
				defer mu.Unlock()
				files = append(files, filepath.FromSlash(string(data)))
				return true
			}, util.NewEventBox(), nil, false, false)
			if !reader.readFiles(roots, walkerOpts{file: true, follow: true}, nil) {
				t.Fatal("file walk failed")
			}
			want := []string{filepath.Join(app, "shared", "docs.txt"), filepath.Join(app, "shared", "frontend", "index.js")}
			if len(roots) > 1 {
				want = append(want, filepath.Join(frontend, "index.js"))
			}
			slices.Sort(files)
			slices.Sort(want)
			if !slices.Equal(files, want) {
				t.Errorf("files = %v, want %v", files, want)
			}
		})
	}
}

func TestReadFilesSkipsAncestorSymlinks(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	if err := os.Mkdir(project, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(project, "file.txt")
	for _, name := range []string{file, filepath.Join(base, "outside.txt")} {
		if err := os.WriteFile(name, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"parent": "..", "self": "."} {
		if err := os.Symlink(target, filepath.Join(project, name)); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
	}
	var mu sync.Mutex
	var files []string
	reader := NewReader(func(data []byte) bool {
		mu.Lock()
		defer mu.Unlock()
		files = append(files, filepath.FromSlash(string(data)))
		return true
	}, util.NewEventBox(), nil, false, false)
	if !reader.readFiles([]string{project}, walkerOpts{file: true, follow: true}, nil) {
		t.Fatal("file walk failed")
	}
	if !slices.Equal(files, []string{file}) {
		t.Errorf("files = %v, want [%s]", files, file)
	}
}
