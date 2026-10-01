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
  metricslint check [-strict] [-registry file] [-url URL | file ...]
      Validate a Prometheus text-format scrape (files, a /metrics URL, or stdin):
      namespace classification, naming, suffixes, required labels, registry
      compliance, label vocabulary and cardinality.
  metricslint refs [-deny-deprecated] [-ext list] [-registry file] path ...
      Scan dashboards, alert/recording rules, SLOs, HPA manifests and runbooks
      for legacy metric names (with replacements) and unregistered platform_* names.
  metricslint registry [-registry file]
      Validate a registry document (the embedded one by default).
  metricslint inventory [-registry file] [-o file]
      Render the registry as the Markdown observability inventory.
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
		url := fset.String("url", "", "scrape this /metrics URL instead of reading files")
		run = func() (int, error) {
			reg, err := loadRegistry(*registryPath)
			if err != nil {
				return 2, err
			}
			return runCheck(reg, *url, fset.Args(), stdin, stdout, Options{Strict: *strict})
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
			if err != nil {
				return 1, err
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
	return Parse(data)
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
	// Plain text exposition keeps the parser simple and is supported by every exporter.
	req.Header.Set("Accept", "text/plain;version=0.0.4")
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

func runRefs(reg *Registry, paths, exts []string, denyDeprecated bool, stdout io.Writer) (int, error) {
	var refs []Reference
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
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return 2, err
		}
		if !info.IsDir() {
			if err := scan(root); err != nil {
				return 2, err
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !slices.Contains(exts, filepath.Ext(path)) {
				return err
			}
			return scan(path)
		})
		if err != nil {
			return 2, err
		}
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
	if errs > 0 {
		return 1, nil
	}
	return 0, nil
}
