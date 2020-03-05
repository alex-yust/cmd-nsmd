package model

import (
	"context"

	"github.com/pkg/errors"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
)

// ForwarderState describes state of forwarder
type ForwarderState int8

const (
	// ForwarderStateNone means there is no active connection in forwarder
	ForwarderStateNone ForwarderState = 0 // In case forwarder is not yet configured for connection

	// ForwarderStateReady means there is an active connection in forwarder
	ForwarderStateReady ForwarderState = 1 // In case forwarder is configured for connection.
)

// Forwarder structure in Model that describes forwarder
type Forwarder struct {
	RegisteredName       string
	SocketLocation       string
	MechanismsConfigured bool
}

// Clone returns pointer to copy of Forwarder
func (d *Forwarder) clone() cloneable {
	if d == nil {
		return nil
	}

	return &Forwarder{
		RegisteredName:       d.RegisteredName,
		SocketLocation:       d.SocketLocation,
		MechanismsConfigured: d.MechanismsConfigured,
	}
}

type forwarderDomain struct {
	baseDomain
}

func newForwarderDomain() forwarderDomain {
	return forwarderDomain{
		baseDomain: newBase(),
	}
}

func (d *forwarderDomain) AddForwarder(ctx context.Context, dp *Forwarder) {
	d.store(ctx, dp.RegisteredName, dp)
}

func (d *forwarderDomain) GetForwarder(name string) *Forwarder {
	v, _ := d.load(name)
	if v != nil {
		return v.(*Forwarder)
	}
	return nil
}

func (d *forwarderDomain) DeleteForwarder(ctx context.Context, name string) {
	d.delete(ctx, name)
}

func (d *forwarderDomain) UpdateForwarder(ctx context.Context, dp *Forwarder) {
	d.store(ctx, dp.RegisteredName, dp)
}

func (d *forwarderDomain) SelectForwarder(forwarderSelector func(dp *Forwarder) bool) (*Forwarder, error) {
	var rv *Forwarder
	d.kvRange(func(key string, value interface{}) bool {
		fwd := value.(*Forwarder)

		if forwarderSelector == nil || forwarderSelector(fwd) {
			rv = fwd
			return false
		}

		return true
	})

	if rv == nil {
		return nil, errors.New("no appropriate forwarders found")
	}

	return rv, nil
}

func (d *forwarderDomain) SetForwarderModificationHandler(h *ModificationHandler) func() {
	return d.addHandler(h)
}
