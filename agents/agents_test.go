package agents

import "testing"

func TestDomainTypeString(t *testing.T) {
	if DomainGeneral.String() != "general" {
		t.Fatalf("DomainGeneral.String() = %q, want %q", DomainGeneral.String(), "general")
	}
	if DomainCode.String() != "code" {
		t.Fatalf("DomainCode.String() = %q, want %q", DomainCode.String(), "code")
	}
	if DomainType(99).String() != "unknown" {
		t.Fatalf("unexpected DomainType(99).String() = %q", DomainType(99).String())
	}
}
