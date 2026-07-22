package webcontrol

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/control"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
)

const sessionCookieName = "id"

//go:embed web/*
var webFiles embed.FS

type ServerOptions struct {
	PublicOrigin   string
	Sessions       *SessionManager
	Control        *control.Manager
	Controller     kvm.OperationalController
	FrameRate      int
	ICEServers     []string
	WebRTCEnabled  bool
	ConnectTimeout time.Duration
}

type Server struct {
	publicOrigin   string
	sessions       *SessionManager
	control        *control.Manager
	controller     kvm.OperationalController
	frameRate      int
	iceServers     []string
	webRTCEnabled  bool
	connectTimeout time.Duration
	assets         fs.FS
}

type clientMessage struct {
	Type    string   `json:"type"`
	Keys    []string `json:"keys,omitempty"`
	X       int      `json:"x,omitempty"`
	Y       int      `json:"y,omitempty"`
	DX      int      `json:"dx,omitempty"`
	DY      int      `json:"dy,omitempty"`
	Buttons []string `json:"buttons,omitempty"`
	Wheel   int      `json:"wheel,omitempty"`
}

type serverMessage struct {
	Type    string `json:"type"`
	State   string `json:"state,omitempty"`
	Holder  string `json:"holder,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func NewServer(options ServerOptions) (*Server, error) {
	if options.Sessions == nil || options.Control == nil || options.Controller == nil {
		return nil, errors.New("web control requires sessions, control, and controller")
	}
	origin, err := url.Parse(options.PublicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("public origin must be an HTTPS origin without a path, query, or fragment")
	}
	if options.FrameRate <= 0 {
		options.FrameRate = 30
	}
	if options.FrameRate > 30 {
		return nil, errors.New("browser frame rate cannot exceed 30")
	}
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = 10 * time.Second
	}
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	return &Server{
		publicOrigin:   strings.TrimRight(options.PublicOrigin, "/"),
		sessions:       options.Sessions,
		control:        options.Control,
		controller:     options.Controller,
		frameRate:      options.FrameRate,
		iceServers:     append([]string(nil), options.ICEServers...),
		webRTCEnabled:  options.WebRTCEnabled,
		connectTimeout: options.ConnectTimeout,
		assets:         assets,
	}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /control/enter", server.handleEntry)
	mux.HandleFunc("GET /control/ws", server.handleWebSocket)
	mux.HandleFunc("POST /control/webrtc/offer", server.handleWebRTCOffer)
	mux.HandleFunc("GET /control/webrtc/config", server.handleWebRTCConfig)
	mux.HandleFunc("GET /control/", server.handleAsset)
	return server.securityHeaders(mux)
}

func (server *Server) handleWebRTCConfig(writer http.ResponseWriter, request *http.Request) {
	if !server.validRequestSession(request) {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(struct {
		Enabled          bool     `json:"enabled"`
		STUNURLs         []string `json:"stun_urls"`
		ConnectTimeoutMS int64    `json:"connect_timeout_ms"`
	}{
		Enabled:          server.webRTCEnabled,
		STUNURLs:         append([]string(nil), server.iceServers...),
		ConnectTimeoutMS: server.connectTimeout.Milliseconds(),
	})
}

func (server *Server) handleWebRTCOffer(writer http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || !server.webRTCEnabled || !server.sessions.ValidateSession(cookie.Value) || request.Header.Get("Origin") != server.publicOrigin {
		http.NotFound(writer, request)
		return
	}
	defer request.Body.Close()
	var offer webrtc.SessionDescription
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&offer); err != nil || offer.Type != webrtc.SDPTypeOffer {
		http.Error(writer, "invalid WebRTC offer", http.StatusBadRequest)
		return
	}
	configuration := webrtc.Configuration{}
	if len(server.iceServers) > 0 {
		configuration.ICEServers = []webrtc.ICEServer{{URLs: append([]string(nil), server.iceServers...)}}
	}
	peer, err := webrtc.NewPeerConnection(configuration)
	if err != nil {
		http.Error(writer, "could not create WebRTC connection", http.StatusInternalServerError)
		return
	}
	peerContext, cancelPeer := context.WithCancel(context.Background())
	var closeOnce sync.Once
	var lifecycleMu sync.Mutex
	var readinessTimer *time.Timer
	var inputPump *browserInputPump
	connectionID := ""
	closed := false
	closePeer := func() {
		closeOnce.Do(func() {
			lifecycleMu.Lock()
			closed = true
			timer := readinessTimer
			readinessTimer = nil
			id := connectionID
			pump := inputPump
			lifecycleMu.Unlock()
			if timer != nil {
				timer.Stop()
			}
			cancelPeer()
			if pump != nil {
				pump.Stop()
			}
			if id != "" {
				server.control.Release(control.BrowserOwner(id))
				server.sessions.FailTransport(id, "WEBRTC_DISCONNECTED")
			}
			_ = server.controller.ReleaseAll(context.Background())
			_ = peer.Close()
		})
	}
	newConnectionID, err := server.sessions.BeginTransport(cookie.Value, TransportDirect, closePeer)
	if err != nil {
		closePeer()
		http.Error(writer, "browser transport is already active", http.StatusConflict)
		return
	}
	lifecycleMu.Lock()
	connectionID = newConnectionID
	if !closed {
		readinessTimer = time.AfterFunc(server.connectTimeout, closePeer)
	}
	lifecycleMu.Unlock()

	var channelsMu sync.Mutex
	var framesChannel *webrtc.DataChannel
	var controlChannel *webrtc.DataChannel
	var framesOpen bool
	var controlOpen bool
	var activateOnce sync.Once
	activate := func() {
		channelsMu.Lock()
		ready := framesOpen && controlOpen
		frames := framesChannel
		commands := controlChannel
		channelsMu.Unlock()
		if !ready {
			return
		}
		activateOnce.Do(func() {
			lifecycleMu.Lock()
			if closed {
				lifecycleMu.Unlock()
				return
			}
			timer := readinessTimer
			readinessTimer = nil
			id := connectionID
			lifecycleMu.Unlock()
			if timer != nil {
				timer.Stop()
			}
			if connectErr := server.sessions.MarkTransportConnected(id); connectErr != nil {
				closePeer()
				return
			}
			owner := control.BrowserOwner(id)
			pump := newBrowserInputPump(peerContext, func(ctx context.Context, message clientMessage) error {
				return server.handleClientMessage(ctx, owner, message, nil)
			}, func(inputErr error) {
				_ = commands.SendText(string(mustJSON(operationServerMessage(inputErr))))
			})
			lifecycleMu.Lock()
			if closed {
				lifecycleMu.Unlock()
				pump.Stop()
				return
			}
			inputPump = pump
			lifecycleMu.Unlock()
			_ = commands.SendText(string(mustJSON(server.controlMessage())))
			go server.captureDataChannelLoop(peerContext, frames, commands, closePeer)
			commands.OnMessage(func(message webrtc.DataChannelMessage) {
				if message.IsString {
					if inputErr := server.queueInput(peerContext, owner, message.Data, nil, pump); inputErr != nil {
						_ = commands.SendText(string(mustJSON(operationServerMessage(inputErr))))
					}
				}
			})
		})
	}
	peer.OnDataChannel(func(channel *webrtc.DataChannel) {
		channelsMu.Lock()
		switch channel.Label() {
		case "frames":
			maxRetransmits := channel.MaxRetransmits()
			if framesChannel != nil || channel.Ordered() || maxRetransmits == nil || *maxRetransmits != 0 || channel.MaxPacketLifeTime() != nil {
				channelsMu.Unlock()
				closePeer()
				return
			}
			framesChannel = channel
			channel.OnClose(closePeer)
			channel.OnError(func(error) { closePeer() })
			channel.OnOpen(func() {
				channelsMu.Lock()
				framesOpen = true
				channelsMu.Unlock()
				activate()
			})
		case "control":
			if controlChannel != nil || !channel.Ordered() || channel.MaxRetransmits() != nil || channel.MaxPacketLifeTime() != nil {
				channelsMu.Unlock()
				closePeer()
				return
			}
			controlChannel = channel
			channel.OnClose(closePeer)
			channel.OnError(func(error) { closePeer() })
			channel.OnOpen(func() {
				channelsMu.Lock()
				controlOpen = true
				channelsMu.Unlock()
				activate()
			})
		default:
			channelsMu.Unlock()
			closePeer()
			return
		}
		channelsMu.Unlock()
	})
	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateDisconnected || state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			closePeer()
		}
	})
	if err := peer.SetRemoteDescription(offer); err != nil {
		closePeer()
		http.Error(writer, "invalid WebRTC offer", http.StatusBadRequest)
		return
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		closePeer()
		http.Error(writer, "could not create WebRTC answer", http.StatusInternalServerError)
		return
	}
	gatheringComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(answer); err != nil {
		closePeer()
		http.Error(writer, "could not set WebRTC answer", http.StatusInternalServerError)
		return
	}
	select {
	case <-gatheringComplete:
	case <-time.After(server.connectTimeout):
		closePeer()
		http.Error(writer, "WebRTC candidate gathering timed out", http.StatusGatewayTimeout)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(peer.LocalDescription()); err != nil {
		closePeer()
	}
}

func (server *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; connect-src 'self'; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) handleEntry(writer http.ResponseWriter, request *http.Request) {
	token := request.URL.Query().Get("token")
	if token == "" {
		http.NotFound(writer, request)
		return
	}
	sessionToken, err := server.sessions.Redeem(token)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/control/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writer.Header().Set("Location", "/control/")
	writer.WriteHeader(http.StatusSeeOther)
}

func (server *Server) handleAsset(writer http.ResponseWriter, request *http.Request) {
	if !server.validRequestSession(request) {
		http.NotFound(writer, request)
		return
	}
	name := strings.TrimPrefix(request.URL.Path, "/control/")
	if name == "" {
		name = "index.html"
	}
	if name != "index.html" && name != "app.js" && name != "style.css" {
		http.NotFound(writer, request)
		return
	}
	data, err := fs.ReadFile(server.assets, name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	switch {
	case strings.HasSuffix(name, ".html"):
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	_, _ = writer.Write(data)
}

func (server *Server) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || !server.sessions.ValidateSession(cookie.Value) {
		http.NotFound(writer, request)
		return
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		OriginPatterns: []string{server.publicOrigin},
	})
	if err != nil {
		return
	}
	connection.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(request.Context())
	connectionID, err := server.sessions.Connect(cookie.Value, cancel)
	if err != nil {
		cancel()
		_ = connection.Close(websocket.StatusPolicyViolation, "browser session is unavailable")
		return
	}
	owner := control.BrowserOwner(connectionID)
	var inputPump *browserInputPump
	defer func() {
		cancel()
		if inputPump != nil {
			inputPump.Stop()
		}
		server.control.Release(owner)
		_ = server.controller.ReleaseAll(context.Background())
		server.sessions.Disconnect(connectionID)
		_ = connection.Close(websocket.StatusNormalClosure, "session disconnected")
	}()

	messages := make(chan []byte, 32)
	frames := make(chan []byte, 1)
	frameAcknowledgements := make(chan struct{}, 1)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		server.writeLoop(ctx, connection, messages, frames, frameAcknowledgements)
		cancel()
	}()
	go server.captureLoop(ctx, messages, frames)
	server.sendMessage(ctx, messages, server.controlMessage())
	inputPump = newBrowserInputPump(ctx, func(ctx context.Context, message clientMessage) error {
		return server.handleClientMessage(ctx, owner, message, frameAcknowledgements)
	}, func(inputErr error) {
		server.sendOperationError(ctx, messages, inputErr)
	})

	for {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			break
		}
		if messageType != websocket.MessageText {
			server.sendError(ctx, messages, "INVALID_MESSAGE", "client messages must be JSON text")
			continue
		}
		if err := server.queueInput(ctx, owner, data, frameAcknowledgements, inputPump); err != nil {
			server.sendOperationError(ctx, messages, err)
		}
	}
	cancel()
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
	}
}

func (server *Server) validRequestSession(request *http.Request) bool {
	cookie, err := request.Cookie(sessionCookieName)
	return err == nil && server.sessions.ValidateSession(cookie.Value)
}

func (server *Server) handleInput(ctx context.Context, owner control.Owner, data []byte, frameAcknowledgements chan<- struct{}) error {
	message, err := decodeClientMessage(data)
	if err != nil {
		return err
	}
	if err := validateClientMessage(message); err != nil {
		return err
	}
	return server.handleClientMessage(ctx, owner, message, frameAcknowledgements)
}

func (server *Server) queueInput(ctx context.Context, owner control.Owner, data []byte, frameAcknowledgements chan<- struct{}, pump *browserInputPump) error {
	message, err := decodeClientMessage(data)
	if err != nil {
		return err
	}
	if err := validateClientMessage(message); err != nil {
		return err
	}
	if message.Type == "frame_ack" || message.Type == "ping" {
		return server.handleClientMessage(ctx, owner, message, frameAcknowledgements)
	}
	return pump.Submit(message)
}

func decodeClientMessage(data []byte) (clientMessage, error) {
	var message clientMessage
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return clientMessage{}, &kvm.OperationError{Code: kvm.ErrorInvalidArgument, Message: "invalid browser input message", Recovery: "send one documented JSON input event"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return clientMessage{}, &kvm.OperationError{Code: kvm.ErrorInvalidArgument, Message: "browser input contains trailing data", Recovery: "send exactly one JSON input event"}
	}
	return message, nil
}

func validateClientMessage(message clientMessage) error {
	switch message.Type {
	case "keyboard":
		return kvm.ValidateKeyboardStateRequest(kvm.KeyboardStateRequest{Keys: message.Keys})
	case "pointer_absolute":
		return kvm.ValidateAbsolutePointerStateRequest(kvm.AbsolutePointerStateRequest{X: message.X, Y: message.Y, Buttons: message.Buttons, Wheel: message.Wheel})
	case "pointer_relative":
		return kvm.ValidateRelativePointerStateRequest(kvm.RelativePointerStateRequest{DX: message.DX, DY: message.DY, Buttons: message.Buttons, Wheel: message.Wheel})
	case "frame_ack", "release_all", "ping":
		return nil
	default:
		return &kvm.OperationError{Code: kvm.ErrorInvalidArgument, Message: "unsupported browser input message", Recovery: "use keyboard, pointer_absolute, pointer_relative, frame_ack, release_all, or ping"}
	}
}

func (server *Server) handleClientMessage(ctx context.Context, owner control.Owner, message clientMessage, frameAcknowledgements chan<- struct{}) error {
	if message.Type == "frame_ack" {
		select {
		case frameAcknowledgements <- struct{}{}:
		default:
		}
		return nil
	}
	if message.Type == "release_all" {
		if !server.control.Release(owner) {
			return server.controller.ReleaseAll(ctx)
		}
		return nil
	}
	if message.Type == "ping" {
		return nil
	}
	if err := server.control.AcquireBrowser(owner.ID); err != nil {
		return err
	}
	switch message.Type {
	case "keyboard":
		return server.controller.SetKeyboardState(ctx, kvm.KeyboardStateRequest{Keys: message.Keys})
	case "pointer_absolute":
		return server.controller.SetPointerAbsoluteState(ctx, kvm.AbsolutePointerStateRequest{X: message.X, Y: message.Y, Buttons: message.Buttons, Wheel: message.Wheel})
	case "pointer_relative":
		return server.controller.SetPointerRelativeState(ctx, kvm.RelativePointerStateRequest{DX: message.DX, DY: message.DY, Buttons: message.Buttons, Wheel: message.Wheel})
	default:
		return &kvm.OperationError{Code: kvm.ErrorInvalidArgument, Message: "unsupported browser input message", Recovery: "use keyboard, pointer_absolute, pointer_relative, frame_ack, release_all, or ping"}
	}
}

func (server *Server) captureLoop(ctx context.Context, messages chan<- []byte, frames chan []byte) {
	frameInterval := time.Second / time.Duration(server.frameRate)
	for ctx.Err() == nil {
		stream, err := server.controller.OpenVideoStream(ctx)
		if err != nil {
			server.sendOperationError(ctx, messages, err)
			if !waitContext(ctx, time.Second) {
				return
			}
			continue
		}
		server.sendMessage(ctx, messages, serverMessage{Type: "video", State: "ready"})
		nextFrameAt := time.Time{}
		for ctx.Err() == nil {
			frame, err := stream.Next(ctx)
			if err != nil {
				_ = stream.Close()
				server.sendOperationError(ctx, messages, err)
				break
			}
			now := time.Now()
			if now.Before(nextFrameAt) {
				continue
			}
			nextFrameAt = now.Add(frameInterval)
			offerLatestFrame(frames, frame.JPEG)
		}
		_ = stream.Close()
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

func (server *Server) captureDataChannelLoop(ctx context.Context, frames, commands *webrtc.DataChannel, closePeer func()) {
	defer closePeer()
	frameInterval := time.Second / time.Duration(server.frameRate)
	var frameID uint32
	for ctx.Err() == nil {
		stream, err := server.controller.OpenVideoStream(ctx)
		if err != nil {
			_ = commands.SendText(string(mustJSON(operationServerMessage(err))))
			if !waitContext(ctx, time.Second) {
				return
			}
			continue
		}
		_ = commands.SendText(string(mustJSON(serverMessage{Type: "video", State: "ready"})))
		nextFrameAt := time.Time{}
		for ctx.Err() == nil {
			frame, frameErr := stream.Next(ctx)
			if frameErr != nil {
				_ = stream.Close()
				_ = commands.SendText(string(mustJSON(operationServerMessage(frameErr))))
				break
			}
			now := time.Now()
			if now.Before(nextFrameAt) || frames.BufferedAmount() != 0 {
				continue
			}
			nextFrameAt = now.Add(frameInterval)
			frameID++
			if err := sendFrameChunks(frameID, frame.JPEG, frames.Send); err != nil {
				if errors.Is(err, errInvalidFrameForChunking) {
					_ = commands.SendText(string(mustJSON(serverMessage{Type: "error", Code: "VIDEO_FRAME_INVALID", Message: "captured video frame cannot be fragmented"})))
					_ = stream.Close()
					break
				}
				if frames.ReadyState() != webrtc.DataChannelStateOpen {
					_ = stream.Close()
					return
				}
				// 部分发送的画面帧会被主动放弃。下一帧的编号让浏览器能够
				// 淘汰未完成的旧帧，同时保持可靠控制通道继续工作。
				continue
			}
		}
		_ = stream.Close()
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func operationServerMessage(err error) serverMessage {
	var operationError *kvm.OperationError
	if errors.As(err, &operationError) {
		return serverMessage{Type: "error", Code: string(operationError.Code), Message: operationError.Message}
	}
	return serverMessage{Type: "error", Code: string(kvm.ErrorInternal), Message: "operation failed"}
}

func (server *Server) writeLoop(ctx context.Context, connection *websocket.Conn, messages <-chan []byte, frames <-chan []byte, frameAcknowledgements <-chan struct{}) {
	canSendFrame := true
	for {
		var availableFrames <-chan []byte
		if canSendFrame {
			availableFrames = frames
		}
		select {
		case <-ctx.Done():
			return
		case message := <-messages:
			if !writeWebSocket(ctx, connection, websocket.MessageText, message) {
				return
			}
		case <-frameAcknowledgements:
			canSendFrame = true
		case frame := <-availableFrames:
			if !writeWebSocket(ctx, connection, websocket.MessageBinary, frame) {
				return
			}
			canSendFrame = false
		}
	}
}

func writeWebSocket(ctx context.Context, connection *websocket.Conn, messageType websocket.MessageType, data []byte) bool {
	writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return connection.Write(writeContext, messageType, data) == nil
}

func offerLatestFrame(frames chan []byte, frame []byte) {
	copyOfFrame := append([]byte(nil), frame...)
	select {
	case frames <- copyOfFrame:
		return
	default:
	}
	select {
	case <-frames:
	default:
	}
	select {
	case frames <- copyOfFrame:
	default:
	}
}

func (server *Server) controlMessage() serverMessage {
	status := server.control.Status()
	return serverMessage{Type: "control", State: status.State, Holder: status.Holder}
}

func (server *Server) sendOperationError(ctx context.Context, messages chan<- []byte, err error) {
	var operationError *kvm.OperationError
	if errors.As(err, &operationError) {
		server.sendError(ctx, messages, string(operationError.Code), operationError.Message)
		return
	}
	server.sendError(ctx, messages, string(kvm.ErrorInternal), "operation failed")
}

func (server *Server) sendError(ctx context.Context, messages chan<- []byte, code, message string) {
	server.sendMessage(ctx, messages, serverMessage{Type: "error", Code: code, Message: message})
}

func (server *Server) sendMessage(ctx context.Context, messages chan<- []byte, message serverMessage) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return
	}
	select {
	case messages <- encoded:
	case <-ctx.Done():
	}
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (server *Server) String() string {
	return fmt.Sprintf("web control at %s/control/", server.publicOrigin)
}
