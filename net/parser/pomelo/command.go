package pomelo

import (
	"time"

	cfacade "github.com/cherry-game/cherry/facade"
	clog "github.com/cherry-game/cherry/logger"
	pmessage "github.com/cherry-game/cherry/net/parser/pomelo/message"
	ppacket "github.com/cherry-game/cherry/net/parser/pomelo/packet"
	pproto "github.com/cherry-game/cherry/net/parser/pomelo/proto"
	jsoniter "github.com/json-iterator/go"
	"go.uber.org/zap/zapcore"
)

type (
	Command struct {
		writeBacklog           int
		sysData                map[string]interface{}
		heartbeatTime          time.Duration
		handshakeBytes         []byte                  // 完整握手响应（包含协议数据）
		handshakeBytesNoProtos []byte                  // 不含协议数据的握手响应（版本匹配时使用）
		heartbeatBytes         []byte
		onPacketFuncMap        map[ppacket.Type]PacketFunc
		onDataRouteFunc        DataRouteFunc
		protoOptions           *pproto.Options         // Proto 配置选项
		protoSchema            *pproto.ProtoSchema     // 解析后的 Proto Schema
	}

	// ClientHandshake 客户端握手数据结构
	ClientHandshake struct {
		Sys  ClientHandshakeSys     `json:"sys"`
		User map[string]interface{} `json:"user"`
	}

	// ClientHandshakeSys 客户端握手系统信息
	ClientHandshakeSys struct {
		Type         string                 `json:"type"`
		Version      string                 `json:"version"`
		ProtoVersion int                    `json:"protoVersion"`
		RSA          map[string]interface{} `json:"rsa"`
	}

	PacketFunc    func(agent *Agent, packet *ppacket.Packet)
	DataRouteFunc func(agent *Agent, route *pmessage.Route, msg *pmessage.Message)
)

const (
	DataHeartbeat  = "heartbeat"
	DataDict       = "dict"
	DataSerializer = "serializer"
	DataProtos     = "protos" // Protobuf Schema 数据
)

var (
	cmd = Command{
		writeBacklog:    64,
		sysData:         make(map[string]interface{}),
		heartbeatTime:   60 * time.Second,
		handshakeBytes:  make([]byte, 0),
		heartbeatBytes:  make([]byte, 0),
		onPacketFuncMap: make(map[ppacket.Type]PacketFunc, 4),
		onDataRouteFunc: DefaultDataRoute,
	}
)

func (p *Command) init(app cfacade.IApplication) {
	p.setData(DataHeartbeat, p.heartbeatTime.Seconds())
	p.setData(DataDict, pmessage.GetDictionary())
	p.setData(DataSerializer, app.Serializer().Name())

	// 解析并设置 Proto Schema
	p.parseAndSetProtos()

	p.setHandshakeBytes()
	p.setHeartbeatBytes()

	p.setOnPacketFunc()
}

// parseAndSetProtos 解析 proto 文件并设置到 sysData
func (p *Command) parseAndSetProtos() {
	if p.protoOptions == nil || !p.protoOptions.HasProtoConfig() {
		return
	}

	parser := pproto.NewParser(*p.protoOptions)
	schema, err := parser.Parse()
	if err != nil {
		clog.Errorf("[ProtoParser] 解析 proto 文件失败: %v", err)
		return
	}

	if schema != nil {
		p.protoSchema = schema
		p.setData(DataProtos, schema)
		clog.Infof("[ProtoParser] Proto Schema 加载成功, version=%d, server routes=%d, client routes=%d",
			schema.Version, len(schema.Server), len(schema.Client))
	}
}

func (p *Command) setData(name string, value interface{}) {
	if _, found := p.sysData[name]; !found {
		p.sysData[name] = value
	}
}

func (p *Command) setHandshakeBytes() {
	// 生成完整握手响应（包含协议数据）
	handshakeData := map[string]interface{}{
		"code": 200,
		"sys":  p.sysData,
	}

	handshakeBytes, err := jsoniter.Marshal(handshakeData)
	if err != nil {
		clog.Error(err)
		return
	}

	p.handshakeBytes, err = ppacket.Encode(ppacket.Handshake, handshakeBytes)
	if err != nil {
		clog.Error(err)
		return
	}

	clog.Infof("[initCommand] handshake data (with protos) = %v", handshakeData)

	// 生成不含协议数据的握手响应（版本匹配时使用，节省带宽）
	sysDataNoProtos := make(map[string]interface{})
	for k, v := range p.sysData {
		if k != DataProtos {
			sysDataNoProtos[k] = v
		}
	}

	handshakeDataNoProtos := map[string]interface{}{
		"code": 200,
		"sys":  sysDataNoProtos,
	}

	handshakeBytesNoProtos, err := jsoniter.Marshal(handshakeDataNoProtos)
	if err != nil {
		clog.Error(err)
		return
	}

	p.handshakeBytesNoProtos, err = ppacket.Encode(ppacket.Handshake, handshakeBytesNoProtos)
	if err != nil {
		clog.Error(err)
		return
	}

	clog.Infof("[initCommand] handshake data (no protos) = %v", handshakeDataNoProtos)
	clog.Infof("[initCommand] handshake bytes size: with protos=%d, without protos=%d",
		len(p.handshakeBytes), len(p.handshakeBytesNoProtos))
}

func (p *Command) setHeartbeatBytes() {
	heartbeatBytes, err := ppacket.Encode(ppacket.Heartbeat, nil)
	if err != nil {
		clog.Error(err)
		return
	}

	p.heartbeatBytes = heartbeatBytes
}

func (p *Command) setOnPacketFunc() {
	packetFuncMaps := map[ppacket.Type]PacketFunc{
		ppacket.Handshake:    handshakeCommand,
		ppacket.HandshakeAck: handshakeACKCommand,
		ppacket.Heartbeat:    heartbeatCommand,
		ppacket.Data:         dataCommand,
	}

	for name, packetFunc := range packetFuncMaps {
		_, found := p.onPacketFuncMap[name]
		if !found {
			p.onPacketFuncMap[name] = packetFunc
		}
	}
}

func handshakeCommand(agent *Agent, pkg *ppacket.Packet) {
	agent.SetState(AgentWaitAck)

	// 默认发送完整握手响应
	responseBytes := cmd.handshakeBytes

	// 尝试解析客户端握手数据，进行版本号校验
	if pkg != nil && len(pkg.Data()) > 0 {
		var clientHandshake ClientHandshake
		if err := jsoniter.Unmarshal(pkg.Data(), &clientHandshake); err == nil {
			clientProtoVersion := clientHandshake.Sys.ProtoVersion

			// 获取服务端协议版本号
			serverProtoVersion := 0
			if cmd.protoSchema != nil {
				serverProtoVersion = cmd.protoSchema.Version
			}

			// 版本号匹配且不为0时，不下发协议数据以节省带宽
			if clientProtoVersion > 0 && clientProtoVersion == serverProtoVersion {
				responseBytes = cmd.handshakeBytesNoProtos
				if clog.PrintLevel(zapcore.DebugLevel) {
					clog.Debugf("[sid = %s,uid = %d] Proto version matched (v%d), skip protos download. [address = %s]",
						agent.SID(),
						agent.UID(),
						clientProtoVersion,
						agent.RemoteAddr(),
					)
				}
			} else {
				if clog.PrintLevel(zapcore.DebugLevel) {
					clog.Debugf("[sid = %s,uid = %d] Proto version mismatch (client=%d, server=%d), sending full protos. [address = %s]",
						agent.SID(),
						agent.UID(),
						clientProtoVersion,
						serverProtoVersion,
						agent.RemoteAddr(),
					)
				}
			}
		}
	}

	agent.SendRaw(responseBytes)

	if clog.PrintLevel(zapcore.DebugLevel) {
		clog.Debugf("[sid = %s,uid = %d] Request handshake. [address = %s]",
			agent.SID(),
			agent.UID(),
			agent.RemoteAddr(),
		)
	}
}

func handshakeACKCommand(agent *Agent, _ *ppacket.Packet) {
	agent.SetState(AgentWorking)

	if clog.PrintLevel(zapcore.DebugLevel) {
		clog.Debugf("[sid = %s,uid = %d] request handshakeACK. [address = %s]",
			agent.SID(),
			agent.UID(),
			agent.RemoteAddr(),
		)
	}
}

func heartbeatCommand(agent *Agent, _ *ppacket.Packet) {
	agent.SendRaw(cmd.heartbeatBytes)
}

func dataCommand(agent *Agent, pkg *ppacket.Packet) {
	if agent.State() != AgentWorking {
		if clog.PrintLevel(zapcore.DebugLevel) {
			clog.Warnf("[sid = %s,uid = %d] Data State is not working. [state = %d]",
				agent.SID(),
				agent.UID(),
				agent.State(),
			)
		}
		return
	}

	msg, err := pmessage.Decode(pkg.Data())
	if err != nil {
		if clog.PrintLevel(zapcore.DebugLevel) {
			clog.Warnf("[sid = %s,uid = %d] Data message decode error. [data = %s, error = %s]",
				agent.SID(),
				agent.UID(),
				pkg.Data(),
				err,
			)
		}
		return
	}

	route, err := pmessage.DecodeRoute(msg.Route)
	if err != nil {
		if clog.PrintLevel(zapcore.DebugLevel) {
			clog.Warnf("[sid = %s,uid = %d] Data Message decode route error. [data = %s, error = %s]",
				agent.SID(),
				agent.UID(),
				pkg.Data(),
				err,
			)
		}
		return
	}

	cmd.onDataRouteFunc(agent, route, &msg)
}

// SetProtoOptions 设置 Proto 配置选项
// 必须在 pomelo Actor 初始化之前调用
func SetProtoOptions(opts pproto.Options) {
	cmd.protoOptions = &opts
}

// GetProtoSchema 获取当前的 Proto Schema
func GetProtoSchema() *pproto.ProtoSchema {
	return cmd.protoSchema
}

// SetProtos 直接设置 Proto Schema（用于手动配置）
func SetProtos(schema *pproto.ProtoSchema) {
	if schema != nil {
		cmd.protoSchema = schema
		cmd.setData(DataProtos, schema)
	}
}
