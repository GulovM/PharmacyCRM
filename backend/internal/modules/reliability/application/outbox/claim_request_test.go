package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
)

func TestClaimRequestValidate(t *testing.T) {
	valid := ClaimRequest{
		Owner:         "worker-1",
		Limit:         3,
		LeaseDuration: time.Minute,
		Now:           time.Now(),
		Protocols:     []EventKey{{Name: "inventory.changed", Version: 1}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name   string
		change func(*ClaimRequest)
	}{
		{"empty owner", func(request *ClaimRequest) { request.Owner = "" }},
		{"owner whitespace", func(request *ClaimRequest) { request.Owner = " worker-1" }},
		{"zero limit", func(request *ClaimRequest) { request.Limit = 0 }},
		{"oversized limit", func(request *ClaimRequest) { request.Limit = 101 }},
		{"invalid lease", func(request *ClaimRequest) { request.LeaseDuration = 0 }},
		{"zero time", func(request *ClaimRequest) { request.Now = time.Time{} }},
		{"no protocols", func(request *ClaimRequest) { request.Protocols = nil }},
		{"invalid protocol name", func(request *ClaimRequest) { request.Protocols[0].Name = "" }},
		{"invalid protocol version", func(request *ClaimRequest) { request.Protocols[0].Version = 0 }},
		{"duplicate protocol", func(request *ClaimRequest) { request.Protocols = append(request.Protocols, request.Protocols[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			request.Protocols = append([]EventKey(nil), valid.Protocols...)
			tt.change(&request)
			err := request.Validate()
			if !errors.Is(err, ErrInvalidClaimRequest) || !errors.Is(err, apperror.ErrInvalidArgument) {
				t.Fatalf("expected typed invalid request error, got %v", err)
			}
		})
	}
}
