package fastresolver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestTimeoutResolver_Lookup_Normal(t *testing.T) {
	base, err := NewResolver("8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTimeoutResolver(base, 5*time.Second)

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestTimeoutResolver_Lookup_Timeout(t *testing.T) {
	base, err := NewResolver("8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	// Use an extremely small timeout to guarantee a timeout error
	r := NewTimeoutResolver(base, 1*time.Microsecond)

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err == nil {
		t.Fatal("expected a timeout error, but got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded error, got %v", err)
	}
}

func TestResolver_WithCustomTimeout(t *testing.T) {
	// Set an extremely short custom socket timeout on the base resolver
	r, err := NewResolverWithTimeout("8.8.8.8", 1*time.Microsecond)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Lookup(context.Background(), "google.com", dns.TypeA)
	if err == nil {
		t.Fatal("expected error due to short custom socket timeout, but got nil")
	}
}
