package pomeloProto

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	clog "github.com/cherry-game/cherry/logger"
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

			// 解析字段
			if matches := fieldRegex.FindStringSubmatch(line); matches != nil {
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
					field.TypeName = fieldType
				}

				currentMessage.Fields = append(currentMessage.Fields, field)
			}

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

// buildSchema 构建 Pomelo Schema（标准格式）
func (p *Parser) buildSchema() *ProtoSchema {
	schema := &ProtoSchema{
		Version: p.options.Version,
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

	return schema
}

// buildRouteSchema 构建单个路由的 Schema（标准 Pomelo 格式）
// 格式示例:
// {
//   "optional uInt32 code": 1,
//   "repeated message Hero heroes": 2,
//   "__messages__": {
//     "Hero": {
//       "optional int32 configId": 1,
//       "optional string name": 2
//     }
//   }
// }
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

// GetMessages 获取所有解析的消息
func (p *Parser) GetMessages() map[string]*ProtoMessage {
	return p.messages
}
