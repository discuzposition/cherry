package pomeloProto

// ProtoSchema Pomelo 标准 Protobuf Schema 定义
type ProtoSchema struct {
	Version int                      `json:"version"`          // 协议版本号
	Server  map[string]interface{}   `json:"server,omitempty"` // 服务端消息协议（用于客户端解码）
	Client  map[string]interface{}   `json:"client,omitempty"` // 客户端消息协议（用于客户端编码）
	Messages map[string]interface{}  `json:"__messages__,omitempty"`
}

// MessageSchema 消息 Schema 定义
// 原始 Pomelo 格式: key = "修饰符 类型 字段名", value = 标签号
// 例如: "optional uInt32 code": 1, "repeated message Item items": 2
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

// FieldModifier 字段修饰符
type FieldModifier string

const (
	ModifierRequired FieldModifier = "required"
	ModifierOptional FieldModifier = "optional"
	ModifierRepeated FieldModifier = "repeated"
)

// 特殊字段名
const (
	MessagesKey = "__messages__" // 嵌套消息定义的 key
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
	Fields []*ProtoField          // 字段列表（保持顺序）
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

// BuildFieldKey 构建原始 Pomelo 格式的字段 key
// 格式: "修饰符 类型 字段名"
// 例如: "optional uInt32 code", "repeated message Item items"
func BuildFieldKey(modifier FieldModifier, fieldType string, fieldName string) string {
	return string(modifier) + " " + fieldType + " " + fieldName
}
