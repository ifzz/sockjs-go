package sockjs

import (
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestSession() *session {
	// session with long expiration and heartbeats with ID
	return newSession(nil, "sessionId", 1000*time.Second, 1000*time.Second)
}

func TestSession_Create(t *testing.T) {
	session := newTestSession()
	_ = session.sendMessage("this is a message")
	if len(session.sendBuffer) != 1 {
		t.Errorf("session send buffer should contain 1 message")
	}
	_ = session.sendMessage("another message")
	if len(session.sendBuffer) != 2 {
		t.Errorf("session send buffer should contain 2 messages")
	}
	if session.GetSessionState() != SessionOpening {
		t.Errorf("session in wrong state %v, should be %v", session.GetSessionState(), SessionOpening)
	}
}

func TestSession_Request(t *testing.T) {
	req, _ := http.NewRequest("POST", "/server/session/jsonp_send", strings.NewReader("[\"message\"]"))
	sess := newSession(req, "session", time.Second, time.Second)

	if sess.Request() == nil {
		t.Error("session initial request should have been saved.")
	}
	if sess.Request().URL.String() != req.URL.String() {
		t.Errorf("Expected stored session request URL to equal %s, got %s", req.URL.String(), sess.Request().URL.String())
	}
}

func TestSession_ConcurrentSend(t *testing.T) {
	session := newTestSession()
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			_ = session.sendMessage("message D")
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if len(session.sendBuffer) != 100 {
		t.Errorf("session send buffer should contain 100 messages")
	}
}

func TestSession_AttachReceiver(t *testing.T) {
	session := newTestSession()
	recv := &testReceiver{}
	if err := session.attachReceiver(recv); err != nil {
		t.Errorf("Should not return error")
	}
	if session.GetSessionState() != SessionActive {
		t.Errorf("session in wrong state after receiver attached %d, should be %d", session.GetSessionState(), SessionActive)
	}
	session.detachReceiver()
	if err := session.attachReceiver(recv); err != nil {
		t.Errorf("Should not return error")
	}
}

func TestSession_Timeout(t *testing.T) {
	sess := newSession(nil, "id", 10*time.Millisecond, 10*time.Second)
	select {
	case <-sess.closeCh:
	case <-time.After(20 * time.Millisecond):
		select {
		case <-sess.closeCh:
			// still ok
		default:
			t.Errorf("sess close notification channel should close")
		}
	}
	if sess.GetSessionState() != SessionClosed {
		t.Errorf("session did not timeout")
	}
}

func TestSession_TimeoutOfClosedSession(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexcpected error '%v'", r)
		}
	}()
	sess := newSession(nil, "id", 1*time.Millisecond, time.Second)
	sess.closing()
	time.Sleep(1 * time.Millisecond)
	sess.closing()
}

func TestSession_AttachReceiverAndCheckHeartbeats(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexcpected error '%v'", r)
		}
	}()
	session := newSession(nil, "id", time.Second, 10*time.Millisecond) // 10ms heartbeats
	recv := newTestReceiver()
	defer close(recv.doneCh)
	noError(t, session.attachReceiver(recv))
	time.Sleep(120 * time.Millisecond)
	recv.Lock()
	if len(recv.frames) < 10 || len(recv.frames) > 13 { // should get around 10 heartbeats (120ms/10ms)
		t.Fatalf("Wrong number of frames received, got '%d'", len(recv.frames))
	}
	for i := 1; i < len(recv.frames); i++ {
		if recv.frames[i] != "h" {
			t.Errorf("Heartbeat no received")
		}
	}
}

func TestSession_AttachReceiverAndRefuse(t *testing.T) {
	session := newTestSession()
	if err := session.attachReceiver(newTestReceiver()); err != nil {
		t.Errorf("Should not return error")
	}
	var a sync.WaitGroup
	a.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			defer a.Done()
			if err := session.attachReceiver(newTestReceiver()); err != errSessionReceiverAttached {
				t.Errorf("Should return error as another receiver is already attached")
			}
		}()
	}
	a.Wait()
}

func TestSession_DetachReceiver(t *testing.T) {
	session := newTestSession()
	session.detachReceiver()
	session.detachReceiver() // idempotent operation
	_ = session.attachReceiver(newTestReceiver())
	session.detachReceiver()

}

func TestSession_SendWithRecv(t *testing.T) {
	session := newTestSession()
	noError(t, session.sendMessage("message A"))
	_ = session.sendMessage("message B")
	if len(session.sendBuffer) != 2 {
		t.Errorf("There should be 2 messages in buffer, but there are %d", len(session.sendBuffer))
	}
	recv := newTestReceiver()
	defer close(recv.doneCh)

	noError(t, session.attachReceiver(recv))
	if len(recv.frames[1:]) != 2 {
		t.Errorf("Reciver should get 2 message frames from session, got %d", len(recv.frames))
	}
	noError(t, session.sendMessage("message C"))
	if len(recv.frames[1:]) != 3 {
		t.Errorf("Reciver should get 3 message frames from session, got %d", len(recv.frames))
	}
	noError(t, session.sendMessage("message D"))
	if len(recv.frames[1:]) != 4 {
		t.Errorf("Reciver should get 4 frames from session, got %d", len(recv.frames))
	}
	if len(session.sendBuffer) != 0 {
		t.Errorf("Send buffer should be empty now, but there are %d messaged", len(session.sendBuffer))
	}
}

func TestSession_Recv(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic should not happen")
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)

	session := newTestSession()
	go func() {
		defer wg.Done()

		noError(t, session.accept("message A"))
		noError(t, session.accept("message B"))
		if err := session.accept("message C"); err != ErrSessionNotOpen {
			t.Errorf("session should not accept new messages if closed, got '%v' expected '%v'", err, ErrSessionNotOpen)
		}
	}()
	if msg, _ := session.Recv(); msg != "message A" {
		t.Errorf("Got %s, should be %s", msg, "message A")
	}
	if msg, _ := session.Recv(); msg != "message B" {
		t.Errorf("Got %s, should be %s", msg, "message B")
	}
	session.close()

	wg.Wait()
}

func TestSession_Closing(t *testing.T) {
	session := newTestSession()
	session.closing()
	if _, err := session.Recv(); err == nil {
		t.Errorf("session's receive buffer channel should close")
	}
	if err := session.sendMessage("some message"); err != ErrSessionNotOpen {
		t.Errorf("session should not accept new message after close")
	}
}

func TestSession_SessionRecv(t *testing.T) {
	s := newTestSession()
	go func() {
		noError(t, s.accept("message 1"))
	}()
	msg, err := s.Recv()
	if msg != "message 1" || err != nil {
		t.Errorf("Should receive a message without error, got '%s' err '%v'", msg, err)
	}
	go func() {
		s.closing()
		_, err := s.Recv()
		if err != ErrSessionNotOpen {
			t.Errorf("session not in correct state, got '%v', expected '%v'", err, ErrSessionNotOpen)
		}
	}()
	_, err = s.Recv()
	if err != ErrSessionNotOpen {
		t.Errorf("session not in correct state, got '%v', expected '%v'", err, ErrSessionNotOpen)
	}
}

func TestSession_SessionSend(t *testing.T) {
	s := newTestSession()
	err := s.Send("message A")
	if err != nil {
		t.Errorf("session should take messages by default")
	}
	if len(s.sendBuffer) != 1 || s.sendBuffer[0] != "message A" {
		t.Errorf("Message not properly queued in session, got '%v'", s.sendBuffer)
	}
}

func TestSession_SessionClose(t *testing.T) {
	s := newTestSession()
	s.state = SessionActive
	recv := newTestReceiver()
	noError(t, s.attachReceiver(recv))
	err := s.Close(1, "some reason")
	if len(recv.frames) != 1 || recv.frames[0] != "c[1,\"some reason\"]" {
		t.Errorf("Expected close frame, got '%v'", recv.frames)
	}
	if err != nil {
		t.Errorf("Should not get any error, got '%s'", err)
	}
	if s.closeFrame != "c[1,\"some reason\"]" {
		t.Errorf("Incorrect closeFrame, got '%s'", s.closeFrame)
	}
	if s.GetSessionState() != SessionClosing {
		t.Errorf("Incorrect session state, expected 'sessionClosing', got '%v'", s.GetSessionState())
	}
	// all the consequent receivers trying to attach shoult get the same close frame
	var i = 100
	for i > 0 {
		recv := newTestReceiver()
		err := s.attachReceiver(recv)
		if err != nil {
			// give a chance to a receiver to detach
			runtime.Gosched()
			continue
		}
		i--
		if len(recv.frames) != 1 || recv.frames[0] != "c[1,\"some reason\"]" {
			t.Errorf("Close frame not received by recv, frames '%v'", recv.frames)
		}
	}
	if err := s.Close(1, "some other reson"); err != ErrSessionNotOpen {
		t.Errorf("Expected error, got '%v'", err)
	}
}

func TestSession_SessionSessionId(t *testing.T) {
	s := newTestSession()
	if s.ID() != "sessionId" {
		t.Errorf("Unexpected session ID, got '%s', expected '%s'", s.ID(), "sessionId")
	}
}

func newTestReceiver() *testReceiver {
	return &testReceiver{
		doneCh:      make(chan struct{}),
		interruptCh: make(chan struct{}),
	}
}

type testReceiver struct {
	sync.Mutex
	doneCh, interruptCh chan struct{}
	frames              []string
}

func (t *testReceiver) doneNotify() <-chan struct{}        { return t.doneCh }
func (t *testReceiver) interruptedNotify() <-chan struct{} { return t.interruptCh }
func (t *testReceiver) close()                             { close(t.doneCh) }
func (t *testReceiver) canSend() bool {
	select {
	case <-t.doneCh:
		return false // already closed
	default:
		return true
	}
}
func (t *testReceiver) sendBulk(messages ...string) error {
	for _, m := range messages {
		if err := t.sendFrame(m); err != nil {
			return err
		}
	}
	return nil
}
func (t *testReceiver) sendFrame(frame string) error {
	t.Lock()
	defer t.Unlock()
	t.frames = append(t.frames, frame)
	return nil
}

func noError(t *testing.T, err error) {
	if err != nil {
		t.Error(err)
		t.Fail()
	}
}

func (t *testReceiver) receiverType() ReceiverType {
	return ReceiverTypeNone
}
