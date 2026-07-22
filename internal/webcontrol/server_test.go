package webcontrol_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/control"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/Kevin-HYX/kvmctl/internal/webcontrol"
	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
)

func TestEntryRedemptionProtectsControlAssets(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()

	unauthorized, err := fixture.server.Client().Get(fixture.server.URL + "/control/")
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusNotFound {
		t.Fatalf("unauthorized status = %d, want 404", unauthorized.StatusCode)
	}

	entry, err := fixture.sessions.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/control/" {
		t.Fatalf("redemption status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
	}
	if response.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(response.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("security headers = %#v", response.Header)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 0 || !cookies[0].Expires.IsZero() {
		t.Fatalf("session cookie = %#v, want non-persistent secure cookie", cookies)
	}

	parsed, _ := url.Parse(entry.EntryURL)
	reused, err := client.Get(fixture.server.URL + "/control/enter?token=" + url.QueryEscape(parsed.Query().Get("token")))
	if err != nil {
		t.Fatal(err)
	}
	if reused.StatusCode != http.StatusNotFound {
		t.Fatalf("reused entry status = %d, want 404", reused.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, fixture.server.URL+"/control/", nil)
	request.AddCookie(cookies[0])
	page, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if page.StatusCode != http.StatusOK || !strings.HasPrefix(page.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page status=%d type=%q", page.StatusCode, page.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`id="control-surface"`, `id="transport-direct"`, `id="transport-relayed"`, `id="connection-rtt"`, `id="connection-fps"`, `id="special-keys-toggle"`, `id="special-keys"`, `id="pointer-mode"`} {
		if !bytes.Contains(body, []byte(required)) {
			t.Fatalf("control page is missing %s", required)
		}
	}
}

func TestBrowserSessionCanReadWebRTCConfiguration(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)
	request, _ := http.NewRequest(http.MethodGet, fixture.server.URL+"/control/webrtc/config", nil)
	request.AddCookie(cookie)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status = %d, want 200", response.StatusCode)
	}
	var got struct {
		Enabled          bool     `json:"enabled"`
		STUNURLs         []string `json:"stun_urls"`
		ConnectTimeoutMS int      `json:"connect_timeout_ms"`
	}
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || len(got.STUNURLs) != 0 || got.ConnectTimeoutMS != 10000 {
		t.Fatalf("WebRTC config = %#v", got)
	}
}

func TestWebSocketStreamsJPEGAndAcceptsStatefulInput(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()
	entry, err := fixture.sessions.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	cookie := response.Cookies()[0]
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	foundJPEG := false
	for !foundJPEG {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if messageType == websocket.MessageBinary {
			foundJPEG = len(data) > 2 && data[0] == 0xff && data[1] == 0xd8
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["ctrl","a"]}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(fixture.controller.Events()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	events := fixture.controller.Events()
	if len(events) == 0 || events[0].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want stateful keyboard event", events)
	}
	_ = connection.Close(websocket.StatusNormalClosure, "test complete")
	deadline = time.Now().Add(time.Second)
	for fixture.sessions.Status().State == webcontrol.StateConnected && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := fixture.sessions.Status().State; got != webcontrol.StateReconnecting {
		t.Fatalf("state after disconnect = %s, want reconnecting", got)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events = fixture.controller.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventReleaseAll {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(events) == 0 || events[len(events)-1].Kind != kvm.EventReleaseAll {
		t.Fatalf("events after disconnect = %#v, want final release_all", events)
	}
}

func TestWebSocketKeepsOnlyTheLatestPendingAbsolutePointerPosition(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &slowPointerController{MemoryController: memory, delay: 110 * time.Millisecond}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	startedAt := time.Now()
	for index := 1; index <= 30; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, index*30)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(800 * time.Millisecond)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(events) == 0 || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events after %s = %#v, want latest pointer state followed by keyboard without a stale pointer backlog", time.Since(startedAt), events)
	}
	pointerEvents := make([]kvm.Event, 0, len(events))
	for _, event := range events {
		if event.Kind == kvm.EventAbsolutePointerState {
			pointerEvents = append(pointerEvents, event)
		}
	}
	if len(pointerEvents) > 2 {
		t.Fatalf("absolute pointer events = %#v, want at most one in-flight state and one latest pending state", pointerEvents)
	}
	if len(pointerEvents) == 0 || pointerEvents[len(pointerEvents)-1].X != 900 {
		t.Fatalf("absolute pointer events = %#v, want final x=900", pointerEvents)
	}
}

func TestWebSocketAccumulatesPendingRelativePointerMovement(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &slowPointerController{MemoryController: memory, delay: 110 * time.Millisecond}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	for index := 0; index < 30; index++ {
		if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_relative","dx":2,"dy":-1,"buttons":[]}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(800 * time.Millisecond)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(events) == 0 || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want accumulated relative movement followed by keyboard without a stale backlog", events)
	}
	var totalDX, totalDY, pointerEvents int
	for _, event := range events {
		if event.Kind == kvm.EventRelativePointerState {
			totalDX += event.DX
			totalDY += event.DY
			pointerEvents++
		}
	}
	if pointerEvents > 2 || totalDX != 60 || totalDY != -30 {
		t.Fatalf("relative pointer events = %#v, count=%d total=(%d,%d), want at most two events totaling (60,-30)", events, pointerEvents, totalDX, totalDY)
	}
}

func TestWebSocketPreservesPointerButtonTransitionsWhileCoalescingMovement(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &slowPointerController{MemoryController: memory, delay: 110 * time.Millisecond}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	for index := 0; index < 20; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, 100+index)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":700,"y":500,"buttons":["left"]}`)); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 10; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":["left"]}`, 700+index*10)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":900,"y":500,"buttons":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(1200 * time.Millisecond)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(events) == 0 || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want pointer transitions followed by keyboard without a stale backlog", events)
	}
	pointerEvents := make([]kvm.Event, 0, len(events))
	for _, event := range events {
		if event.Kind == kvm.EventAbsolutePointerState {
			pointerEvents = append(pointerEvents, event)
		}
	}
	pressed := -1
	for index, event := range pointerEvents {
		if len(event.Buttons) == 1 && event.Buttons[0] == "left" {
			pressed = index
			break
		}
	}
	if pressed < 0 {
		t.Fatalf("absolute pointer events = %#v, want a preserved left-button press", pointerEvents)
	}
	last := pointerEvents[len(pointerEvents)-1]
	if pressed == len(pointerEvents)-1 || len(last.Buttons) != 0 || last.X != 900 {
		t.Fatalf("absolute pointer events = %#v, want the preserved release at x=900 after the press", pointerEvents)
	}
	if len(pointerEvents) > 5 {
		t.Fatalf("absolute pointer events = %#v, want movement coalesced around the button transitions", pointerEvents)
	}
}

func TestWebSocketAppliesBackpressureWithoutDroppingReliableInput(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &blockingKeyboardController{
		MemoryController: memory,
		started:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	connection, ctx, cancel := dialBrowserWebSocket(t, fixture)
	defer cancel()
	defer connection.CloseNow()

	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("first keyboard state did not enter the controller")
	}
	const reliableInputs = maxPendingBrowserInputsForTest + 44
	for index := 1; index < reliableInputs; index++ {
		key := "a"
		if index%2 == 1 {
			key = "b"
		}
		message := fmt.Sprintf(`{"type":"keyboard","keys":[%q]}`, key)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the first controller call blocked long enough for the server to fill
	// its pending-input capacity and apply backpressure to the socket reader.
	time.Sleep(100 * time.Millisecond)
	close(controller.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countEvents(memory.Events(), kvm.EventKeyboardState) == reliableInputs {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("keyboard events = %d, want all %d reliable inputs", countEvents(memory.Events(), kvm.EventKeyboardState), reliableInputs)
}

func TestWebSocketDisconnectWaitsForInFlightInputBeforeRelease(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &blockingPointerController{
		MemoryController: memory,
		started:          make(chan struct{}),
		release:          make(chan struct{}),
		canceled:         make(chan struct{}),
	}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	connection, ctx, cancel := dialBrowserWebSocket(t, fixture)
	defer cancel()

	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":700,"y":500,"buttons":[]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("pointer state did not enter the controller")
	}
	closed := make(chan struct{})
	go func() {
		_ = connection.Close(websocket.StatusNormalClosure, "test disconnect")
		close(closed)
	}()
	select {
	case <-controller.canceled:
	case <-time.After(100 * time.Millisecond):
	}
	close(controller.release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("WebSocket did not close after the in-flight input completed")
	}

	deadline := time.Now().Add(time.Second)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) >= 2 && events[len(events)-1].Kind == kvm.EventReleaseAll {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(events) < 2 || events[0].Kind != kvm.EventAbsolutePointerState || events[len(events)-1].Kind != kvm.EventReleaseAll {
		t.Fatalf("events = %#v, want the in-flight pointer state followed by release_all", events)
	}
}

func TestWebSocketValidatesPointerInputBeforeCoalescing(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &blockingPointerController{
		MemoryController: memory,
		started:          make(chan struct{}),
		release:          make(chan struct{}),
		canceled:         make(chan struct{}),
	}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	connection, ctx, cancel := dialBrowserWebSocket(t, fixture)
	defer cancel()
	defer connection.CloseNow()
	defer close(controller.release)

	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":100,"y":500,"buttons":[]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("pointer state did not enter the controller")
	}
	for _, message := range []string{
		`{"type":"pointer_absolute","x":500,"y":500,"buttons":[]}`,
		`{"type":"pointer_absolute","x":2000,"y":500,"buttons":[]}`,
		`{"type":"pointer_absolute","x":600,"y":500,"buttons":[]}`,
	} {
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}

	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readCancel()
	for {
		messageType, data, err := connection.Read(readCtx)
		if err != nil {
			t.Fatalf("did not receive INVALID_ARGUMENT before pointer coalescing: %v", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		var response struct {
			Type string `json:"type"`
			Code string `json:"code"`
		}
		if json.Unmarshal(data, &response) == nil && response.Type == "error" && response.Code == string(kvm.ErrorInvalidArgument) {
			return
		}
	}
}

func TestWebSocketReleaseAllDiscardsPendingPointerMotion(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &blockingPointerController{
		MemoryController: memory,
		started:          make(chan struct{}),
		release:          make(chan struct{}),
		canceled:         make(chan struct{}),
	}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	connection, ctx, cancel := dialBrowserWebSocket(t, fixture)
	defer cancel()
	defer connection.CloseNow()

	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":100,"y":500,"buttons":[]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("pointer state did not enter the controller")
	}
	for index := 1; index <= 30; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, index*30)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"release_all"}`)); err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	close(controller.release)

	deadline := time.Now().Add(time.Second)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if countEvents(events, kvm.EventAbsolutePointerState) != 1 {
		t.Fatalf("events = %#v, want release_all to discard all pending pointer motion", events)
	}
	if len(events) < 3 || events[0].Kind != kvm.EventAbsolutePointerState || events[len(events)-2].Kind != kvm.EventReleaseAll || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want in-flight pointer, release_all, then keyboard", events)
	}
}

func TestWebSocketPreservesWheelAsACoalescingBarrier(t *testing.T) {
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	controller := &slowPointerController{MemoryController: memory, delay: 110 * time.Millisecond}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	connection, ctx, cancel := dialBrowserWebSocket(t, fixture)
	defer cancel()
	defer connection.CloseNow()

	for index := 1; index <= 20; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, index*10)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"pointer_absolute","x":500,"y":500,"buttons":[],"wheel":3}`)); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 20; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, 500+index*10)
		if err := connection.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"keyboard","keys":["a"]}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	var events []kvm.Event
	for time.Now().Before(deadline) {
		events = memory.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	wheelEvents := 0
	pointerEvents := 0
	for _, event := range events {
		if event.Kind != kvm.EventAbsolutePointerState {
			continue
		}
		pointerEvents++
		if event.Delta == 3 {
			wheelEvents++
		}
	}
	if wheelEvents != 1 || pointerEvents > 4 || len(events) == 0 || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want one preserved wheel barrier, coalesced motion, then keyboard", events)
	}
}

func TestWebRTCStreamsJPEGAndAcceptsStatefulInput(t *testing.T) {
	largeJPEG := make([]byte, 1_524_251)
	largeJPEG[0], largeJPEG[1] = 0xff, 0xd8
	largeJPEG[len(largeJPEG)-2], largeJPEG[len(largeJPEG)-1] = 0xff, 0xd9
	memory := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	fixedFrames := &fixedFrameController{MemoryController: memory, jpeg: append([]byte(nil), largeJPEG...)}
	controller := &slowFramePointerController{fixedFrameController: fixedFrames, delay: 110 * time.Millisecond}
	fixture := newServerFixtureWithController(t, memory, controller)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ordered := false
	maxRetransmits := uint16(0)
	frames, err := peer.CreateDataChannel("frames", &webrtc.DataChannelInit{Ordered: &ordered, MaxRetransmits: &maxRetransmits})
	if err != nil {
		t.Fatal(err)
	}
	controlChannel, err := peer.CreateDataChannel("control", nil)
	if err != nil {
		t.Fatal(err)
	}
	frameChunks := make(chan []byte, 1024)
	controlOpened := make(chan struct{}, 1)
	frames.OnMessage(func(message webrtc.DataChannelMessage) {
		select {
		case frameChunks <- append([]byte(nil), message.Data...):
		default:
		}
	})
	controlChannel.OnOpen(func() { controlOpened <- struct{}{} })

	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatheringComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gatheringComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("client ICE gathering timed out")
	}
	payload, err := json.Marshal(peer.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, fixture.server.URL+"/control/webrtc/offer", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", fixture.server.URL)
	request.AddCookie(cookie)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("offer status = %d, want 200", response.StatusCode)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	select {
	case <-controlOpened:
	case <-ctx.Done():
		t.Fatal("control DataChannel did not open")
	}
	const sampleFrames = 10
	startedAt := time.Now()
	for index := 0; index < sampleFrames; index++ {
		frame := receiveChunkedFrame(t, ctx, frameChunks)
		if len(frame) != len(largeJPEG) {
			t.Fatalf("frame length = %d, want large frame length %d", len(frame), len(largeJPEG))
		}
		if len(frame) < 2 || frame[0] != 0xff || frame[1] != 0xd8 {
			t.Fatalf("frame does not contain JPEG data: %x", frame[:min(2, len(frame))])
		}
	}
	elapsed := time.Since(startedAt)
	t.Logf("received %d fragmented frames (%d bytes each) in %s: %.1f FPS, %.1f Mbit/s payload", sampleFrames, len(largeJPEG), elapsed, float64(sampleFrames)/elapsed.Seconds(), float64(sampleFrames*len(largeJPEG)*8)/elapsed.Seconds()/1_000_000)
	for index := 1; index <= 30; index++ {
		message := fmt.Sprintf(`{"type":"pointer_absolute","x":%d,"y":500,"buttons":[]}`, index*30)
		if err := controlChannel.SendText(message); err != nil {
			t.Fatal(err)
		}
	}
	if err := controlChannel.SendText(`{"type":"keyboard","keys":["ctrl","a"]}`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events := fixture.controller.Events()
		if len(events) > 0 && events[len(events)-1].Kind == kvm.EventKeyboardState {
			break
		}
		time.Sleep(time.Millisecond)
	}
	events := fixture.controller.Events()
	if len(events) == 0 || events[len(events)-1].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want coalesced pointer states followed by keyboard input", events)
	}
	pointerEvents := make([]kvm.Event, 0, len(events))
	for _, event := range events {
		if event.Kind == kvm.EventAbsolutePointerState {
			pointerEvents = append(pointerEvents, event)
		}
	}
	if len(pointerEvents) == 0 || len(pointerEvents) > 2 || pointerEvents[len(pointerEvents)-1].X != 900 {
		t.Fatalf("direct-channel pointer events = %#v, want at most two events ending at x=900", pointerEvents)
	}
	status := fixture.sessions.Status()
	if status.State != webcontrol.StateConnected || status.Transport != webcontrol.TransportDirect || status.TransportState != webcontrol.TransportStateConnected {
		t.Fatalf("session status = %#v, want connected direct transport", status)
	}
	if err := frames.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events = fixture.controller.Events()
		status = fixture.sessions.Status()
		if status.State == webcontrol.StateReconnecting && len(events) > 0 && events[len(events)-1].Kind == kvm.EventReleaseAll {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if status.State != webcontrol.StateReconnecting || len(events) == 0 || events[len(events)-1].Kind != kvm.EventReleaseAll {
		t.Fatalf("after channel close status=%#v events=%#v, want reconnecting and release_all", status, events)
	}
}

func TestWebRTCFrameSendFailureKeepsReliableControlChannelOpen(t *testing.T) {
	jpeg := make([]byte, 12<<10)
	jpeg[0], jpeg[1] = 0xff, 0xd8
	jpeg[len(jpeg)-2], jpeg[len(jpeg)-1] = 0xff, 0xd9
	t.Run("transport send failure", func(t *testing.T) {
		testWebRTCFrameFailureKeepsReliableControlChannelOpen(t, jpeg, 8192, "")
	})
	t.Run("invalid captured frame", func(t *testing.T) {
		testWebRTCFrameFailureKeepsReliableControlChannelOpen(t, make([]byte, 0), 0, "VIDEO_FRAME_INVALID")
	})
}

func testWebRTCFrameFailureKeepsReliableControlChannelOpen(t *testing.T, jpeg []byte, remoteMaxMessageSize int, wantErrorCode string) {
	t.Helper()
	fixture := newServerFixtureWithJPEG(t, jpeg)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ordered := false
	maxRetransmits := uint16(0)
	frames, err := peer.CreateDataChannel("frames", &webrtc.DataChannelInit{Ordered: &ordered, MaxRetransmits: &maxRetransmits})
	if err != nil {
		t.Fatal(err)
	}
	controlChannel, err := peer.CreateDataChannel("control", nil)
	if err != nil {
		t.Fatal(err)
	}
	controlOpened := make(chan struct{}, 1)
	controlChannel.OnOpen(func() { controlOpened <- struct{}{} })
	controlUpdates := make(chan []byte, 16)
	controlChannel.OnMessage(func(message webrtc.DataChannelMessage) {
		select {
		case controlUpdates <- append([]byte(nil), message.Data...):
		default:
		}
	})
	frameReceived := make(chan struct{}, 1)
	frames.OnMessage(func(webrtc.DataChannelMessage) {
		select {
		case frameReceived <- struct{}{}:
		default:
		}
	})

	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatheringComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gatheringComplete:
	case <-time.After(5 * time.Second):
		t.Fatal("client ICE gathering timed out")
	}
	localDescription := *peer.LocalDescription()
	if remoteMaxMessageSize > 0 {
		localDescription.SDP = replaceSDPMaxMessageSize(t, localDescription.SDP, remoteMaxMessageSize)
	}
	payload, _ := json.Marshal(localDescription)
	request, _ := http.NewRequest(http.MethodPost, fixture.server.URL+"/control/webrtc/offer", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", fixture.server.URL)
	request.AddCookie(cookie)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("offer status = %d, want 200", response.StatusCode)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}

	select {
	case <-controlOpened:
	case <-time.After(5 * time.Second):
		t.Fatal("control DataChannel did not open")
	}
	if wantErrorCode != "" {
		waitForControlErrorCode(t, controlUpdates, wantErrorCode)
	} else {
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case <-frameReceived:
		t.Fatal("oversized DataChannel message unexpectedly reached the client")
	default:
	}
	if controlChannel.ReadyState() != webrtc.DataChannelStateOpen {
		t.Fatalf("control DataChannel state = %s, want open", controlChannel.ReadyState())
	}
	if err := controlChannel.SendText(`{"type":"keyboard","keys":["a"]}`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(fixture.controller.Events()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if events := fixture.controller.Events(); len(events) == 0 || events[0].Kind != kvm.EventKeyboardState {
		t.Fatalf("events = %#v, want keyboard input after frame send failure", events)
	}
}

func waitForControlErrorCode(t *testing.T, updates <-chan []byte, want string) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case data := <-updates:
			var update struct {
				Type string `json:"type"`
				Code string `json:"code"`
			}
			if json.Unmarshal(data, &update) == nil && update.Type == "error" && update.Code == want {
				return
			}
		case <-timer.C:
			t.Fatalf("control DataChannel did not report %s", want)
		}
	}
}

func replaceSDPMaxMessageSize(t *testing.T, sdp string, size int) string {
	t.Helper()
	const prefix = "a=max-message-size:"
	start := strings.Index(sdp, prefix)
	if start < 0 {
		t.Fatal("offer SDP does not advertise max-message-size")
	}
	valueStart := start + len(prefix)
	valueEnd := strings.Index(sdp[valueStart:], "\r\n")
	if valueEnd < 0 {
		t.Fatal("offer SDP max-message-size line is malformed")
	}
	valueEnd += valueStart
	return sdp[:valueStart] + fmt.Sprint(size) + sdp[valueEnd:]
}

func receiveChunkedFrame(t *testing.T, ctx context.Context, packets <-chan []byte) []byte {
	t.Helper()
	var frameID uint32
	var frameSize uint32
	var chunks [][]byte
	received := 0
	for {
		select {
		case packet := <-packets:
			if len(packet) < 16 || string(packet[:4]) != "KVM1" {
				t.Fatalf("invalid frame chunk header: %x", packet[:min(16, len(packet))])
			}
			packetFrameID := binary.BigEndian.Uint32(packet[4:8])
			chunkIndex := int(binary.BigEndian.Uint16(packet[8:10]))
			chunkCount := int(binary.BigEndian.Uint16(packet[10:12]))
			packetFrameSize := binary.BigEndian.Uint32(packet[12:16])
			if chunks == nil || isNewerTestFrameID(packetFrameID, frameID) {
				frameID, frameSize = packetFrameID, packetFrameSize
				chunks, received = make([][]byte, chunkCount), 0
			}
			if packetFrameID != frameID || packetFrameSize != frameSize || chunkIndex >= len(chunks) {
				continue
			}
			if chunks[chunkIndex] == nil {
				chunks[chunkIndex] = append([]byte(nil), packet[16:]...)
				received++
			}
			if received == len(chunks) {
				frame := bytes.Join(chunks, nil)
				if len(frame) != int(frameSize) {
					t.Fatalf("reassembled frame length = %d, want %d", len(frame), frameSize)
				}
				return frame
			}
		case <-ctx.Done():
			t.Fatal("frames DataChannel did not receive a complete JPEG")
		}
	}
}

func isNewerTestFrameID(candidate, current uint32) bool {
	distance := candidate - current
	return distance != 0 && distance < 1<<31
}

func TestWebRTCDoesNotAcceptAReliableOrderedFrameChannel(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()
	cookie := redeemBrowserEntry(t, fixture)

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	_, err = peer.CreateDataChannel("frames", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = peer.CreateDataChannel("control", nil)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gatheringComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gatheringComplete
	payload, _ := json.Marshal(peer.LocalDescription())
	request, _ := http.NewRequest(http.MethodPost, fixture.server.URL+"/control/webrtc/offer", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", fixture.server.URL)
	request.AddCookie(cookie)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("offer status = %d, want 200", response.StatusCode)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if status := fixture.sessions.Status(); status.State == webcontrol.StateConnected {
		t.Fatalf("session status = %#v, reliable ordered frames must not establish direct transport", status)
	}
}

func TestWebSocketWaitsForFrameAcknowledgement(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()
	entry, err := fixture.sessions.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	cookie := response.Cookies()[0]
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	for {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if messageType == websocket.MessageBinary && len(data) > 2 && data[0] == 0xff && data[1] == 0xd8 {
			break
		}
	}

	nextFrame := make(chan error, 1)
	go func() {
		for {
			messageType, data, err := connection.Read(ctx)
			if err != nil {
				nextFrame <- err
				return
			}
			if messageType == websocket.MessageBinary && len(data) > 2 && data[0] == 0xff && data[1] == 0xd8 {
				nextFrame <- nil
				return
			}
		}
	}()

	select {
	case err := <-nextFrame:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("received a second JPEG before acknowledging the first")
	case <-time.After(150 * time.Millisecond):
	}

	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"frame_ack"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-nextFrame:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("no JPEG arrived after frame acknowledgement")
	}
}

func TestWebSocketRejectsAnUntrustedOrigin(t *testing.T) {
	fixture := newServerFixture(t)
	defer fixture.server.Close()
	entry, err := fixture.sessions.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	cookie := response.Cookies()[0]
	header := http.Header{}
	header.Set("Origin", "https://untrusted.invalid")
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, rejected, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if connection != nil {
		connection.CloseNow()
	}
	if err == nil {
		t.Fatal("websocket.Dial() error = nil, want untrusted origin rejection")
	}
	if rejected == nil || rejected.StatusCode != http.StatusForbidden {
		t.Fatalf("rejected response = %#v, want HTTP 403", rejected)
	}
	_ = rejected.Body.Close()
}

type serverFixture struct {
	server     *httptest.Server
	sessions   *webcontrol.SessionManager
	controller *kvm.MemoryController
}

func redeemBrowserEntry(t *testing.T, fixture serverFixture) *http.Cookie {
	t.Helper()
	entry, err := fixture.sessions.Open()
	if err != nil {
		t.Fatal(err)
	}
	client := fixture.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSeeOther || len(response.Cookies()) != 1 {
		t.Fatalf("entry response status=%d cookies=%d", response.StatusCode, len(response.Cookies()))
	}
	_ = response.Body.Close()
	return response.Cookies()[0]
}

func newServerFixture(t *testing.T) serverFixture {
	return newServerFixtureWithJPEG(t, nil)
}

func newServerFixtureWithJPEG(t *testing.T, jpeg []byte) serverFixture {
	t.Helper()
	controller := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		ScreenImage:        solidImage(320, 180),
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})
	var operationalController kvm.OperationalController = controller
	if jpeg != nil {
		operationalController = &fixedFrameController{MemoryController: controller, jpeg: append([]byte(nil), jpeg...)}
	}
	return newServerFixtureWithController(t, controller, operationalController)
}

func newServerFixtureWithController(t *testing.T, controller *kvm.MemoryController, operationalController kvm.OperationalController) serverFixture {
	t.Helper()
	testServer := httptest.NewUnstartedServer(nil)
	origin := "https://" + testServer.Listener.Addr().String()
	arbiter := control.New(time.Second, 5*time.Second, func() { _ = controller.ReleaseAll(context.Background()) })
	sessions := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   origin,
		EntryTTL:       time.Minute,
		ReconnectGrace: time.Second,
	})
	webServer, err := webcontrol.NewServer(webcontrol.ServerOptions{
		PublicOrigin:   origin,
		Sessions:       sessions,
		Control:        arbiter,
		Controller:     operationalController,
		FrameRate:      30,
		WebRTCEnabled:  true,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	testServer.Config.Handler = webServer.Handler()
	testServer.StartTLS()
	return serverFixture{server: testServer, sessions: sessions, controller: controller}
}

type slowPointerController struct {
	*kvm.MemoryController
	delay time.Duration
}

type slowFramePointerController struct {
	*fixedFrameController
	delay time.Duration
}

func (controller *slowFramePointerController) SetPointerAbsoluteState(ctx context.Context, request kvm.AbsolutePointerStateRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(controller.delay):
	}
	return controller.fixedFrameController.SetPointerAbsoluteState(ctx, request)
}

const maxPendingBrowserInputsForTest = 256

type blockingKeyboardController struct {
	*kvm.MemoryController
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
}

func (controller *blockingKeyboardController) SetKeyboardState(ctx context.Context, request kvm.KeyboardStateRequest) error {
	controller.startedOnce.Do(func() {
		close(controller.started)
		select {
		case <-ctx.Done():
		case <-controller.release:
		}
	})
	return controller.MemoryController.SetKeyboardState(ctx, request)
}

type blockingPointerController struct {
	*kvm.MemoryController
	started     chan struct{}
	release     chan struct{}
	canceled    chan struct{}
	startedOnce sync.Once
}

func (controller *blockingPointerController) SetPointerAbsoluteState(ctx context.Context, request kvm.AbsolutePointerStateRequest) error {
	controller.startedOnce.Do(func() { close(controller.started) })
	select {
	case <-ctx.Done():
		close(controller.canceled)
		return ctx.Err()
	case <-controller.release:
		return controller.MemoryController.SetPointerAbsoluteState(ctx, request)
	}
}

func dialBrowserWebSocket(t *testing.T, fixture serverFixture) (*websocket.Conn, context.Context, context.CancelFunc) {
	t.Helper()
	cookie := redeemBrowserEntry(t, fixture)
	header := http.Header{}
	header.Set("Origin", fixture.server.URL)
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	connection, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(fixture.server.URL, "https")+"/control/ws", &websocket.DialOptions{
		HTTPClient: fixture.server.Client(),
		HTTPHeader: header,
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return connection, ctx, cancel
}

func countEvents(events []kvm.Event, kind kvm.EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func (controller *slowPointerController) SetPointerAbsoluteState(ctx context.Context, request kvm.AbsolutePointerStateRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(controller.delay):
	}
	return controller.MemoryController.SetPointerAbsoluteState(ctx, request)
}

func (controller *slowPointerController) SetPointerRelativeState(ctx context.Context, request kvm.RelativePointerStateRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(controller.delay):
	}
	return controller.MemoryController.SetPointerRelativeState(ctx, request)
}

type fixedFrameController struct {
	*kvm.MemoryController
	jpeg []byte
}

func (controller *fixedFrameController) OpenVideoStream(context.Context) (kvm.VideoStream, error) {
	return &fixedFrameStream{jpeg: append([]byte(nil), controller.jpeg...)}, nil
}

type fixedFrameStream struct {
	jpeg        []byte
	closed      bool
	nextFrameAt time.Time
}

func (stream *fixedFrameStream) Next(ctx context.Context) (kvm.VideoFrame, error) {
	if err := ctx.Err(); err != nil {
		return kvm.VideoFrame{}, err
	}
	if stream.closed {
		return kvm.VideoFrame{}, io.EOF
	}
	if !stream.nextFrameAt.IsZero() {
		wait := time.Until(stream.nextFrameAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return kvm.VideoFrame{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	stream.nextFrameAt = time.Now().Add(time.Second / 30)
	return kvm.VideoFrame{JPEG: append([]byte(nil), stream.jpeg...), Width: 1920, Height: 1080, CapturedAt: time.Now()}, nil
}

func (stream *fixedFrameStream) Close() error {
	stream.closed = true
	return nil
}

func solidImage(width, height int) image.Image {
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			frame.Set(x, y, color.RGBA{R: 20, G: 40, B: 80, A: 255})
		}
	}
	return frame
}
