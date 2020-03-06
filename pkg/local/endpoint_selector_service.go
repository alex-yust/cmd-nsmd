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

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/api/pkg/api/networkservice"

	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/api/nsm"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/common"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/model"
	"github.com/networkservicemesh/sdk/pkg/tools/spanhelper"
)

// ConnectionService makes basic Mechanism selection for the incoming connection
type endpointSelectorService struct {
	nseManager nsm.NetworkServiceEndpointManager
}

func (es *endpointSelectorService) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	logger := common.Log(ctx)
	span := spanhelper.GetSpanHelper(ctx)

	clientConnection := common.ModelConnection(ctx)
	if clientConnection == nil {
		return nil, errors.Errorf("client connection need to be passed")
	}

	// 4. Check if Heal/Update if we need to ask remote NSM or it is a just local mechanism change requested.
	// true if we detect we need to request NSE to upgrade/update connection.
	// 4.1 New Network service is requested, we need to close current connection and do re-request of NSE.
	requestNSEOnUpdate := es.checkNSEUpdateIsRequired(ctx, clientConnection, request, logger)
	span.LogObject("requestNSEOnUpdate", requestNSEOnUpdate)

	// 7. do a Request() on NSE and select it.
	if clientConnection.ConnectionState == model.ClientConnectionHealing && !requestNSEOnUpdate {
		return es.checkUpdateConnectionContext(ctx, request, clientConnection)
	}

	// 7.1 try find NSE and do a Request to it.
	var lastError error
	ignoreEndpoints := common.IgnoredEndpoints(ctx)
	parentCtx := ctx
	attempt := 0
	for {
		attempt++
		span := spanhelper.FromContext(parentCtx, fmt.Sprintf("select-nse-%v", attempt))

		logger := span.Logger()
		ctx = common.WithLog(span.Context(), logger)

		// 7.1.1 Clone Connection to support iteration via EndPoints
		newRequest, endpoint, err := es.prepareRequest(ctx, request, clientConnection, ignoreEndpoints)
		if err != nil {
			span.Finish()
			return es.combineErrors(span, lastError, err)
		}
		if err = es.checkTimeout(parentCtx, span); err != nil {
			span.Finish()
			return nil, err
		}

		// 7.1.7 perform request to NSE/remote NSMD/NSE
		ctx = common.WithEndpoint(ctx, endpoint)
		// Perform passing execution to next chain element.
		conn, err := common.ProcessNext(ctx, newRequest)

		// 7.1.8 in case of error we put NSE into ignored list to check another one.
		if err != nil {
			logger.Errorf("NSM:(7.1.8) NSE respond with error: %v ", err)
			lastError = err
			ignoreEndpoints[endpoint.GetEndpointNSMName()] = endpoint
			span.Finish()
			continue
		}
		// We could put endpoint to clientConnection.
		clientConnection.Endpoint = endpoint
		if !es.nseManager.IsLocalEndpoint(endpoint) {
			clientConnection.RemoteNsm = endpoint.GetNetworkServiceManager()
		}
		// 7.1.9 We are fine with NSE connection and could continue.
		span.Finish()
		return conn, nil
	}
}

func (es *endpointSelectorService) combineErrors(span spanhelper.SpanHelper, err, lastError error) (*networkservice.Connection, error) {
	if lastError != nil {
		span.LogError(lastError)
		return nil, errors.Errorf("NSM:(7.1.5) %v. Last NSE Error: %v", err, lastError)
	}
	return nil, err
}

func (es *endpointSelectorService) selectEndpoint(ctx context.Context, clientConnection *model.ClientConnection, ignoreEndpoints map[registry.EndpointNSMName]*registry.NSERegistration, nseConn *networkservice.Connection) (*registry.NSERegistration, error) {
	var endpoint *registry.NSERegistration
	if clientConnection.ConnectionState == model.ClientConnectionHealing {
		// 7.1.2 Check previous endpoint, and it we will be able to contact it, it should be fine.
		endpointName := clientConnection.Endpoint.GetEndpointNSMName()
		if clientConnection.Endpoint != nil && ignoreEndpoints[endpointName] == nil {
			endpoint = clientConnection.Endpoint
		} else {
			// Ignored, we need to update DSTid.
			clientConnection.Xcon.Destination.Id = "-"
		}
		//TODO: Add check if endpoint are in registry or not.
	}
	// 7.1.3 Check if endpoint is not ignored yet
	if endpoint == nil {
		// 7.1.4 Choose a new endpoint
		return es.nseManager.GetEndpoint(ctx, nseConn, ignoreEndpoints)
	}
	return endpoint, nil
}

func (es *endpointSelectorService) checkNSEUpdateIsRequired(ctx context.Context, clientConnection *model.ClientConnection, request *networkservice.NetworkServiceRequest, logger logrus.FieldLogger) bool {
	requestNSEOnUpdate := false
	if clientConnection.ConnectionState == model.ClientConnectionHealing {
		if request.Connection.GetNetworkService() != clientConnection.GetNetworkService() {
			requestNSEOnUpdate = true

			// Just close, since client connection already passed with context.
			// Network service is closing, we need to close remote NSM and re-program local one.
			if _, err := common.ProcessClose(ctx, request.GetConnection()); err != nil {
				logger.Errorf("NSM:(4.1) Error during close of NSE during Request.Upgrade %v Existing connection: %v error %v", request, clientConnection, err)
			}
		} else {
			fwd := common.Forwarder(ctx)
			// 4.2 Check if NSE is still required, if some more context requests are different.
			requestNSEOnUpdate = es.checkNeedNSERequest(logger, request, clientConnection, fwd)
			if requestNSEOnUpdate {
				logger.Infof("Context is different, NSE request is required")
			}
		}
	}
	return requestNSEOnUpdate
}

func (es *endpointSelectorService) validateConnection(_ context.Context, conn *networkservice.Connection) error {
	return conn.IsComplete()
}

func (es *endpointSelectorService) updateConnectionContext(ctx context.Context, source, destination *networkservice.Connection) error {
	if err := es.validateConnection(ctx, destination); err != nil {
		return err
	}

	if err := source.UpdateContext(destination.GetContext()); err != nil {
		return err
	}

	return nil
}

/**
check if we need to do a NSE/Remote NSM request in case of our connection Upgrade/Healing procedure.
*/
func (es *endpointSelectorService) checkNeedNSERequest(logger logrus.FieldLogger, request *networkservice.NetworkServiceRequest, existingCC *model.ClientConnection, fwd *model.Forwarder) bool {
	// 4.2.x
	// 4.2.1 Check if context is changed, if changed we need to
	if !proto.Equal(request.GetConnection().GetContext(), existingCC.GetConnectionSource().GetContext()) {
		return true
	}
	// We need to check, fwd has mechanism changes in our Remote connection selected mechanism.

	if dst := existingCC.GetConnectionDestination(); dst.IsRemote() {
		dstM := dst.GetMechanism()

		// 4.2.2 Let's check if remote destination is matches our forwarder destination.
		if reqM := es.findMechanism(request.GetMechanismPreferences(), dstM.GetType()); reqM != nil {
			// 4.2.3 We need to check if source mechanism type and source parameters are different
			for k, v := range reqM.GetParameters() {
				rmV := dstM.GetParameters()[k]
				if v != rmV {
					logger.Infof("NSM:(4.2.3) Remote mechanism parameter %s was different with previous one : %v  %v", k, rmV, v)
					return true
				}
			}
			if !reqM.Equals(dstM) {
				logger.Infof("NSM:(4.2.4)  Remote mechanism was different with previous selected one : %v  %v", dstM, reqM)
				return true
			}
		} else {
			logger.Infof("NSM:(4.2.5) Remote mechanism previously selected was not found: %v  in forwarder %v", dstM, request.GetMechanismPreferences())
			return true
		}
	}

	return false
}

func (es *endpointSelectorService) findMechanism(mechanismPreferences []*networkservice.Mechanism, mechanismType string) *networkservice.Mechanism {
	for _, m := range mechanismPreferences {
		if m.GetType() == mechanismType {
			return m
		}
	}
	return nil
}

func (es *endpointSelectorService) Close(ctx context.Context, connection *networkservice.Connection) (*empty.Empty, error) {
	return common.ProcessClose(ctx, connection)
}

func (es *endpointSelectorService) checkUpdateConnectionContext(ctx context.Context, request *networkservice.NetworkServiceRequest, clientConnection *model.ClientConnection) (*networkservice.Connection, error) {
	// We do not need to do request to endpoint and just need to update all stuff.
	// 7.2 We do not need to access NSE, since all parameters are same.
	logger := common.Log(ctx)
	clientConnection.Xcon.Source.Mechanism = request.Connection.GetMechanism()
	clientConnection.Xcon.Source.State = networkservice.State_UP

	// 7.3 Destination context probably has been changed, so we need to update source context.
	if err := es.updateConnectionContext(ctx, request.GetConnection(), clientConnection.GetConnectionDestination()); err != nil {
		err = errors.Errorf("NSM:(7.3) Failed to update source connection context: %v", err)

		// Just close since client connection is already passed with context
		if _, closeErr := common.ProcessClose(ctx, request.GetConnection()); closeErr != nil {
			logger.Errorf("Failed to perform close: %v", closeErr)
		}
		return nil, err
	}

	if !es.nseManager.IsLocalEndpoint(clientConnection.Endpoint) {
		clientConnection.RemoteNsm = clientConnection.Endpoint.GetNetworkServiceManager()
	}
	return request.Connection, nil
}

func (es *endpointSelectorService) prepareRequest(ctx context.Context, request *networkservice.NetworkServiceRequest, clientConnection *model.ClientConnection, ignoreEndpoints map[registry.EndpointNSMName]*registry.NSERegistration) (*networkservice.NetworkServiceRequest, *registry.NSERegistration, error) {
	newRequest := request.Clone()
	nseConn := newRequest.Connection
	span := spanhelper.GetSpanHelper(ctx)

	endpoint, err := es.selectEndpoint(ctx, clientConnection, ignoreEndpoints, nseConn)
	if err != nil {
		return nil, nil, err
	}

	span.LogObject("selected endpoint", endpoint)
	if nseConn.GetContext() == nil {
		nseConn.Context = &networkservice.ConnectionContext{}
	}

	newRequest.Connection = nseConn
	return newRequest, endpoint, nil
}

func (es *endpointSelectorService) checkTimeout(ctx context.Context, span spanhelper.SpanHelper) error {
	if ctx.Err() != nil {
		newErr := errors.Errorf("NSM:(7.1.0) Context timeout, during find/call NSE... %v", ctx.Err())
		span.LogError(newErr)
		return newErr
	}
	return nil
}

// NewEndpointSelectorService - creates a service to select endpoint
func NewEndpointSelectorService(nseManager nsm.NetworkServiceEndpointManager) networkservice.NetworkServiceServer {
	return &endpointSelectorService{
		nseManager: nseManager,
	}
}
