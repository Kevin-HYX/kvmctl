package mcpserver_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/Kevin-HYX/kvmctl/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsListOnlyPublishesConfiguredCapabilities(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{
		KeyboardConfigured: true,
		RelativePointer:    true,
	})
	server := mcpserver.New("0.1.0-test", controller)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "kvmctl-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	got := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	want := []string{
		"get_status",
		"key_press",
		"mouse_click",
		"mouse_drag_relative",
		"mouse_move_relative",
		"mouse_scroll",
		"release_all",
		"release_control",
		"type_text",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("tools = %v, want %v", got, want)
		}
	}
}

func TestGetScreenReturnsFreshCoordinateAnnotatedImage(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			frame.Set(x, y, color.RGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	controller := kvm.NewMemoryController(kvm.MemoryOptions{ScreenConfigured: true, ScreenImage: frame})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "kvmctl-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_screen",
		Arguments: map[string]any{"view": "coordinates", "format": "png"},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want one image and no error", result)
	}
	content, ok := result.Content[0].(*mcp.ImageContent)
	if !ok || content.MIMEType != "image/png" {
		t.Fatalf("content = %#v, want PNG image", result.Content[0])
	}
	decoded, err := png.Decode(bytes.NewReader(content.Data))
	if err != nil {
		t.Fatalf("decode returned PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 180 {
		t.Fatalf("image bounds = %v, want 320x180", decoded.Bounds())
	}
	if got := color.RGBAModel.Convert(decoded.At(160, 90)).(color.RGBA); got == (color.RGBA{R: 20, G: 30, B: 40, A: 255}) {
		t.Fatalf("center pixel was not annotated: %#v", got)
	}
}

func TestGetScreenNeverSubstitutesAStaleFrame(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{ScreenConfigured: true})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "get_screen", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want structured tool error", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "NO_VIDEO_SIGNAL") {
		t.Fatalf("content = %#v, want NO_VIDEO_SIGNAL and no image", result.Content)
	}
}

func TestKeyPressToolExecutesAtomicControllerOperation(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	server := mcpserver.New("0.1.0-test", controller)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "kvmctl-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "key_press",
		Arguments: map[string]any{
			"keys":    []any{"ctrl", "alt", "delete"},
			"hold_ms": 80,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %#v", result.Content)
	}
	events := controller.Events()
	if len(events) != 2 || events[0].Kind != kvm.EventKeyPress || events[1].Kind != kvm.EventReleaseAll {
		t.Fatalf("events = %#v, want atomic key press and release", events)
	}
}

func TestGetStatusReportsVersionAndAnonymousControlState(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "get_status", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || structured["version"] != "0.1.0-test" {
		t.Fatalf("structured status = %#v, want version", result.StructuredContent)
	}
	control, ok := structured["control"].(map[string]any)
	if !ok || control["state"] != "idle" {
		t.Fatalf("control = %#v, want anonymous idle state", structured["control"])
	}
}

func TestEveryConfiguredInputToolHasAnExecutableHandler(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "kvmctl-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"key_press", map[string]any{"keys": []any{"escape"}}},
		{"type_text", map[string]any{"text": "hello"}},
		{"mouse_move_absolute", map[string]any{"x": 100, "y": 200}},
		{"mouse_click_absolute", map[string]any{"x": 100, "y": 200, "button": "left"}},
		{"mouse_drag_absolute", map[string]any{"from_x": 100, "from_y": 200, "to_x": 300, "to_y": 400, "button": "left"}},
		{"mouse_move_relative", map[string]any{"dx": 20, "dy": -10}},
		{"mouse_click", map[string]any{"button": "right"}},
		{"mouse_drag_relative", map[string]any{"dx": 20, "dy": 10, "button": "left"}},
		{"mouse_scroll", map[string]any{"delta": -3}},
		{"release_all", map[string]any{}},
		{"release_control", map[string]any{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: test.args})
			if err != nil {
				t.Fatalf("CallTool(): %v", err)
			}
			if result.IsError {
				t.Fatalf("tool returned error: %#v", result.Content)
			}
		})
	}
}

func TestServerPublishesSelfDescribingResources(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "kvmctl-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	resources, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources(): %v", err)
	}
	got := make([]string, 0, len(resources.Resources))
	for _, resource := range resources.Resources {
		got = append(got, resource.URI)
	}
	sort.Strings(got)
	want := []string{"kvmctl://errors", "kvmctl://guide", "kvmctl://key-names"}
	if len(got) != len(want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("resources = %v, want %v", got, want)
		}
	}
	guide, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: "kvmctl://guide"})
	if err != nil {
		t.Fatalf("ReadResource(guide): %v", err)
	}
	if len(guide.Contents) != 1 || !strings.Contains(guide.Contents[0].Text, "0..1000") {
		t.Fatalf("guide = %#v, want normalized coordinate instructions", guide.Contents)
	}
}

func TestInputControlIsExclusiveBetweenMCPSessions(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()

	connect := func(name string) (*mcp.ClientSession, *mcp.ServerSession) {
		t.Helper()
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatalf("connect server for %s: %v", name, err)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: name, Version: "0.1.0"}, nil)
		clientSession, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatalf("connect client %s: %v", name, err)
		}
		return clientSession, serverSession
	}
	clientOne, serverOne := connect("client-one")
	defer clientOne.Close()
	defer serverOne.Close()
	clientTwo, serverTwo := connect("client-two")
	defer clientTwo.Close()
	defer serverTwo.Close()

	callKey := func(client *mcp.ClientSession) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "key_press", Arguments: map[string]any{"keys": []any{"escape"}, "hold_ms": 80}})
		if err != nil {
			t.Fatalf("CallTool(key_press): %v", err)
		}
		return result
	}
	if result := callKey(clientOne); result.IsError {
		t.Fatalf("first client could not acquire control: %#v", result.Content)
	}
	if result := callKey(clientTwo); !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "CONTROL_BUSY") {
		t.Fatalf("second client result = %#v, want CONTROL_BUSY", result)
	}
	released, err := clientOne.CallTool(ctx, &mcp.CallToolParams{Name: "release_control", Arguments: map[string]any{}})
	if err != nil || released.IsError {
		t.Fatalf("release_control result = %#v, error = %v", released, err)
	}
	if result := callKey(clientTwo); result.IsError {
		t.Fatalf("second client could not acquire after release: %#v", result.Content)
	}
}

func TestClosingOwningSessionReleasesInputControl(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	server := mcpserver.New("0.1.0-test", controller)
	ctx := context.Background()
	connect := func(name string) (*mcp.ClientSession, *mcp.ServerSession) {
		t.Helper()
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		clientSession, err := mcp.NewClient(&mcp.Implementation{Name: name, Version: "0.1.0"}, nil).Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		return clientSession, serverSession
	}
	owner, ownerServer := connect("owner")
	result, err := owner.CallTool(ctx, &mcp.CallToolParams{Name: "key_press", Arguments: map[string]any{"keys": []any{"escape"}, "hold_ms": 80}})
	if err != nil || result.IsError {
		t.Fatalf("owner input: result=%#v err=%v", result, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	_ = ownerServer.Close()

	next, nextServer := connect("next")
	defer next.Close()
	defer nextServer.Close()
	deadline := time.Now().Add(time.Second)
	for {
		result, err = next.CallTool(ctx, &mcp.CallToolParams{Name: "key_press", Arguments: map[string]any{"keys": []any{"escape"}, "hold_ms": 80}})
		if err == nil && !result.IsError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("control was not released after session close: result=%#v err=%v", result, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExpiredLeaseAutomaticallyReleasesInputState(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	server := mcpserver.NewWithOptions("0.1.0-test", controller, mcpserver.Options{ControlLease: 30 * time.Millisecond})
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "owner", Version: "0.1.0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "key_press", Arguments: map[string]any{"keys": []any{"escape"}, "hold_ms": 80}})
	if err != nil || result.IsError {
		t.Fatalf("input result=%#v err=%v", result, err)
	}
	before := len(controller.Events())
	time.Sleep(80 * time.Millisecond)
	events := controller.Events()
	if len(events) <= before || events[len(events)-1].Kind != kvm.EventReleaseAll {
		t.Fatalf("events = %#v, want automatic release after lease expiry", events)
	}
}
