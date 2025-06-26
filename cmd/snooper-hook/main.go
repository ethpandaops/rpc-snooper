package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Config struct {
	URL        string
	ModuleType string
	ModuleName string
	Config     map[string]interface{}
	Verbose    bool
}

type TestClient struct {
	conn   *websocket.Conn
	logger *logrus.Logger
	config *Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Centralized request/response handling
	requestCounter  uint64
	pendingRequests map[uint64]chan *protocol.WSMessageWithBinary
	requestMu       sync.RWMutex

	// Module state
	moduleID      uint64
	binaryReaders map[uint64]io.ReadCloser
	binaryMu      sync.RWMutex
}

func main() {
	config := parseFlags()

	logger := logrus.New()
	if config.Verbose {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &TestClient{
		logger:          logger,
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
		pendingRequests: make(map[uint64]chan *protocol.WSMessageWithBinary),
		binaryReaders:   make(map[uint64]io.ReadCloser),
	}

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	if err := client.Connect(); err != nil {
		logger.WithError(err).Fatal("Failed to connect to snooper")
	}

	if err := client.RegisterModule(); err != nil {
		logger.WithError(err).Fatal("Failed to register module")
	}

	logger.WithFields(logrus.Fields{
		"module_type": config.ModuleType,
		"module_name": config.ModuleName,
		"module_id":   client.moduleID,
	}).Info("Module registered successfully, listening for hooks...")

	// Wait for graceful shutdown with timeout
	shutdownDone := make(chan struct{})
	go func() {
		client.wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		logger.Info("Test client shutdown complete")
	}
}

func parseFlags() *Config {
	config := &Config{
		Config: make(map[string]interface{}),
	}

	flag.StringVar(&config.URL, "url", "ws://localhost:8080/_snooper/control", "WebSocket URL of the snooper control endpoint")
	flag.StringVar(&config.ModuleType, "type", "request_snooper", "Module type (request_snooper, response_snooper, request_counter, response_tracer)")
	flag.StringVar(&config.ModuleName, "name", "test-hook", "Module name")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")

	var configStr string
	flag.StringVar(&configStr, "config", "{}", "Module configuration as JSON string")

	flag.Parse()

	if configStr != "{}" {
		if err := json.Unmarshal([]byte(configStr), &config.Config); err != nil {
			log.Fatalf("Invalid config JSON: %v", err)
		}
	}

	// Validate module type
	validTypes := []string{
		"request_snooper", "response_snooper", "request_counter", "response_tracer",
	}

	valid := false
	for _, t := range validTypes {
		if config.ModuleType == t {
			valid = true
			break
		}
	}

	if !valid {
		log.Fatalf("Invalid module type: %s. Valid types: %s", config.ModuleType, strings.Join(validTypes, ", "))
	}

	return config
}

func (c *TestClient) Connect() error {
	u, err := url.Parse(c.config.URL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	c.logger.WithField("url", c.config.URL).Info("Connecting to snooper control endpoint...")

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.conn = conn
	c.logger.Info("WebSocket connection established")

	// Start the SINGLE message handling goroutine
	c.wg.Add(1)
	go c.handleMessages()

	return nil
}

// Centralized request/response system
func (c *TestClient) sendRequest(method string, data interface{}, binaryData []byte) (*protocol.WSMessageWithBinary, error) {
	requestID := atomic.AddUint64(&c.requestCounter, 1)

	msg := protocol.WSMessage{
		RequestID: requestID,
		Method:    method,
		Data:      data,
		Timestamp: time.Now().UnixNano(),
		Binary:    binaryData != nil,
	}

	// Register pending request BEFORE sending
	responseChan := make(chan *protocol.WSMessageWithBinary, 1)
	c.requestMu.Lock()
	c.pendingRequests[requestID] = responseChan
	c.requestMu.Unlock()

	// Cleanup on exit
	defer func() {
		c.requestMu.Lock()
		delete(c.pendingRequests, requestID)
		c.requestMu.Unlock()
	}()

	// Send the request
	if err := c.conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if binaryData != nil {
		if err := c.conn.WriteMessage(websocket.BinaryMessage, binaryData); err != nil {
			return nil, fmt.Errorf("failed to send binary message: %w", err)
		}
	}

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	select {
	case response := <-responseChan:
		return response, nil
	case <-ctx.Done():
		if c.ctx.Err() != nil {
			return nil, fmt.Errorf("context cancelled: %w", c.ctx.Err())
		}
		return nil, fmt.Errorf("request timeout")
	}
}

func (c *TestClient) RegisterModule() error {
	regReq := protocol.RegisterModuleRequest{
		Type:   c.config.ModuleType,
		Name:   c.config.ModuleName,
		Config: c.config.Config,
	}

	response, err := c.sendRequest("register_module", regReq, nil)
	if err != nil {
		return err
	}

	if response.Error != nil {
		return fmt.Errorf("registration failed: %s", *response.Error)
	}

	if regResp, ok := response.Data.(map[string]interface{}); ok {
		if success, ok := regResp["success"].(bool); ok && success {
			if moduleID, ok := regResp["module_id"].(float64); ok {
				c.moduleID = uint64(moduleID)
				return nil
			}
		}
		if msg, ok := regResp["message"].(string); ok {
			return fmt.Errorf("registration failed: %s", msg)
		}
	}
	return fmt.Errorf("invalid registration response")
}

func (c *TestClient) handleMessages() {
	defer c.wg.Done()
	defer c.conn.Close()

	var expectingBinary bool
	var lastJSONMessage *protocol.WSMessage

	// Set read deadline based on context to handle cancellation properly
	go func() {
		<-c.ctx.Done()
		// Force close the connection when context is cancelled
		c.conn.Close()
	}()

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			select {
			case <-c.ctx.Done():
				// Context was cancelled, this is expected
				return
			default:
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					c.logger.Info("WebSocket connection closed")
				} else if !strings.Contains(err.Error(), "use of closed network connection") {
					c.logger.WithError(err).Error("WebSocket read error")
				}
				c.cancel()
				return
			}
		}

		switch messageType {
		case websocket.TextMessage:
			fmt.Println(string(data))

			var msg protocol.WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				c.logger.WithError(err).Debug("Failed to unmarshal JSON message")
				return
			}

			if msg.Binary {
				expectingBinary = true
				lastJSONMessage = &msg
			} else {
				c.handleJSONMessage(&protocol.WSMessageWithBinary{
					WSMessage:  &msg,
					BinaryData: nil,
				})
			}
		case websocket.BinaryMessage:
			if expectingBinary && lastJSONMessage != nil {
				msgWithBinary := &protocol.WSMessageWithBinary{
					WSMessage:  lastJSONMessage,
					BinaryData: data,
				}
				c.handleJSONMessage(msgWithBinary)
				expectingBinary = false
				lastJSONMessage = nil
			} else {
				c.logger.Warn("Received unexpected binary message")
			}
		}
	}
}

func (c *TestClient) handleJSONMessage(msg *protocol.WSMessageWithBinary) {
	c.logger.WithField("data", msg.Method).Info("Received JSON message")

	c.logger.WithFields(logrus.Fields{
		"method":      msg.Method,
		"request_id":  msg.RequestID,
		"response_id": msg.ResponseID,
		"module_id":   msg.ModuleID,
		"binary":      msg.Binary,
	}).Debug("Received JSON message")

	if msg.ResponseID != 0 {
		c.requestMu.RLock()
		responseChan, exists := c.pendingRequests[msg.ResponseID]
		c.requestMu.RUnlock()

		if exists {
			select {
			case responseChan <- msg:
				return
			default:
				c.logger.WithField("response_id", msg.ResponseID).Warn("Failed to deliver response")
			}
		}
	} else {
		switch msg.Method {
		case "hook_event":
			c.handleHookEvent(msg)
		case "counter_event":
			c.handleCounterEvent(msg)
		case "tracer_event":
			c.handleTracerEvent(msg)
		default:
			c.logger.WithField("method", msg.Method).Warn("Unknown message method")
		}
	}
}

func (c *TestClient) handleHookEvent(msg *protocol.WSMessageWithBinary) {
	hookData, ok := msg.Data.(map[string]interface{})
	if !ok {
		c.logger.Debug("Invalid hook event data")
		return
	}

	hookType, _ := hookData["hook_type"].(string)
	requestIDStr, _ := hookData["request_id"].(string)
	contentType, _ := hookData["content_type"].(string)

	c.logger.WithFields(logrus.Fields{
		"hook_type":    hookType,
		"request_id":   requestIDStr,
		"content_type": contentType,
	}).Info("Hook event received")

	// For JSON data, pretty print it
	var jsonData map[string]interface{}
	if err := json.Unmarshal(msg.BinaryData, &jsonData); err == nil {
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			c.logger.WithField("request_id", requestIDStr).Infof("JSON Data:\n%s", string(prettyJSON))
		}
	}
}

func (c *TestClient) handleCounterEvent(msg *protocol.WSMessageWithBinary) {
	counterData, ok := msg.Data.(map[string]interface{})
	if !ok {
		c.logger.Debug("Invalid counter event data")
		return
	}

	count, _ := counterData["count"].(float64)
	requestType, _ := counterData["request_type"].(string)

	c.logger.WithFields(logrus.Fields{
		"count":        int64(count),
		"request_type": requestType,
	}).Info("Counter event received")
}

func (c *TestClient) handleTracerEvent(msg *protocol.WSMessageWithBinary) {
	tracerData, ok := msg.Data.(map[string]interface{})
	if !ok {
		c.logger.Debug("Invalid tracer event data")
		return
	}

	requestID, _ := tracerData["request_id"].(string)
	duration, _ := tracerData["duration_ms"].(float64)
	statusCode, _ := tracerData["status_code"].(float64)
	requestSize, _ := tracerData["request_size"].(float64)
	responseSize, _ := tracerData["response_size"].(float64)
	requestData := tracerData["request_data"]
	responseData := tracerData["response_data"]

	c.logger.WithFields(logrus.Fields{
		"request_id":    requestID,
		"duration_ms":   int64(duration),
		"status_code":   int(statusCode),
		"request_size":  int64(requestSize),
		"response_size": int64(responseSize),
		"request_data":  requestData,
		"response_data": responseData,
	}).Info("Tracer event received")
}
