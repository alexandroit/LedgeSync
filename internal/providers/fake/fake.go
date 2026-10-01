// Package fake supplies a read-only, deterministic provider for offline previews.
package fake

import (
	"context"
	"github.com/alexandroit/LedgeSync/internal/domain"
)

type Provider struct {
	Inventory domain.Inventory
	ListError error
}

func Empty() *Provider {
	return &Provider{Inventory: domain.Inventory{Identity: "fake:empty-offline", Complete: true, Entries: []domain.RemoteEntry{}}}
}
func (p *Provider) List(ctx context.Context) (domain.Inventory, error) {
	if ctx.Err() != nil {
		return domain.Inventory{}, domain.Fail("CANCELLED", "fake listing cancelled")
	}
	if p.ListError != nil {
		return domain.Inventory{}, p.ListError
	}
	v := p.Inventory
	v.Entries = append([]domain.RemoteEntry{}, v.Entries...)
	return v, nil
}
