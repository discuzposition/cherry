package pomeloProto

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
				Fields: make(map[string]*ProtoField),
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

				currentMessage.Fields[fieldName] = field
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

// buildSchema 构建 Pomelo Schema
func (p *Parser) buildSchema() *ProtoSchema {
	schema := &ProtoSchema{
		Version: p.options.Version,
		Server:  make(map[string]MessageSchema),
		Client:  make(map[string]MessageSchema),
	}

	// 构建服务端路由 Schema
	for route, msgName := range p.options.ServerRoutes {
		if msg, ok := p.messages[msgName]; ok {
			schema.Server[route] = p.buildMessageSchema(msg)
		} else {
			clog.Warnf("[ProtoParser] 服务端路由消息未找到: route=%s, message=%s", route, msgName)
		}
	}

	// 构建客户端路由 Schema
	for route, msgName := range p.options.ClientRoutes {
		if msg, ok := p.messages[msgName]; ok {
			schema.Client[route] = p.buildMessageSchema(msg)
		} else {
			clog.Warnf("[ProtoParser] 客户端路由消息未找到: route=%s, message=%s", route, msgName)
		}
	}

	return schema
}

// buildMessageSchema 构建消息 Schema
func (p *Parser) buildMessageSchema(msg *ProtoMessage) MessageSchema {
	schema := make(MessageSchema)

	for fieldName, field := range msg.Fields {
		if field.Repeated {
			// 数组类型
			if field.Type == TypeMessage {
				// 嵌套消息数组
				if nestedMsg, ok := p.messages[field.TypeName]; ok {
					schema[fieldName] = []interface{}{p.buildMessageSchema(nestedMsg)}
				} else {
					schema[fieldName] = []interface{}{map[string]interface{}{}}
				}
			} else {
				// 基础类型数组
				schema[fieldName] = []interface{}{string(field.Type)}
			}
		} else {
			// 非数组类型
			if field.Type == TypeMessage {
				// 嵌套消息
				if nestedMsg, ok := p.messages[field.TypeName]; ok {
					schema[fieldName] = p.buildMessageSchema(nestedMsg)
				} else {
					schema[fieldName] = map[string]interface{}{}
				}
			} else {
				// 基础类型
				schema[fieldName] = string(field.Type)
			}
		}
	}

	return schema
}

// GetMessages 获取所有解析的消息
func (p *Parser) GetMessages() map[string]*ProtoMessage {
	return p.messages
}

