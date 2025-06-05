package pipelines

// StageConfig 阶段配置
type StageConfig struct {
	Name    string                 `json:"name" yaml:"name"`
	Type    string                 `json:"type" yaml:"type"`
	Enabled bool                   `json:"enabled" yaml:"enabled"`
	Config  map[string]interface{} `json:"config" yaml:"config"`
	Plugin  string                 `json:"plugin" yaml:"plugin"`
}
