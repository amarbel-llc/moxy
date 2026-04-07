package proxy

import (
	"testing"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

func boolPtr(b bool) *bool { return &b }

// Issue #29: annotation filters should use OR semantics, not AND.

func TestAnnotationFilter_ORSemantics_ReadOnlyOnly(t *testing.T) {
	// A tool with only readOnlyHint=true should match a filter that
	// includes readOnlyHint=true, even if the filter also includes
	// idempotentHint=true and the tool doesn't set idempotentHint.
	filter := &config.AnnotationFilter{
		ReadOnlyHint:   boolPtr(true),
		IdempotentHint: boolPtr(true),
	}
	annotations := &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(true),
		// idempotentHint not set — nil
	}

	if !matchesAnnotationFilter(annotations, filter) {
		t.Error("tool with readOnlyHint=true should match filter with readOnlyHint=true OR idempotentHint=true, but was rejected (AND semantics bug)")
	}
}

func TestAnnotationFilter_ORSemantics_IdempotentOnly(t *testing.T) {
	// A tool with only idempotentHint=true should match a filter that
	// includes readOnlyHint=true OR idempotentHint=true.
	filter := &config.AnnotationFilter{
		ReadOnlyHint:   boolPtr(true),
		IdempotentHint: boolPtr(true),
	}
	annotations := &protocol.ToolAnnotations{
		IdempotentHint: boolPtr(true),
		// readOnlyHint not set — nil
	}

	if !matchesAnnotationFilter(annotations, filter) {
		t.Error("tool with idempotentHint=true should match filter with readOnlyHint=true OR idempotentHint=true, but was rejected (AND semantics bug)")
	}
}

func TestAnnotationFilter_ORSemantics_NilAnnotations(t *testing.T) {
	// A tool with no annotations at all should NOT match any filter.
	filter := &config.AnnotationFilter{
		ReadOnlyHint: boolPtr(true),
	}

	if matchesAnnotationFilter(nil, filter) {
		t.Error("tool with nil annotations should not match any filter")
	}
}

func TestAnnotationFilter_ORSemantics_NoMatchingHints(t *testing.T) {
	// A tool whose annotations don't match ANY of the filter's hints
	// should be rejected.
	filter := &config.AnnotationFilter{
		ReadOnlyHint: boolPtr(true),
	}
	annotations := &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(false), // explicitly false, filter wants true
	}

	if matchesAnnotationFilter(annotations, filter) {
		t.Error("tool with readOnlyHint=false should not match filter with readOnlyHint=true")
	}
}

func TestAnnotationFilter_NilFilter(t *testing.T) {
	// Nil filter should match everything.
	annotations := &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(true),
	}

	if !matchesAnnotationFilter(annotations, nil) {
		t.Error("nil filter should match all tools")
	}
	if !matchesAnnotationFilter(nil, nil) {
		t.Error("nil filter should match nil annotations")
	}
}

func TestAnnotationFilter_ORSemantics_SingleHint(t *testing.T) {
	// Single-hint filter should work like before — exact match required.
	filter := &config.AnnotationFilter{
		ReadOnlyHint: boolPtr(true),
	}
	annotations := &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(true),
	}

	if !matchesAnnotationFilter(annotations, filter) {
		t.Error("tool with readOnlyHint=true should match filter with readOnlyHint=true")
	}
}
