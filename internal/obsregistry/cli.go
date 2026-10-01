package obsregistry

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const usage = `metricslint enforces the Enterprise Platform Observability Standard.

Usage:
  metricslint check [-strict] [-allow-proposed-owner list] [-registry file] [-url URL | file ...]
      Validate a Prometheus text-format or OpenMetrics scrape (files, a /metrics
      URL, or stdin; -url and files are mutually exclusive): namespace
      classification, naming, suffixes, required labels, registry compliance,
      label vocabulary and cardinality.
  metricslint refs [-deny-deprecated] [-ext list] [-registry file] path ...
      Scan dashboards, alert/recording rules, SLOs, HPA manifests and runbooks
      for legacy metric names (with replacements) and unregistered platform_* names.
      Directories are walked recursively, skipping .git, node_modules and vendor.
      An unreadable path (or a file over 64 MiB) is reported as an error finding,
      the scan continues, and the exit code is 2 after all findings are printed.
  metricslint registry [-registry file]
      Validate a registry document (the embedded one by default).
  metricslint inventory [-registry file] [-o file]
      Render the registry as the Markdown observability inventory.

Exit codes: 0 compliant, 1 findings (for registry: the document violates the
standard), 2 usage or I/O error.
`

// defaultRefExtensions are the file types refs scans when walking directories.
const defaultRefExtensions = ".yml,.yaml,.json,.jsonnet,.libsonnet,.rules,.promql,.tf"

// Run is the metricslint command line. It returns the process exit code:
// 0 compliant, 1 findings, 2 usage or I/O error.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	fset := flag.NewFlagSet("metricslint "+cmd, flag.ContinueOnError)
	fset.SetOutput(stderr)
	registryPath := fset.String("registry", "", "registry JSON document (default: the embedded registry)")

	var run func() (int, error)
	switch cmd {
	case "check":
		strict := fset.Bool("strict", false, "treat proposed (unratified) registry metrics as errors")
		owners := fset.String("allow-proposed-owner", "",
			"with -strict, comma-separated registry owners (e.g. platform-gincommon) whose proposed metrics stay warnings")
		url := fset.String("url", "", "scrape this /metrics URL instead of reading files")
		run = func() (int, error) {
			if *url != "" && fset.NArg() > 0 {
				_, _ = fmt.Fprint(stderr, usage)
				return 2, errors.New("check: -url and files are mutually exclusive")
			}
			reg, err := loadRegistry(*registryPath)
			if err != nil {
				return 2, err
			}
			return runCheck(reg, *url, fset.Args(), stdin, stdout, Options{Strict: *strict, AllowProposedOwners: splitList(*owners)})
		}
	case "refs":
		deny := fset.Bool("deny-deprecated", false, "fail on legacy metric references (after dashboards/alerts are migrated)")
		exts := fset.String("ext", defaultRefExtensions, "comma-separated file extensions scanned in directories")
		run = func() (int, error) {
			reg, err := loadRegistry(*registryPath)
			if err != nil {
				return 2, err
			}
			if fset.NArg() == 0 {
				return 2, errors.New("refs: at least one path is required")
			}
			return runRefs(reg, fset.Args(), strings.Split(*exts, ","), *deny, stdout)
		}
	case "registry":
		run = func() (int, error) {
			reg, err := loadRegistry(*registryPath)
			var invalid *invalidRegistryError
			switch {
			case errors.As(err, &invalid):
				return 1, err // readable, but violates the standard: a finding
			case err != nil:
				return 2, err // I/O
			}
			_, _ = fmt.Fprintf(stdout, "registry OK: %d metrics, %d labels, %d domains\n", len(reg.Metrics), len(reg.Labels), len(reg.Domains()))
			return 0, nil
		}
	case "inventory":
		out := fset.String("o", "", "write to this file instead of stdout")
		run = func() (int, error) {
			reg, err := loadRegistry(*registryPath)
			if err != nil {
				return 2, err
			}
			var buf bytes.Buffer
			if err := reg.WriteInventory(&buf); err != nil {
				return 2, err
			}
			if *out == "" {
				_, err = stdout.Write(buf.Bytes())
				return 0, err
			}
			return 0, os.WriteFile(*out, buf.Bytes(), 0o644)
		}
	case "-h", "-help", "--help", "help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}

	if err := fset.Parse(rest); err != nil {
		return 2
	}
	code, err := run()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "metricslint:", err)
	}
	return code
}

func loadRegistry(path string) (*Registry, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reg, err := Parse(data)
	if err != nil {
		return nil, &invalidRegistryError{err: err}
	}
	return reg, nil
}

// invalidRegistryError marks a registry document that was read but failed
// Parse, as opposed to an I/O error reading it.
type invalidRegistryError struct{ err error }

func (e *invalidRegistryError) Error() string { return e.err.Error() }

// splitList splits a comma-separated flag value, dropping empty items.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func runCheck(reg *Registry, url string, files []string, stdin io.Reader, stdout io.Writer, opts Options) (int, error) {
	var inputs []io.Reader
	switch {
	case url != "":
		body, err := fetch(url)
		if err != nil {
			return 2, err
		}
		inputs = append(inputs, bytes.NewReader(body))
	case len(files) == 0:
		inputs = append(inputs, stdin)
	default:
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return 2, err
			}
			inputs = append(inputs, bytes.NewReader(data))
		}
	}
	code := 0
	for _, in := range inputs {
		rep, err := reg.ValidateText(in, opts)
		if err != nil {
			return 2, err
		}
		if _, err := rep.WriteTo(stdout); err != nil {
			return 2, err
		}
		if !rep.OK() {
			code = 1
		}
	}
	return code, nil
}

func fetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	// Prefer the plain text exposition (every exporter supports it); an
	// OpenMetrics-only exporter's response is normalized by ValidateText.
	req.Header.Set("Accept", "text/plain;version=0.0.4;q=1,application/openmetrics-text;q=0.5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBytes+1))
	if err == nil && len(body) > maxScrapeBytes {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", url, maxScrapeBytes)
	}
	return body, err
}

// maxScrapeBytes bounds a fetched scrape; real /metrics pages are far smaller.
const maxScrapeBytes = 64 << 20

// skipDirs are directories refs never descends into: VCS metadata and
// vendored third-party trees.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true}

func runRefs(reg *Registry, paths, exts []string, denyDeprecated bool, stdout io.Writer) (int, error) {
	var refs []Reference
	ioErrs := 0
	scan := func(path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		found, err := reg.ScanReferences(path, f)
		refs = append(refs, found...)
		return err
	}
	// unreadable records an entry that could not be read (or exceeded the
	// scan size limit) as an error finding, so one permission problem does
	// not hide the rest of the tree; any such entry makes the exit code 2
	// (I/O error) once every finding has been printed.
	unreadable := func(path string, err error) {
		ioErrs++
		refs = append(refs, Reference{File: path, Severity: SeverityError, Message: fmt.Sprintf("cannot read: %v", err)})
	}
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			unreadable(root, err)
			continue
		}
		if !info.IsDir() {
			if err := scan(root); err != nil {
				unreadable(root, err)
			}
			continue
		}
		// The walk callback never returns an error other than fs.SkipDir, so
		// WalkDir itself cannot fail here.
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				unreadable(path, err)
				if d != nil && d.IsDir() && path != root {
					return fs.SkipDir
				}
				return nil
			case d.IsDir():
				if path != root && skipDirs[d.Name()] {
					return fs.SkipDir
				}
				return nil
			case !slices.Contains(exts, filepath.Ext(path)):
				return nil
			}
			if err := scan(path); err != nil {
				unreadable(path, err)
			}
			return nil
		})
	}

	errs, warns := 0, 0
	for _, ref := range refs {
		if ref.Severity == SeverityWarning && denyDeprecated {
			ref.Severity = SeverityError
		}
		if ref.Severity == SeverityError {
			errs++
		} else {
			warns++
		}
		_, _ = fmt.Fprintln(stdout, ref)
	}
	_, _ = fmt.Fprintf(stdout, "%d references: %d errors, %d warnings\n", len(refs), errs, warns)
	if ioErrs > 0 {
		return 2, fmt.Errorf("refs: %d path(s) could not be read", ioErrs)
	}
	if errs > 0 {
		return 1, nil
	}
	return 0, nil
}
