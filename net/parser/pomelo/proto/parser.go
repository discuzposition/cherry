package pomeloProto

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	clog "github.com/cherry-game/cherry/logger"
	jsoniter "github.com/json-iterator/go"
)

// Parser Proto 文件解析器
type Parser struct {
	options  Options
	messages map[string]*ProtoMessage // 所有解析的消息定义
}

// NewParser 创建解析器
func NewParser(opts Options) *Parser {
	return &Parser{
		options:  opts,
		messages: make(map[string]*ProtoMessage),
	}
}

// Parse 解析 proto 文件并生成 Pomelo Schema
func (p *Parser) Parse() (*ProtoSchema, error) {
	if !p.options.HasProtoConfig() {
		return nil, nil
	}

	// 获取所有 proto 文件
	files, err := p.getProtoFiles()
	if err != nil {
		return nil, fmt.Errorf("获取 proto 文件失败: %w", err)
	}

	if len(files) == 0 {
		clog.Warn("[ProtoParser] 没有找到 proto 文件")
		return nil, nil
	}

	// 解析所有 proto 文件
	for _, file := range files {
		if err := p.parseFile(file); err != nil {
			clog.Warnf("[ProtoParser] 解析文件失败: %s, 错误: %v", file, err)
			continue
		}
	}

	// 生成 Pomelo Schema
	schema := p.buildSchema()
	return schema, nil
}

// getProtoFiles 获取所有 proto 文件路径
func (p *Parser) getProtoFiles() ([]string, error) {
	var files []string

	// 添加直接指定的文件
	files = append(files, p.options.ProtoFiles...)

	// 扫描目录
	if p.options.ProtoDir != "" {
		err := filepath.Walk(p.options.ProtoDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".proto") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return files, nil
}

// parseFile 解析单个 proto 文件
func (p *Parser) parseFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentMessage *ProtoMessage
	var braceCount int
	var inMessage bool

	// 正则表达式
	messageRegex := regexp.MustCompile(`^\s*message\s+(\w+)\s*\{?\s*$`)
	fieldRegex := regexp.MustCompile(`^\s*(repeated\s+)?(\w+)\s+(\w+)\s*=\s*(\d+)\s*;`)
	mapRegex := regexp.MustCompile(`^\s*map\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>\s+(\w+)\s*=\s*(\d+)\s*;`)
	openBraceRegex := regexp.MustCompile(`\{`)
	closeBraceRegex := regexp.MustCompile(`\}`)

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过注释和空行
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "//") {
			continue
		}

		// 检查 message 开始
		if matches := messageRegex.FindStringSubmatch(line); matches != nil {
			messageName := matches[1]
			currentMessage = &ProtoMessage{
				Name:   messageName,
				Fields: make([]*ProtoField, 0),
			}
			inMessage = true
			braceCount = strings.Count(line, "{") - strings.Count(line, "}")
			if braceCount == 0 {
				braceCount = 1 // message 定义后 { 可能在下一行
			}
			continue
		}

		// 在 message 内部
		if inMessage && currentMessage != nil {
			// 计算大括号
			braceCount += len(openBraceRegex.FindAllString(line, -1))
			braceCount -= len(closeBraceRegex.FindAllString(line, -1))

			// 解析 map 字段: map<keyType, valueType> fieldName = tag;
			if matches := mapRegex.FindStringSubmatch(line); matches != nil {
				keyTypeRaw := matches[1]
				valueTypeRaw := matches[2]
				fieldName := matches[3]
				tag, _ := strconv.Atoi(matches[4])

				keyType := normalizeTypeName(keyTypeRaw)
				valueType := normalizeTypeName(valueTypeRaw)

				// 生成 map entry message
				// 注意：该名字不会出现在 wire 上，只要 schema 内一致即可
				entryMsgName := currentMessage.Name + "_" + fieldName + "Entry"

				// 构建 entry 消息
				entryMsg := &ProtoMessage{
					Name:   entryMsgName,
					Fields: make([]*ProtoField, 0, 2),
				}

				// key 字段（tag=1）
				keyField := &ProtoField{
					Name:     "key",
					Tag:      1,
					Repeated: false,
				}
				if pomeloType, ok := GetPomeloType(keyType); ok {
					keyField.Type = pomeloType
				} else {
					// map 的 key 必须是标量类型；如果解析失败，退化为 string
					clog.Warnf("[ProtoParser] map key 类型不支持，已退化为 string: %s (field=%s.%s)", keyTypeRaw, currentMessage.Name, fieldName)
					keyField.Type = TypeString
				}
				entryMsg.Fields = append(entryMsg.Fields, keyField)

				// value 字段（tag=2）
				valueField := &ProtoField{
					Name:     "value",
					Tag:      2,
					Repeated: false,
				}
				if pomeloType, ok := GetPomeloType(valueType); ok {
					valueField.Type = pomeloType
				} else {
					valueField.Type = TypeMessage
					valueField.TypeName = valueType
				}
				entryMsg.Fields = append(entryMsg.Fields, valueField)

				// 注册 entry message
				if _, exists := p.messages[entryMsgName]; !exists {
					p.messages[entryMsgName] = entryMsg
				}

				// 当前 message 添加 map 字段：在 wire 上表现为 repeated message Entry
				mapField := &ProtoField{
					Name:     fieldName,
					Tag:      tag,
					Repeated: true,
					Type:     TypeMessage,
					TypeName: entryMsgName,
				}
				currentMessage.Fields = append(currentMessage.Fields, mapField)
			} else if matches := fieldRegex.FindStringSubmatch(line); matches != nil {
				// 解析普通字段
				repeated := strings.TrimSpace(matches[1]) == "repeated"
				fieldType := matches[2]
				fieldName := matches[3]
				tag, _ := strconv.Atoi(matches[4])

				field := &ProtoField{
					Name:     fieldName,
					Tag:      tag,
					Repeated: repeated,
				}

				// 判断类型
				if pomeloType, ok := GetPomeloType(fieldType); ok {
					field.Type = pomeloType
				} else {
					// 自定义消息类型
					field.Type = TypeMessage
					field.TypeName = normalizeTypeName(fieldType)
				}

				currentMessage.Fields = append(currentMessage.Fields, field)
			}

			// 解析字段
			// message 结束
			if braceCount <= 0 {
				p.messages[currentMessage.Name] = currentMessage
				currentMessage = nil
				inMessage = false
			}
		}
	}

	return scanner.Err()
}

// normalizeTypeName 将带包名的类型引用简化为最后一段（例如 foo.bar.Baz -> Baz）
func normalizeTypeName(t string) string {
	if strings.Contains(t, ".") {
		parts := strings.Split(t, ".")
		return parts[len(parts)-1]
	}
	return t
}

// buildSchema 构建 Pomelo Schema（标准格式）
func (p *Parser) buildSchema() *ProtoSchema {
	schema := &ProtoSchema{
		Version: 0, // 先设为0，后面根据内容计算
		Server:  make(map[string]interface{}),
		Client:  make(map[string]interface{}),
	}

	// 构建服务端路由 Schema
	for route, msgName := range p.options.ServerRoutes {
		if msg, ok := p.messages[msgName]; ok {
			schema.Server[route] = p.buildRouteSchema(msg)
		} else {
			clog.Warnf("[ProtoParser] 服务端路由消息未找到: route=%s, message=%s", route, msgName)
		}
	}

	// 构建客户端路由 Schema
	for route, msgName := range p.options.ClientRoutes {
		if msg, ok := p.messages[msgName]; ok {
			schema.Client[route] = p.buildRouteSchema(msg)
		} else {
			clog.Warnf("[ProtoParser] 客户端路由消息未找到: route=%s, message=%s", route, msgName)
		}
	}

	if p.options.GlobalMessages {
		globalMessages := make(map[string]interface{})
		p.collectGlobalMessages(schema.Server, globalMessages)
		p.collectGlobalMessages(schema.Client, globalMessages)
		if len(globalMessages) > 0 {
			schema.Messages = globalMessages
		}
	}

	// 计算基于内容的版本号
	// 如果配置了手动版本号且大于0，则使用手动版本号
	// 否则基于 schema 内容计算 hash 作为版本号
	if p.options.Version > 0 {
		schema.Version = p.options.Version
	} else {
		schema.Version = p.calculateSchemaVersion(schema)
	}

	return schema
}

// calculateSchemaVersion 基于 schema 内容计算版本号
// 使用 CRC32 hash，确保相同内容生成相同版本号
func (p *Parser) calculateSchemaVersion(schema *ProtoSchema) int {
	// 创建一个临时结构用于计算 hash（不包含 version 字段）
	hashData := struct {
		Server   map[string]interface{} `json:"server"`
		Client   map[string]interface{} `json:"client"`
		Messages map[string]interface{} `json:"__messages__,omitempty"`
	}{
		Server:   schema.Server,
		Client:   schema.Client,
		Messages: schema.Messages,
	}

	// 序列化为 JSON（使用排序的 key 确保一致性）
	jsonBytes, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(hashData)
	if err != nil {
		clog.Warnf("[ProtoParser] 计算版本号失败，使用默认版本号 1: %v", err)
		return 1
	}

	// 使用 CRC32 计算 hash，转为正整数
	hash := crc32.ChecksumIEEE(jsonBytes)
	version := int(hash & 0x7FFFFFFF) // 确保为正整数

	clog.Infof("[ProtoParser] 基于 schema 内容计算版本号: %d (hash=0x%08X)", version, hash)
	return version
}

// buildRouteSchema 构建单个路由的 Schema（标准 Pomelo 格式）
// 格式示例:
//
//	{
//	  "optional uInt32 code": 1,
//	  "repeated message Hero heroes": 2,
//	  "__messages__": {
//	    "Hero": {
//	      "optional int32 configId": 1,
//	      "optional string name": 2
//	    }
//	  }
//	}
func (p *Parser) buildRouteSchema(msg *ProtoMessage) map[string]interface{} {
	result := make(map[string]interface{})
	nestedMessages := make(map[string]interface{})

	// 按标签号排序字段
	sortedFields := make([]*ProtoField, len(msg.Fields))
	copy(sortedFields, msg.Fields)
	sort.Slice(sortedFields, func(i, j int) bool {
		return sortedFields[i].Tag < sortedFields[j].Tag
	})

	// 处理每个字段
	for _, field := range sortedFields {
		fieldKey := p.buildFieldKey(field)
		result[fieldKey] = field.Tag

		// 如果是嵌套消息类型，递归收集嵌套消息定义
		if field.Type == TypeMessage {
			p.collectNestedMessages(field.TypeName, nestedMessages)
		}
	}

	// 如果有嵌套消息，添加 __messages__ 字段
	if len(nestedMessages) > 0 {
		result[MessagesKey] = nestedMessages
	}

	return result
}

// buildFieldKey 构建字段的 key
// 格式: "修饰符 类型 字段名"
func (p *Parser) buildFieldKey(field *ProtoField) string {
	var modifier FieldModifier
	var typeStr string

	// 确定修饰符
	if field.Repeated {
		modifier = ModifierRepeated
	} else {
		modifier = ModifierOptional
	}

	// 确定类型字符串
	if field.Type == TypeMessage {
		// 嵌套消息类型使用原始类型名
		typeStr = "message " + field.TypeName
	} else {
		typeStr = string(field.Type)
	}

	return string(modifier) + " " + typeStr + " " + field.Name
}

// collectNestedMessages 递归收集嵌套消息定义
func (p *Parser) collectNestedMessages(msgName string, collected map[string]interface{}) {
	// 避免重复收集
	if _, exists := collected[msgName]; exists {
		return
	}

	msg, ok := p.messages[msgName]
	if !ok {
		return
	}

	// 构建该消息的 schema
	msgSchema := make(map[string]interface{})

	// 按标签号排序字段
	sortedFields := make([]*ProtoField, len(msg.Fields))
	copy(sortedFields, msg.Fields)
	sort.Slice(sortedFields, func(i, j int) bool {
		return sortedFields[i].Tag < sortedFields[j].Tag
	})

	for _, field := range sortedFields {
		fieldKey := p.buildFieldKey(field)
		msgSchema[fieldKey] = field.Tag

		// 递归收集嵌套消息
		if field.Type == TypeMessage {
			p.collectNestedMessages(field.TypeName, collected)
		}
	}

	collected[msgName] = msgSchema
}

func (p *Parser) collectGlobalMessages(routes map[string]interface{}, global map[string]interface{}) {
	for _, routeSchema := range routes {
		schemaMap, ok := routeSchema.(map[string]interface{})
		if !ok {
			continue
		}
		msgsRaw, found := schemaMap[MessagesKey]
		if !found {
			continue
		}
		msgsMap, ok := msgsRaw.(map[string]interface{})
		if !ok {
			delete(schemaMap, MessagesKey)
			continue
		}
		for name, msgSchema := range msgsMap {
			if existing, exists := global[name]; exists {
				if !reflect.DeepEqual(existing, msgSchema) {
					clog.Warnf("[ProtoParser] 全局消息冲突: %s", name)
				}
				continue
			}
			global[name] = msgSchema
		}
		delete(schemaMap, MessagesKey)
	}
}

// GetMessages 获取所有解析的消息
func (p *Parser) GetMessages() map[string]*ProtoMessage {
	return p.messages
}
