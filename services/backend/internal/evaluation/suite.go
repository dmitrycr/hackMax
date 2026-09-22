// Package evaluation compares synthetic cases with declared expected outcomes.
// Status/source matching is not a replacement for human semantic review.
package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"hackmax/backend/internal/modules/semantic"
)

type Expected struct {
	RequirementID   string   `json:"requirement_id"`
	Status          string   `json:"status"`
	RequiredSources []string `json:"required_source_ids"`
	AllowedSources  []string `json:"allowed_source_ids"`
}
type Case struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Input    semantic.Input `json:"input"`
	Expected []Expected     `json:"expected"`
}
type Suite struct {
	Version   string `json:"version"`
	Synthetic bool   `json:"synthetic"`
	Cases     []Case `json:"cases"`
}

func Load(data []byte) (Suite, string, error) {
	var suite Suite
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&suite); err != nil {
		return suite, "", err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return suite, "", errors.New("extra data after suite")
	}
	if !suite.Synthetic || suite.Version == "" || len(suite.Cases) == 0 {
		return suite, "", errors.New("a versioned, nonempty synthetic suite is required")
	}
	ids := map[string]bool{}
	for _, c := range suite.Cases {
		if strings.TrimSpace(c.ID) == "" || ids[c.ID] {
			return suite, "", errors.New("invalid or duplicate case ID")
		}
		ids[c.ID] = true
		if err := semantic.ValidateInput(c.Input); err != nil {
			return suite, "", fmt.Errorf("case %s: %w", c.ID, err)
		}
		if len(c.Expected) != len(c.Input.Requirements) {
			return suite, "", errors.New("expected findings must cover every requirement")
		}
		requirements, sources := map[string]bool{}, map[string]bool{}
		for _, r := range c.Input.Requirements {
			requirements[r.ID] = true
		}
		for _, f := range c.Input.Fragments {
			sources[f.ID] = true
		}
		for _, e := range c.Expected {
			if !requirements[e.RequirementID] {
				return suite, "", errors.New("invalid or duplicate expected requirement")
			}
			delete(requirements, e.RequirementID)
			if e.Status != "met" && e.Status != "needs_review" && e.Status != "insufficient_data" {
				return suite, "", errors.New("invalid expected status")
			}
			allowed := map[string]bool{}
			for _, id := range e.AllowedSources {
				if !sources[id] || allowed[id] {
					return suite, "", errors.New("invalid allowed source")
				}
				allowed[id] = true
			}
			required := map[string]bool{}
			for _, id := range e.RequiredSources {
				if !allowed[id] || required[id] {
					return suite, "", errors.New("invalid required source")
				}
				required[id] = true
			}
			if e.Status != "insufficient_data" && len(required) == 0 {
				return suite, "", errors.New("expected conclusion requires evidence")
			}
		}
	}
	return suite, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
