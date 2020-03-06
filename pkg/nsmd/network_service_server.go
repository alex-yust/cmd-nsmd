package nsmd

import (
	"github.com/networkservicemesh/api/pkg/api/networkservice"

	"github.com/networkservicemesh/cmd-nsmgr/pkg/api/nsm"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/common"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/local"
	"github.com/networkservicemesh/cmd-nsmgr/pkg/model"
)

// NewNetworkServiceServer - construct a local network service chain
func NewNetworkServiceServer(model model.Model, ws *Workspace,
	nsmManager nsm.NetworkServiceManager) networkservice.NetworkServiceServer {
	return common.NewCompositeService("Local",
		common.NewRequestValidator(),
		common.NewMonitorService(ws.MonitorConnectionServer()),
		local.NewWorkspaceService(ws.Name()),
		local.NewConnectionService(model),
		local.NewEndpointSelectorService(nsmManager.NseManager()),
		local.NewForwarderService(model, nsmManager.ServiceRegistry()),
		common.NewExcludedPrefixesService(),
		local.NewEndpointService(nsmManager.NseManager(), nsmManager.GetHealProperties(), nsmManager.Model()),
		common.NewCrossConnectService(),
	)
}
