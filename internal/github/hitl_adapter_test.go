package github

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
)

func TestAdapterSatisfiesGHAdapter(t *testing.T) {
	// Compile-time check that HITLAdapter satisfies hitl.GHAdapter
	var _ hitl.GHAdapter = (*HITLAdapter)(nil)
}
