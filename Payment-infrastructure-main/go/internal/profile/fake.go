package profile

import (
	"context"

	"nazar/internal/contract"
)

// FakeProfileStore is the deterministic in-memory second implementation of
// contract.ProfileStore required by docs/00 §3.2 ("every seam has >=2 implementations from
// day one"). Tests construct a bundle directly and hand it back on Load; Apply is a no-op
// recorder. It is also what makes test_no_io_after_profile_load meaningful: swap this in and
// assert zero network calls happen downstream of Load.
type FakeProfileStore struct {
	Bundles map[string]*contract.ProfileBundle // keyed by debtor_account
	Applied []*contract.Event
}

func NewFakeProfileStore() *FakeProfileStore {
	return &FakeProfileStore{Bundles: map[string]*contract.ProfileBundle{}}
}

func (f *FakeProfileStore) Load(ctx context.Context, ev *contract.Event) (*contract.ProfileBundle, error) {
	if b, ok := f.Bundles[ev.DebtorAccount]; ok {
		return b, nil
	}
	return &contract.ProfileBundle{}, nil
}

func (f *FakeProfileStore) Apply(ctx context.Context, ev *contract.Event) error {
	f.Applied = append(f.Applied, ev)
	return nil
}
