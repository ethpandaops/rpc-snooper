package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/builtin"
	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type ModuleManager struct {
	modules        map[uint64]types.Module
	connections    map[*websocket.Conn]*ConnectionManager
	filters        map[uint64]*types.FilterConfig
	moduleCounter  uint64
	requestCounter uint64
	mu             sync.RWMutex
	enabled        bool
}

type ConnectionManager struct {
	conn            *websocket.Conn
	manager         *ModuleManager
	pendingRequests map[uint64]chan *protocol.WSMessageWithBinary
	modules         []uint64
	mu              sync.RWMutex
	done            chan struct{}
	writeMu         sync.Mutex
	closed          bool
	closeMu         sync.Mutex
}

type Manager struct {
	*ModuleManager
	logger       logrus.FieldLogger
	upgrader     websocket.Upgrader
	filterEngine *FilterEngine
}

func NewModuleManager() *ModuleManager {
	return &ModuleManager{
		modules:     make(map[uint64]types.Module),
		connections: make(map[*websocket.Conn]*ConnectionManager),
		filters:     make(map[uint64]*types.FilterConfig),
		enabled:     true,
	}
}

func NewManager(logger logrus.FieldLogger) *Manager {
	return &Manager{
		ModuleManager: NewModuleManager(),
		logger:        logger,
		filterEngine:  NewFilterEngine(logger),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

// Manager methods that delegate to ModuleManager with filterEngine
func (m *Manager) ProcessRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	if !m.ModuleManager.IsEnabled() {
		return ctx, nil
	}

	m.ModuleManager.mu.RLock()
	modules := make([]types.Module, 0, len(m.ModuleManager.modules))
	for _, module := range m.ModuleManager.modules {
		modules = append(modules, module)
	}
	m.ModuleManager.mu.RUnlock()

	for _, module := range modules {
		if m.ModuleManager.shouldProcessRequest(module, ctx, m.filterEngine) {
			newCtx, err := module.OnRequest(ctx)
			if err != nil {
				return ctx, err
			}
			if newCtx != nil {
				ctx = newCtx
			}
		}
	}

	return ctx, nil
}

func (m *Manager) ProcessResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	if !m.ModuleManager.IsEnabled() {
		return ctx, nil
	}

	m.ModuleManager.mu.RLock()
	modules := make([]types.Module, 0, len(m.ModuleManager.modules))
	for _, module := range m.ModuleManager.modules {
		modules = append(modules, module)
	}
	m.ModuleManager.mu.RUnlock()

	for _, module := range modules {
		if m.ModuleManager.shouldProcessResponse(module, ctx, m.filterEngine) {
			newCtx, err := module.OnResponse(ctx)
			if err != nil {
				return ctx, err
			}
			if newCtx != nil {
				ctx = newCtx
			}
		}
	}

	return ctx, nil
}

func (cm *ConnectionManager) WaitForResponse(requestID uint64) (*protocol.WSMessageWithBinary, error) {
	responseChan := make(chan *protocol.WSMessageWithBinary, 1)
	cm.RegisterPendingRequest(requestID, responseChan)
	defer cm.UnregisterPendingRequest(requestID)

	select {
	case response := <-responseChan:
		return response, nil
	case <-cm.done:
		return nil, context.Canceled
	}
}

func (cm *ConnectionManager) GenerateRequestID() uint64 {
	return atomic.AddUint64(&cm.manager.requestCounter, 1)
}

func (cm *ConnectionManager) RegisterPendingRequest(requestID uint64, responseChan chan *protocol.WSMessageWithBinary) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.pendingRequests == nil {
		cm.pendingRequests = make(map[uint64]chan *protocol.WSMessageWithBinary)
	}
	cm.pendingRequests[requestID] = responseChan
}

func (cm *ConnectionManager) UnregisterPendingRequest(requestID uint64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.pendingRequests, requestID)
}

func (cm *ConnectionManager) SendMessage(msg *protocol.WSMessage) error {
	cm.writeMu.Lock()
	defer cm.writeMu.Unlock()

	/*
		json, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		fmt.Println(string(json))
		return cm.conn.WriteMessage(websocket.TextMessage, json)
	*/
	return cm.conn.WriteJSON(msg)
}

func (cm *ConnectionManager) SendMessageWithBinary(msg *protocol.WSMessage, binaryData []byte) error {
	cm.writeMu.Lock()
	defer cm.writeMu.Unlock()

	// Set the Binary flag
	msg.Binary = true

	// Send the JSON message first
	if err := cm.conn.WriteJSON(msg); err != nil {
		return err
	}

	// Send the binary frame immediately after
	return cm.conn.WriteMessage(websocket.BinaryMessage, binaryData)
}

func (cm *ConnectionManager) Close() {
	cm.closeMu.Lock()
	defer cm.closeMu.Unlock()

	if !cm.closed {
		close(cm.done)
		cm.closed = true
	}
	cm.conn.Close()
}

// ModuleManager methods

func (mm *ModuleManager) GenerateModuleID() uint64 {
	return atomic.AddUint64(&mm.moduleCounter, 1)
}

func (mm *ModuleManager) IsEnabled() bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.enabled
}

func (mm *ModuleManager) SetEnabled(enabled bool) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.enabled = enabled
}

func (mm *ModuleManager) shouldProcessRequest(module types.Module, ctx *types.RequestContext, filterEngine *FilterEngine) bool {
	mm.mu.RLock()
	filterConfig, exists := mm.filters[module.ID()]
	mm.mu.RUnlock()

	if !exists || filterConfig.RequestFilter == nil {
		return true
	}

	shouldProcess := filterEngine.ShouldProcessRequestFilter(filterConfig.RequestFilter, ctx)
	if !shouldProcess {
		ctx.CallCtx.SetData(module.ID(), "skip_response", true)
	}

	return shouldProcess
}

func (mm *ModuleManager) shouldProcessResponse(module types.Module, ctx *types.ResponseContext, filterEngine *FilterEngine) bool {
	// Check if module explicitly requested this response
	if ctx.CallCtx.GetData(module.ID(), "wants_response") == true {
		return true
	}

	if ctx.CallCtx.GetData(module.ID(), "skip_response") == true {
		return false
	}

	mm.mu.RLock()
	filterConfig, exists := mm.filters[module.ID()]
	mm.mu.RUnlock()

	if !exists || filterConfig.ResponseFilter == nil {
		return true
	}

	return filterEngine.ShouldProcessResponseFilter(filterConfig.ResponseFilter, ctx)
}

func (mm *ModuleManager) RegisterModule(module types.Module, filter *types.FilterConfig) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.modules[module.ID()] = module
	if filter != nil {
		mm.filters[module.ID()] = filter
	}

	return nil
}

func (mm *ModuleManager) UnregisterModule(moduleID uint64) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if module, exists := mm.modules[moduleID]; exists {
		module.Close()
		delete(mm.modules, moduleID)
		delete(mm.filters, moduleID)
	}

	return nil
}

func (mm *ModuleManager) parseFilterConfig(config map[string]interface{}) *types.FilterConfig {
	filterConfig := &types.FilterConfig{}

	// Parse request filter
	if requestFilterData, ok := config["request_filter"].(map[string]interface{}); ok {
		filterConfig.RequestFilter = mm.parseFilter(requestFilterData)
	}

	// Parse response filter
	if responseFilterData, ok := config["response_filter"].(map[string]interface{}); ok {
		filterConfig.ResponseFilter = mm.parseFilter(responseFilterData)
	}

	return filterConfig
}

func (mm *ModuleManager) parseFilter(config map[string]interface{}) *types.Filter {
	filter := &types.Filter{}

	if contentTypes, ok := config["content_types"].([]interface{}); ok {
		filter.ContentTypes = make([]string, len(contentTypes))
		for i, ct := range contentTypes {
			if str, ok := ct.(string); ok {
				filter.ContentTypes[i] = str
			}
		}
	}

	if jsonQuery, ok := config["json_query"].(string); ok {
		filter.JSONQuery = jsonQuery
	}

	if methods, ok := config["methods"].([]interface{}); ok {
		filter.Methods = make([]string, len(methods))
		for i, method := range methods {
			if str, ok := method.(string); ok {
				filter.Methods[i] = str
			}
		}
	}

	if statusCodes, ok := config["status_codes"].([]interface{}); ok {
		filter.StatusCodes = make([]int, len(statusCodes))
		for i, code := range statusCodes {
			if num, ok := code.(float64); ok {
				filter.StatusCodes[i] = int(num)
			}
		}
	}

	return filter
}

func (m *Manager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		m.logger.WithError(err).Error("WebSocket upgrade failed")
		return
	}

	connMgr := &ConnectionManager{
		conn:            conn,
		manager:         m.ModuleManager,
		pendingRequests: make(map[uint64]chan *protocol.WSMessageWithBinary),
		modules:         make([]uint64, 0),
		done:            make(chan struct{}),
	}

	m.mu.Lock()
	m.connections[conn] = connMgr
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connections, conn)
		m.mu.Unlock()

		for _, moduleID := range connMgr.modules {
			m.UnregisterModule(moduleID)
		}

		connMgr.Close()
	}()

	m.logger.WithField("remote", conn.RemoteAddr()).Info("WebSocket connection established")

	go m.handleConnection(connMgr)

	<-connMgr.done
	m.logger.WithField("remote", conn.RemoteAddr()).Info("WebSocket connection closed")
}

func (m *Manager) handleConnection(connMgr *ConnectionManager) {
	defer connMgr.Close()

	var expectingBinary bool
	var lastJSONMessage *protocol.WSMessage

	for {
		messageType, data, err := connMgr.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				m.logger.Info("WebSocket connection closed")
			} else {
				m.logger.WithError(err).Error("WebSocket read error")
			}
			return
		}

		switch messageType {
		case websocket.TextMessage:
			var msg protocol.WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				m.logger.WithError(err).Debug("Failed to unmarshal JSON message")
				return
			}

			if msg.Binary {
				expectingBinary = true
				lastJSONMessage = &msg
			} else {
				m.handleJSONMessage(connMgr, &msg, nil)
			}
		case websocket.BinaryMessage:
			if expectingBinary && lastJSONMessage != nil {
				m.handleJSONMessage(connMgr, lastJSONMessage, data)
				expectingBinary = false
				lastJSONMessage = nil
			} else {
				m.logger.Warn("Received unexpected binary message")
			}
		}
	}
}

func (m *Manager) handleJSONMessage(connMgr *ConnectionManager, msg *protocol.WSMessage, binaryData []byte) {
	if msg.ResponseID != 0 {
		m.handleResponse(connMgr, msg, binaryData)
	} else {
		m.handleRequest(connMgr, msg, binaryData)
	}
}

func (m *Manager) handleResponse(connMgr *ConnectionManager, msg *protocol.WSMessage, binaryData []byte) {
	connMgr.mu.RLock()
	responseChan, exists := connMgr.pendingRequests[msg.ResponseID]
	connMgr.mu.RUnlock()

	if exists {
		select {
		case responseChan <- &protocol.WSMessageWithBinary{
			WSMessage:  msg,
			BinaryData: binaryData,
		}:
		default:
		}
	}
}

func (m *Manager) handleRequest(connMgr *ConnectionManager, msg *protocol.WSMessage, binaryData []byte) {
	switch msg.Method {
	case "register_module":
		m.handleModuleRegistration(connMgr, msg)
	case "unregister_module":
		m.handleModuleUnregistration(connMgr, msg)
	default:
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Unknown method: %s", msg.Method))
	}
}

func (m *Manager) handleModuleRegistration(connMgr *ConnectionManager, msg *protocol.WSMessage) {
	var req protocol.RegisterModuleRequest
	if err := m.parseMessageData(msg.Data, &req); err != nil {
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Invalid registration request: %v", err))
		return
	}

	moduleID := m.GenerateModuleID()

	var module types.Module
	var err error

	switch req.Type {
	case "request_snooper":
		module, err = m.createRequestSnooper(moduleID, connMgr, req.Config)
	case "response_snooper":
		module, err = m.createResponseSnooper(moduleID, connMgr, req.Config)
	case "request_counter":
		module, err = m.createRequestCounter(moduleID, connMgr, req.Config)
	case "response_tracer":
		module, err = m.createResponseTracer(moduleID, connMgr, req.Config)
	default:
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Unknown module type: %s", req.Type))
		return
	}

	if err != nil {
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Failed to create module: %v", err))
		return
	}

	filterConfig := m.ModuleManager.parseFilterConfig(req.Config)

	// Compile the filters if they have JSON queries
	if filterConfig != nil {
		if filterConfig.RequestFilter != nil && filterConfig.RequestFilter.JSONQuery != "" {
			if err := m.filterEngine.CompileFilter(filterConfig.RequestFilter); err != nil {
				m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Failed to compile request filter: %v", err))
				return
			}
		}
		if filterConfig.ResponseFilter != nil && filterConfig.ResponseFilter.JSONQuery != "" {
			if err := m.filterEngine.CompileFilter(filterConfig.ResponseFilter); err != nil {
				m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Failed to compile response filter: %v", err))
				return
			}
		}
	}

	module.Configure(req.Config)

	if err := m.RegisterModule(module, filterConfig); err != nil {
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Failed to register module: %v", err))
		return
	}

	connMgr.mu.Lock()
	connMgr.modules = append(connMgr.modules, moduleID)
	connMgr.mu.Unlock()

	resp := protocol.RegisterModuleResponse{
		Success:  true,
		ModuleID: moduleID,
		Message:  fmt.Sprintf("Module %s registered successfully", req.Type),
	}

	m.sendResponse(connMgr, msg, resp)
	m.logger.WithFields(logrus.Fields{
		"module_id":   moduleID,
		"module_type": req.Type,
		"module_name": req.Name,
	}).Info("Module registered")
}

func (m *Manager) handleModuleUnregistration(connMgr *ConnectionManager, msg *protocol.WSMessage) {
	var moduleID uint64
	if err := m.parseMessageData(msg.Data, &moduleID); err != nil {
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Invalid module ID: %v", err))
		return
	}

	if err := m.UnregisterModule(moduleID); err != nil {
		m.sendErrorResponse(connMgr, msg, fmt.Sprintf("Failed to unregister module: %v", err))
		return
	}

	connMgr.mu.Lock()
	for i, id := range connMgr.modules {
		if id == moduleID {
			connMgr.modules = append(connMgr.modules[:i], connMgr.modules[i+1:]...)
			break
		}
	}
	connMgr.mu.Unlock()

	resp := map[string]interface{}{
		"success": true,
		"message": "Module unregistered successfully",
	}

	m.sendResponse(connMgr, msg, resp)
	m.logger.WithField("module_id", moduleID).Info("Module unregistered")
}

func (m *Manager) parseMessageData(data interface{}, target interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(dataBytes, target)
}

func (m *Manager) sendResponse(connMgr *ConnectionManager, originalMsg *protocol.WSMessage, data interface{}) {
	response := &protocol.WSMessage{
		ResponseID: originalMsg.RequestID,
		Method:     originalMsg.Method,
		Data:       data,
		Timestamp:  time.Now().UnixNano(),
	}

	if err := connMgr.SendMessage(response); err != nil {
		m.logger.WithError(err).Error("Failed to send WebSocket response")
	}
}

func (m *Manager) sendErrorResponse(connMgr *ConnectionManager, originalMsg *protocol.WSMessage, errorMsg string) {
	errStr := errorMsg
	response := &protocol.WSMessage{
		ResponseID: originalMsg.RequestID,
		Method:     originalMsg.Method,
		Error:      &errStr,
		Timestamp:  time.Now().UnixNano(),
	}

	if err := connMgr.SendMessage(response); err != nil {
		m.logger.WithError(err).Error("Failed to send WebSocket error response")
	}
}

func (m *Manager) createRequestSnooper(id uint64, connMgr *ConnectionManager, config map[string]interface{}) (types.Module, error) {
	return &builtin.RequestSnooper{
		Id:      id,
		ConnMgr: connMgr,
	}, nil
}

func (m *Manager) createResponseSnooper(id uint64, connMgr *ConnectionManager, config map[string]interface{}) (types.Module, error) {
	return &builtin.ResponseSnooper{
		Id:      id,
		ConnMgr: connMgr,
	}, nil
}

func (m *Manager) createRequestCounter(id uint64, connMgr *ConnectionManager, config map[string]interface{}) (types.Module, error) {
	return &builtin.RequestCounter{
		Id:      id,
		ConnMgr: connMgr,
	}, nil
}

func (m *Manager) createResponseTracer(id uint64, connMgr *ConnectionManager, config map[string]interface{}) (types.Module, error) {
	return &builtin.ResponseTracer{
		Id:      id,
		ConnMgr: connMgr,
	}, nil
}
