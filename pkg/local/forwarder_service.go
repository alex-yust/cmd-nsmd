// Copyright (c) 2019 Cisco and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package local

import (
	"context"
	"fmt"
	"github.com/networkservicemesh/api/pkg/api/registry"
	"strings"
	"time"

	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/srv6"

	"github.com/pkg/errors"

	"github.com/networkservicemesh/sdk/pkg/tools/spanhelper"

	"github.com/sirupsen/logrus"

	"github.com/golang/protobuf/ptypes/empty"

	"github.com/networkservicemesh/api/pkg/api/networkservice"

	"github.com/networkservicemesh/cmd-nsmgr/pkg/api/crossconnect"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/common"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/model"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/serviceregistry"
)

const (
	// ForwarderRetryCount - A number of times to call Forwarder Request, TODO: Remove after DP will be stable.
	ForwarderRetryCount = 10
	// ForwarderRetryDelay - a delay between operations.
	ForwarderRetryDelay = 500 * time.Millisecond
	// ForwarderTimeout - A forwarder timeout
	ForwarderTimeout = 15 * time.Second
	// ErrorCloseTimeout - timeout to close all stuff in case of error
	ErrorCloseTimeout = 15 * time.Second
	forwarderNamePrefix = "forwarder"
)

// forwarderService -
type forwarderService struct {
	serviceRegistry serviceregistry.ServiceRegistry
	model           model.Model
}

func isForwarderRequest(request *networkservice.NetworkServiceRequest) bool {
	if conn := request.GetConnection(); conn != nil {
		if path := conn.GetPath(); path != nil {
			if path.Index >= 0 && int(path.Index) < len(path.PathSegments) {
				return strings.HasPrefix(path.PathSegments[path.Index].Name, forwarderNamePrefix)
			}
		}
	}
	return false
}

func (cce *forwarderService) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	if isForwarderRequest(request) {
		// Forwarder is already selected, pass through
		return common.ProcessNext(ctx, request)
	}

	span := spanhelper.GetSpanHelper(ctx)
	logger := span.Logger()

	logger.Infof("Finding forwarder for request: %v", request)

	// TODO: RoundRobin selection of xNSE
	forwarderServiceName := fmt.Sprintf("%s@%s", forwarderNamePrefix, "localIP")
	xconnNSEs := cce.model.GetEndpointsByNetworkService(forwarderServiceName)
	if len(xconnNSEs) == 0 {
		return nil, errors.New("No forwarders found")
	}

	for _, fwdNSE := range xconnNSEs {
		logger.Infof("Forwarder candidate: %v", fwdNSE)
		endpoint := cce.model.GetEndpoint(fwdNSE.GetName())
		if endpoint == nil {
			logger.Errorf("Forwarder endpoint not found: %s", fwdNSE.GetName())
			continue
		}
		fwdClient, _, err := cce.serviceRegistry.EndpointConnection(ctx, endpoint)
		if err != nil {
			logger.Errorf("Failed to connect forwarde: %v", err)
			continue
		}
		conn, fwdErr := fwdClient.Request(ctx, request)
		if fwdErr != nil {
			logger.Errorf("failed to use forwarder %s: %v", fwdNSE.Name, fwdErr)
			continue
		}

		request.Connection = conn

		fwd := &model.Forwarder{
			RegisteredName:       endpoint.EndpointName(),
			SocketLocation:       endpoint.SocketLocation,
			MechanismsConfigured: false,
		}

		ctx = common.WithForwarder(ctx, fwd)
		ctx = common.WithRemoteMechanisms(ctx, cce.prepareRemoteMechanisms(request))
		return common.ProcessNext(ctx, request)
	}

	return nil, errors.Errorf("Failed to provide a valid forwarder for request: %v", request)
}

// prepareRemoteMechanisms fills mechanism properties
func (cce *forwarderService) prepareRemoteMechanisms(request *networkservice.NetworkServiceRequest) []*networkservice.Mechanism {
	m := request.Connection.Mechanism.Clone()
	switch m.GetType() {
	case srv6.MECHANISM:
		parameters := m.GetParameters()
		if parameters == nil {
			parameters = map[string]string{}
		}
		parameters[srv6.SrcBSID] = cce.serviceRegistry.SIDAllocator().SID(request.Connection.GetId())
		parameters[srv6.SrcLocalSID] = cce.serviceRegistry.SIDAllocator().SID(request.Connection.GetId())
		m.Parameters = parameters
	}

	return []*networkservice.Mechanism{m}
}

func (cce *forwarderService) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	cc := common.ModelConnection(ctx)
	logger := common.Log(ctx)
	empt, err := common.ProcessClose(ctx, conn)
	if closeErr := cce.performClose(ctx, cc, logger); closeErr != nil {
		logger.Errorf("Failed to close: %v", closeErr)
	}
	return empt, err
}

func (cce *forwarderService) performClose(ctx context.Context, cc *model.ClientConnection, logger logrus.FieldLogger) error {
	// Close endpoints, etc
	if cc.ForwarderState != model.ForwarderStateNone {
		logger.Info("NSM.Forwarder: Closing cross connection on forwarder...")
		fwd := cce.model.GetForwarder(cc.ForwarderRegisteredName)
		forwarderClient, conn, err := cce.serviceRegistry.ForwarderConnection(ctx, fwd)
		if err != nil {
			logger.Error(err)
			return err
		}
		if conn != nil {
			defer func() { _ = conn.Close() }()
		}
		if _, err := forwarderClient.Close(ctx, cc.Xcon); err != nil {
			logger.Error(err)
			return err
		}
		logger.Info("NSM.Forwarder: Cross connection successfully closed on forwarder")
		cc.ForwarderState = model.ForwarderStateNone
	}
	return nil
}

// NewForwarderService -  creates a service to program forwarder.
func NewForwarderService(model model.Model, serviceRegistry serviceregistry.ServiceRegistry) networkservice.NetworkServiceServer {
	return &forwarderService{
		model:           model,
		serviceRegistry: serviceRegistry,
	}
}
