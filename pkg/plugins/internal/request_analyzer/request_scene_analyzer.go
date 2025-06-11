package request_analyzer

import (
	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/discovery"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

// SceneAnalyzerStage 基于推理场景识别的Stage
type SceneAnalyzerStage struct {
	*pipelines.BaseStage
}

func NewSceneAnalyzerStage(name string) *SceneAnalyzerStage {
	return &SceneAnalyzerStage{
		BaseStage: pipelines.NewBaseStage(name),
	}
}

func (stage *SceneAnalyzerStage) Process(rc *rcx.RequestContext) error {
	// 设置请求状态为预处理中
	rc.SetState(rcx.StatePreprocessing)

	serviceDiscovery := discovery.GetServiceDiscoveryDirectly()
	endpoints, err := serviceDiscovery.GetEndpoints(rc.Context())
	if err != nil {
		logger.Errorf("failed to get endpoints, err: %+v", err)
		return err
	}

	// 设置推理场景
	rc.Scene = stage.analyzeScene(endpoints)
	return nil
}

// analyzeScene 分析推理场景，核心业务逻辑
func (stage *SceneAnalyzerStage) analyzeScene(endpoints []*discovery.Endpoint) common.InferenceScene {
	endpointCount := len(endpoints)

	// 单实例场景
	if endpointCount == 1 {
		return common.SINGLE
	}

	// 多实例场景，需要进一步判断是否为PD分离
	var hasProducerOrConsumer bool

	// 预分配slice避免动态扩容
	roles := make([]discovery.KVRole, 0, endpointCount)

	for i := range endpoints {
		role := endpoints[i].KVRole
		roles = append(roles, role)

		// 一旦发现Producer或Consumer就可以提前退出
		if role == discovery.KVRoleProducer || role == discovery.KVRoleConsumer {
			hasProducerOrConsumer = true
			break
		}
	}

	// PD分离场景：存在Producer或Consumer角色
	if hasProducerOrConsumer {
		return common.PD_DISAGGREGATION
	}

	// 默认多实例场景
	return common.MULTI_INSTANCE
}
