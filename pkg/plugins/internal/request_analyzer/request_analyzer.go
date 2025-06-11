package request_analyzer

import (
	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// RequestAnalyzer 请求分析器
type RequestAnalyzer struct {
	*plugins.BasePlugin

	// typeAnalyzerStage 请求类型分析器
	typeAnalyzerStage pipelines.Stage

	// sceneAnalyzerStage 场景识别分析器
	sceneAnalyzerStage pipelines.Stage
}

func NewRequestAnalyzer(pluginInfo *config.PluginInfo) *RequestAnalyzer {
	base := plugins.NewBasePlugin(pluginInfo)

	return &RequestAnalyzer{
		BasePlugin:         base,
		typeAnalyzerStage:  NewTypeAnalyzeStage("request_type_analyzer"),
		sceneAnalyzerStage: NewSceneAnalyzerStage("request_scene_analyzer"),
	}
}

// Shutdown 关闭阶段
func (s *RequestAnalyzer) Shutdown() error {
	s.typeAnalyzerStage = nil
	s.sceneAnalyzerStage = nil
	return s.BasePlugin.Shutdown(s.BasePlugin.GetContext())
}
