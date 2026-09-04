package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ailiheizi/restoreweave/server/internal/releasequal"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("source", "", "absolute path to an already admitted pinned semantic bundle")
	output := flags.String("output", "", "absolute path for a new semantic bundle archive (.tar.gz)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}
	if flags.NArg() != 0 || *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: semantic-bundle-archive --source ABSOLUTE_BUNDLE_DIR --output ABSOLUTE_ARCHIVE.tar.gz")
		os.Exit(2)
	}
	if !filepath.IsAbs(*source) || !filepath.IsAbs(*output) {
		fmt.Fprintln(os.Stderr, "error: --source and --output must be absolute paths")
		os.Exit(2)
	}
	archive, err := releasequal.BuildAdmittedSemanticBundleArchive(context.Background(), *output, *source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: candidate semantic archive was not created: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("status: CANDIDATE_ONLY_NOT_SUPPORTED")
	fmt.Printf("archive: %s\nsha256: %s\nprofile_digest: %s\n", archive.Path, archive.SHA256, archive.ProfileDigest)
}
