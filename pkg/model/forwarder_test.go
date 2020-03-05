package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/memif"

	. "github.com/onsi/gomega"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
)

func TestAddAndGetFwd(t *testing.T) {
	g := NewWithT(t)

	fwd := &Forwarder{
		RegisteredName: "fwd1",
		SocketLocation: "/socket",
		LocalMechanisms: []*networkservice.Mechanism{
			&networkservice.Mechanism{
				Type: memif.MECHANISM,
				Parameters: map[string]string{
					"localParam": "value",
				},
			},
		},
		RemoteMechanisms: []*networkservice.Mechanism{
			&networkservice.Mechanism{
				Type: "gre",
				Parameters: map[string]string{
					"remoteParam": "value",
				},
			},
		},
		MechanismsConfigured: true,
	}

	dd := newForwarderDomain()
	dd.AddForwarder(context.Background(), fwd)
	getFwd := dd.GetForwarder("fwd1")

	g.Expect(getFwd.RegisteredName).To(Equal(fwd.RegisteredName))
	g.Expect(getFwd.SocketLocation).To(Equal(fwd.SocketLocation))
	g.Expect(getFwd.MechanismsConfigured).To(Equal(fwd.MechanismsConfigured))
	g.Expect(getFwd.LocalMechanisms).To(Equal(fwd.LocalMechanisms))
	g.Expect(getFwd.RemoteMechanisms).To(Equal(fwd.RemoteMechanisms))

	g.Expect(fmt.Sprintf("%p", getFwd.LocalMechanisms)).ToNot(Equal(fmt.Sprintf("%p", fwd.LocalMechanisms)))
	g.Expect(fmt.Sprintf("%p", getFwd.RemoteMechanisms)).ToNot(Equal(fmt.Sprintf("%p", fwd.RemoteMechanisms)))
}

func TestDeleteFwd(t *testing.T) {
	g := NewWithT(t)

	dd := newForwarderDomain()
	dd.AddForwarder(context.Background(), &Forwarder{
		RegisteredName: "fwd1",
		SocketLocation: "/socket",
		LocalMechanisms: []*networkservice.Mechanism{
			&networkservice.Mechanism{
				Type: memif.MECHANISM,
				Parameters: map[string]string{
					"localParam": "value",
				},
			},
		},
		RemoteMechanisms: []*networkservice.Mechanism{
			&networkservice.Mechanism{
				Type: "gre",
				Parameters: map[string]string{
					"remoteParam": "value",
				},
			},
		},
		MechanismsConfigured: true,
	})

	cc := dd.GetForwarder("fwd1")
	g.Expect(cc).ToNot(BeNil())

	dd.DeleteForwarder(context.Background(), "fwd1")

	fwdDel := dd.GetForwarder("fwd1")
	g.Expect(fwdDel).To(BeNil())

	dd.DeleteForwarder(context.Background(), "NotExistingId")
}

func TestSelectFwd(t *testing.T) {
	g := NewWithT(t)

	amount := 5
	dd := newForwarderDomain()
	for i := 0; i < amount; i++ {
		dd.AddForwarder(context.Background(), &Forwarder{
			RegisteredName: fmt.Sprintf("fwd%d", i),
			SocketLocation: fmt.Sprintf("/socket-%d", i),
			LocalMechanisms: []*networkservice.Mechanism{
				&networkservice.Mechanism{
					Type: memif.MECHANISM,
					Parameters: map[string]string{
						"localParam": "value",
					},
				},
			},
			RemoteMechanisms: []*networkservice.Mechanism{
				&networkservice.Mechanism{
					Type: "gre",
					Parameters: map[string]string{
						"remoteParam": "value",
					},
				},
			},
			MechanismsConfigured: true,
		})
	}

	selector := func(fwd *Forwarder) bool {
		return fwd.SocketLocation == "/socket-4"
	}

	selectedFwd, err := dd.SelectForwarder(selector)
	g.Expect(err).To(BeNil())
	g.Expect(selectedFwd.RegisteredName).To(Equal("fwd4"))

	emptySelector := func(fwd *Forwarder) bool {
		return false
	}
	selectedFwd, err = dd.SelectForwarder(emptySelector)
	g.Expect(err.Error()).To(ContainSubstring("no appropriate forwarders found"))
	g.Expect(selectedFwd).To(BeNil())

	first, err := dd.SelectForwarder(nil)
	g.Expect(err).To(BeNil())
	g.Expect(first.RegisteredName).ToNot(BeNil())
}
