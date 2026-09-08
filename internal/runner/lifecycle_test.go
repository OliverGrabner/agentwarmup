package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
)

func TestUnfinishedLifecycleBlocksRequest(t *testing.T) {
	for _, name := range []string{"lifecycle.json", ".uninstalling"} {
		t.Run(name, func(t *testing.T) {
			paths := config.Paths{Root: t.TempDir()}
			if err := os.WriteFile(filepath.Join(paths.Root, name), []byte("pending"), 0600); err != nil {
				t.Fatal(err)
			}
			r := New(paths)
			r.Execute = func(context.Context, provider.Invocation) provider.Result {
				t.Fatal("request during incomplete lifecycle operation")
				return provider.Result{}
			}
			if err := r.Check(context.Background()); err == nil {
				t.Fatal("ignored lifecycle marker")
			}
		})
	}
}
