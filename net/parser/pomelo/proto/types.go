package pomeloProto

// ProtoSchema Pomelo 风格的 Protobuf Schema 定义
type ProtoSchema struct {
	Version int                       `json:"version"`          // 协议版本号
	Server  map[string]MessageSchema  `json:"server,omitempty"` // 服务端消息协议（用于客户端解码）
	Client  map[string]MessageSchema  `json:"client,omitempty"` // 客户端消息协议（用于客户端编码）
}

// MessageSchema 消息 Schema 定义
// key: 字段名, value: 字段类型
type MessageSchema map[string]interface{}

// FieldType Pomelo 风格的字段类型
type FieldType string

const (
	// 基础类型
	TypeString  FieldType = "string"
	TypeBool    FieldType = "bool"
	TypeInt32   FieldType = "int32"
	TypeUInt32  FieldType = "uInt32"
	TypeSInt32  FieldType = "sInt32"
	TypeInt64   FieldType = "int64"
	TypeUInt64  FieldType = "uInt64"
	TypeSInt64  FieldType = "sInt64"
	TypeFloat   FieldType = "float"
	TypeDouble  FieldType = "double"
	TypeBytes   FieldType = "bytes"
	TypeMessage FieldType = "message" // 嵌套消息类型
)

// RouteMapping 路由到消息的映射配置
type RouteMapping struct {
	Route       string // 路由名称，如 "connector.entryHandler.entry"
	RequestMsg  string // 请求消息名称（客户端发送）
	ResponseMsg string // 响应消息名称（服务端返回）
}

// ProtoMessage 解析后的 Proto 消息定义
type ProtoMessage struct {
	Name   string                 // 消息名称
	Fields map[string]*ProtoField // 字段映射
}

// ProtoField Proto 字段定义
type ProtoField struct {
	Name     string    // 字段名称
	Type     FieldType // 字段类型
	Tag      int       // 字段标签号
	Repeated bool      // 是否为数组
	TypeName string    // 自定义类型名称（用于嵌套消息）
}

// protoTypeMapping Proto 类型到 Pomelo 类型的映射
var protoTypeMapping = map[string]FieldType{
	"string":   TypeString,
	"bool":     TypeBool,
	"int32":    TypeInt32,
	"uint32":   TypeUInt32,
	"sint32":   TypeSInt32,
	"int64":    TypeInt64,
	"uint64":   TypeUInt64,
	"sint64":   TypeSInt64,
	"float":    TypeFloat,
	"double":   TypeDouble,
	"bytes":    TypeBytes,
	"fixed32":  TypeUInt32,
	"fixed64":  TypeUInt64,
	"sfixed32": TypeInt32,
	"sfixed64": TypeInt64,
}

// GetPomeloType 获取 Pomelo 风格的类型名称
func GetPomeloType(protoType string) (FieldType, bool) {
	t, ok := protoTypeMapping[protoType]
	return t, ok
}

