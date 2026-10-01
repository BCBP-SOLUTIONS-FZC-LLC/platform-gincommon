// Package obsregistry exposes the Platform Observability Registry and the
// Enterprise Platform Observability Standard's compliance checks to consuming
// services, so each service can enforce the standard in its own CI:
//
//	func TestMetricsCompliance(t *testing.T) {
//		// ... build the router with gincommon.Config{Domain: "iam", ...},
//		// register custom metrics, drive one request through it ...
//		rep, err := obsregistry.Validate(gincommon.MetricsGatherer(), obsregistry.Options{})
//		if err != nil {
//			t.Fatal(err)
//		}
//		for _, f := range rep.Errors() {
//			t.Error(f)
//		}
//	}
//
// The same checks run against a live scrape with
// go run github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/cmd/metricslint check -url ....
package obsregistry

import (
	"fmt"
	"io"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
)

// Registry is a parsed registry document.
type Registry = obsregistry.Registry

// Metric is one registry entry.
type Metric = obsregistry.Metric

// Label is one entry of the label vocabulary.
type Label = obsregistry.Label

// Options tune validation.
type Options = obsregistry.Options

// Report is the outcome of a validation.
type Report = obsregistry.Report

// Finding is one compliance problem.
type Finding = obsregistry.Finding

// Reference is a metric name found in a dashboard, rule or manifest.
type Reference = obsregistry.Reference

// Tier is a metric's namespace classification.
type Tier = obsregistry.Tier

// Metric tiers.
const (
	TierPlatform = obsregistry.TierPlatform
	TierDomain   = obsregistry.TierDomain
	TierService  = obsregistry.TierService
	TierLegacy   = obsregistry.TierLegacy
	TierExempt   = obsregistry.TierExempt
)

// Status is a registry entry's governance state.
type Status = obsregistry.Status

// Registry statuses.
const (
	StatusApproved   = obsregistry.StatusApproved
	StatusProposed   = obsregistry.StatusProposed
	StatusDeprecated = obsregistry.StatusDeprecated
	StatusRemoved    = obsregistry.StatusRemoved
)

// Severity of a Finding.
type Severity = obsregistry.Severity

// Finding severities. Errors fail CI; warnings are reported only.
const (
	SeverityError   = obsregistry.SeverityError
	SeverityWarning = obsregistry.SeverityWarning
)

// Rule identifiers reported in Finding.Rule.
const (
	RuleNameFormat      = obsregistry.RuleNameFormat
	RuleNamespace       = obsregistry.RuleNamespace
	RuleType            = obsregistry.RuleType
	RuleSuffix          = obsregistry.RuleSuffix
	RuleRequiredLabel   = obsregistry.RuleRequiredLabel
	RuleLabelVocabulary = obsregistry.RuleLabelVocabulary
	RuleProhibitedLabel = obsregistry.RuleProhibitedLabel
	RuleLabelValue      = obsregistry.RuleLabelValue
	RuleLabelName       = obsregistry.RuleLabelName
	RuleCardinality     = obsregistry.RuleCardinality
	RuleStatus          = obsregistry.RuleStatus
	RuleDeprecated      = obsregistry.RuleDeprecated
	RuleRemoved         = obsregistry.RuleRemoved
	RuleGather          = obsregistry.RuleGather
)

// Default returns the registry embedded in this module version.
func Default() *Registry { return obsregistry.Default() }

// Parse decodes and checks a registry document.
func Parse(data []byte) (*Registry, error) { return obsregistry.Parse(data) }

// ServicePrefix returns the <domain>_<service>_ prefix of Tier 3 metrics.
func ServicePrefix(domain, service string) string { return obsregistry.ServicePrefix(domain, service) }

// Validate gathers g and validates the result against the default registry.
//
// When Gather fails, the families it did return are still validated: the
// report is returned together with the gather error, and also carries the
// error as a "gather" error finding, so rep.OK() is false either way.
func Validate(g prometheus.Gatherer, opts Options) (*Report, error) {
	fams, gatherErr := g.Gather()
	rep := Default().ValidateFamilies(fams, opts)
	if gatherErr != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: obsregistry.SeverityError, Rule: obsregistry.RuleGather, Metric: "(gather)", Message: gatherErr.Error(),
		})
		return rep, fmt.Errorf("gather metrics: %w", gatherErr)
	}
	return rep, nil
}

// ValidateText validates a Prometheus text-format scrape against the default registry.
func ValidateText(r io.Reader, opts Options) (*Report, error) {
	return Default().ValidateText(r, opts)
}
