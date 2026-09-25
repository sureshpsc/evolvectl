// Package ai defines the optional reasoning provider. The noop provider is the default.
package ai

import "github.com/sureshpsc/evolvectl/internal/redact"

// Proposal is a review item. Applying it is a separate policy decision.
type Proposal struct {
	Provider   string
	Summary    string
	Patch      string
	Confidence string
}

// Provider proposes patches. It must not write files.
type Provider interface {
	Name() string
	Propose(summary string) (Proposal, error)
}

// Noop never produces a patch and never contacts a network.
type Noop struct{}

func (Noop) Name() string { return "noop" }

func (Noop) Propose(summary string) (Proposal, error) {
	return Proposal{
		Provider:   "noop",
		Summary:    "AI is disabled. " + redact.Text(summary),
		Confidence: "LOW",
	}, nil
}

// Static is a reference provider that returns text for review and does not apply it.
type Static struct{}

func (Static) Name() string { return "static-review" }

func (Static) Propose(summary string) (Proposal, error) {
	return Proposal{
		Provider:   "static-review",
		Summary:    redact.Text(summary),
		Patch:      "",
		Confidence: "LOW",
	}, nil
}
