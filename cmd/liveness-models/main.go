// Command liveness-models fills a models directory with the files the
// liveness package's manifests name, and verifies each against its SHA-256.
//
// It is the one part of this repository that opens network connections, and
// it is a separate program for that reason: the library an application links
// reads model files from the directory it is given and never downloads
// anything. Run it once, where the application is deployed or when the image
// is built:
//
//	go run github.com/hyz-is/arandu-liveness/cmd/liveness-models -dir /srv/models
//
// Each file is fetched over HTTPS from the source_url its manifest records,
// which pins a revision (or, for a release asset, a release whose digest the
// manifest records), written beside its final name, hashed while it is
// written, and renamed into place only when the digest matches. A file
// already present with the right digest is left alone. -check verifies what
// is there without connecting to anything.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

// maxModelBytes caps one download. Each model is under 2 MB.
const maxModelBytes = 64 << 20

func main() {
	dir := flag.String("dir", "", "the models directory to fill (required)")
	only := flag.String("only", "", "comma-separated model ids to fetch; empty means every model")
	check := flag.Bool("check", false, "verify the files already in -dir and download nothing")
	timeout := flag.Duration("timeout", 30*time.Minute, "how long all downloads together may take")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "liveness-models: -dir is required")
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	if err := run(ctx, *dir, *only, *check); err != nil {
		fmt.Fprintln(os.Stderr, "liveness-models:", err)
		os.Exit(1)
	}
}

// run fetches or checks every selected model and reports each on stdout.
func run(ctx context.Context, dir, only string, check bool) error {
	manifests, err := selected(only)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: httpsOnly}
	failed := 0
	for _, m := range manifests {
		target := filepath.Join(dir, m.File)
		switch ok, err := verify(target, m.SHA256); {
		case ok:
			fmt.Printf("ok       %s %s %s\n", m.ID, m.Version, target)
			continue
		case check:
			fmt.Printf("missing  %s %s %s: %v\n", m.ID, m.Version, target, err)
			failed++
			continue
		}
		if err := download(ctx, client, m, target); err != nil {
			fmt.Printf("failed   %s %s: %v\n", m.ID, m.Version, err)
			failed++
			continue
		}
		fmt.Printf("fetched  %s %s %s\n", m.ID, m.Version, target)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d models are not in place", failed, len(manifests))
	}
	return nil
}

// selected is the manifests named by a comma-separated list, or all of them.
func selected(only string) ([]onnx.Manifest, error) {
	all, err := liveness.Manifests()
	if err != nil {
		return nil, err
	}
	if only == "" {
		return all, nil
	}
	out := []onnx.Manifest{}
	for _, id := range strings.Split(only, ",") {
		m, err := liveness.LookupManifest(strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// verify reports whether the file exists and has the digest, and otherwise
// why not.
func verify(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return false, fmt.Errorf("sha256 is %s and the manifest records %s", got, want)
	}
	return true, nil
}

// httpsOnly refuses a redirect that leaves HTTPS, or one too many.
func httpsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing a redirect to %s: only https is followed", req.URL.Redacted())
	}
	if len(via) >= 10 {
		return errors.New("more than 10 redirects")
	}
	return nil
}

// download fetches one model into target through a temporary file beside it,
// and renames it into place only when its digest is the manifest's.
func download(ctx context.Context, client *http.Client, m onnx.Manifest, target string) error {
	source, err := url.Parse(m.SourceURL)
	if err != nil || source.Scheme != "https" {
		return fmt.Errorf("source_url %q is not https", m.SourceURL)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", source.Redacted(), resp.Status)
	}

	partial, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.part")
	if err != nil {
		return err
	}
	defer os.Remove(partial.Name())
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(partial, h), io.LimitReader(resp.Body, maxModelBytes+1))
	if closeErr := partial.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if written > maxModelBytes {
		return fmt.Errorf("the file is larger than %d bytes", maxModelBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != m.SHA256 {
		return fmt.Errorf("downloaded sha256 is %s and the manifest records %s: the file was not kept", got, m.SHA256)
	}
	if err := os.Chmod(partial.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(partial.Name(), target)
}
