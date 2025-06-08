package pipelines

// StageGroup 阶段组 - 用于并行模式的分组执行
type StageGroup []Stage

// StageGroups 多个阶段组 - 用于并行模式的多层执行
type StageGroups []StageGroup
