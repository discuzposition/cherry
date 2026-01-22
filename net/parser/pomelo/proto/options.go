package pomeloProto

// Options Proto 解析配置选项
type Options struct {
	// ProtoFiles proto 文件路径列表
	ProtoFiles []string

	// ProtoDir proto 文件目录，会自动扫描目录下所有 .proto 文件
	ProtoDir string

	// Version 协议版本号
	Version int

	GlobalMessages bool

	// ServerRoutes 服务端路由映射
	// key: 路由名称 (如 "connector.entryHandler.entry")
	// value: 消息名称 (如 "EntryResponse")
	ServerRoutes map[string]string

	// ClientRoutes 客户端路由映射
	// key: 路由名称 (如 "connector.entryHandler.entry")
	// value: 消息名称 (如 "EntryRequest")
	ClientRoutes map[string]string
}

// DefaultOptions 默认配置
func DefaultOptions() Options {
	return Options{
		ProtoFiles:   make([]string, 0),
		ProtoDir:     "",
		Version:      1,
		GlobalMessages: false,
		ServerRoutes: make(map[string]string),
		ClientRoutes: make(map[string]string),
	}
}

// Validate 验证配置
func (o *Options) Validate() error {
	if o.ProtoDir == "" && len(o.ProtoFiles) == 0 {
		return nil // 没有配置 proto，不启用功能
	}
	return nil
}

// HasProtoConfig 检查是否配置了 proto
func (o *Options) HasProtoConfig() bool {
	return o.ProtoDir != "" || len(o.ProtoFiles) > 0
}

